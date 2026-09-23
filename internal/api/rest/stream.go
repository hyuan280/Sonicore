package rest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/transcoder"
)

// maxMseSegmentSeconds caps a single MSE segment request for callers without
// download permission, so one request cannot pull the whole track at once.
const maxMseSegmentSeconds = 30.0

type StreamHandler struct {
	trackRepo    *repository.TrackRepo
	sessionStore *cache.SessionStore
	userRepo     *repository.UserRepo
	heatRepo     *repository.HeatRepo
	perm         *middleware.PermissionChecker
}

func NewStreamHandler(db *sql.DB, sessionStore *cache.SessionStore) *StreamHandler {
	return &StreamHandler{
		trackRepo:    repository.NewTrackRepo(db),
		sessionStore: sessionStore,
		userRepo:     repository.NewUserRepo(db),
		heatRepo:     repository.NewHeatRepo(db),
		perm:         middleware.NewPermissionChecker(db),
	}
}

// canDownload reports whether the user's global role carries download
// permission (admin and above). The session token identifies the user but not
// its role, so the role is read from the database.
func (h *StreamHandler) canDownload(ctx context.Context, userID string) bool {
	u, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.Error("[stream] download permission lookup failed: %v", err)
		}
		return false
	}
	return port.HasPermission(u.Role, port.PermDownload)
}

// awardDownload records the download heat event. Its dedupe key is per
// (user, track), so each user is counted at most once per track.
func (h *StreamHandler) awardDownload(ctx context.Context, userID, trackID string) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := h.heatRepo.Award(ctx, trackID, userID,
		domain.HeatEventDownload, domain.HeatWeightDownload,
		domain.DownloadDedupeKey(userID, trackID)); err != nil {
		logger.Error("[stream] download heat error: %v", err)
	}
}

func (h *StreamHandler) ServeStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	trackID := vars["id"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	userID, err := h.sessionStore.Validate(r.Context(), session)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	if !h.perm.IsMember(r.Context(), track.LibraryID, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// A request is segmented playback only when it asks for MSE pieces
	// (init / start). Every other shape (raw file, byte Range, full transcode,
	// cache serve) delivers the whole track in one response and is treated as
	// a download, which requires download permission.
	query := r.URL.Query()
	isMse := query.Get("init") == "1" || query.Get("start") != ""
	if !isMse {
		if !h.canDownload(r.Context(), userID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Award the download heat only after a fully served transfer (a 200
		// body, not a HEAD, not a 206 range, and the client still connected).
		// The dedupe key is permanent, so a failed or partial transfer must
		// not consume it.
		rec := middleware.NewStatusRecorder(w)
		w = rec
		defer func() {
			if rec.Status() == http.StatusOK && r.Method != http.MethodHead && r.Context().Err() == nil {
				h.awardDownload(context.Background(), userID, track.ID)
			}
		}()
	} else if raw := query.Get("duration"); raw != "" {
		// Enforce the segment cap only when a duration is actually supplied
		// (init requests have none). Invalid, non-finite or oversized values
		// fail closed: the role check runs for all of them.
		d, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(d) || math.IsInf(d, 0) || d > maxMseSegmentSeconds {
			if !h.canDownload(r.Context(), userID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}

	quality := transcoder.ParseQuality(query.Get("quality"))
	if isMse {
		transcoder.ServeTranscoded(r.Context(), w, r, track.FilePath, quality)
		return
	}
	if transcoder.Decide(track.BitRate, track.AudioCodec, quality).Transcode {
		transcoder.ServeTranscoded(r.Context(), w, r, track.FilePath, quality)
		return
	}

	w.Header().Set("Content-Type", middleware.AudioContentType(track.FilePath))
	w.Header().Set("Content-Length", strconv.FormatInt(track.FileSize, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, track.FilePath)
}

// ServeTranscodeStatus reports whether the transcode cache for a track is ready,
// so the frontend can switch from the live stream to the seekable cache file.
func (h *StreamHandler) ServeTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	trackID := vars["id"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	userID, err := h.sessionStore.Validate(r.Context(), session)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	if !h.perm.IsMember(r.Context(), track.LibraryID, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	quality := transcoder.ParseQuality(r.URL.Query().Get("quality"))
	ready := transcoder.CacheReady(track.FilePath, quality)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ready":%t}`, ready)
}
