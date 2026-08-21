package rest

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type CoverHandler struct {
	db           *sql.DB
	sessionStore *cache.SessionStore
	trackRepo    *repository.TrackRepo
	albumRepo    *repository.AlbumRepo
	images       *repository.ImageRepo
	perm         *middleware.PermissionChecker
	covers       *metadata.CoverManager
}

func NewCoverHandler(db *sql.DB, imagesDir string, sessionStore *cache.SessionStore, covers *metadata.CoverManager) *CoverHandler {
	if covers == nil {
		// Standalone fallback (tests): no platform chain for network
		// covers; the server always passes the shared manager.
		covers = metadata.NewCoverManager(imagesDir, db, nil)
	}
	return &CoverHandler{
		db:           db,
		sessionStore: sessionStore,
		trackRepo:    repository.NewTrackRepo(db),
		albumRepo:    repository.NewAlbumRepo(db),
		images:       repository.NewImageRepo(db),
		perm:         middleware.NewPermissionChecker(db),
		covers:       covers,
	}
}

// Serve resolves the cover by images-row id (the cover_image_id clients get
// from the API). A missing file triggers an on-demand single-track
// re-extraction before serving.
func (h *CoverHandler) Serve(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	imageID := vars["imageId"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	userID, err := h.sessionStore.Validate(r.Context(), session)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if imageID == "" {
		http.Error(w, "missing imageId", http.StatusBadRequest)
		return
	}

	size := 0
	if s := r.URL.Query().Get("size"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			http.Error(w, "invalid size", http.StatusBadRequest)
			return
		}
		size = v
	}

	ctx := r.Context()

	img, err := h.images.FindByID(ctx, imageID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "cover not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Error("[cover] image lookup error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Album/artist covers are shared and carry no library (never gated);
	// track covers always belong to a library and fail closed when the row
	// is missing one.
	if img.OwnerType == "track" && (img.LibraryID == "" || !h.perm.IsMember(ctx, img.LibraryID, userID)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	p := metadata.ImageVariantPath(img, size)
	if !metadata.CoverFileExists(p) {
		switch img.OwnerType {
		case "track":
			// Restore on demand through the unified flow: embedded cover
			// first, then the platform chain (the track's metadata source)
			// when the file carries none. The track's images row may be
			// replaced, so re-resolve afterwards.
			if track, terr := h.trackRepo.FindByID(ctx, img.OwnerID); terr == nil {
				var album *domain.Album
				if len(track.Albums) > 0 {
					a, aerr := h.albumRepo.FindByID(ctx, track.Albums[0].AlbumID)
					if aerr != nil && !errors.Is(aerr, sql.ErrNoRows) {
						logger.Info("[cover] album lookup for %s: %v", track.Albums[0].AlbumID, aerr)
					} else {
						album = a
					}
				}
				if err := h.covers.EnsureTrackCover(ctx, track.LibraryID, track, album, false, true); err != nil {
					logger.Error("[cover] on-demand restoration for %s failed: %v", track.ID, err)
				} else if track.CoverImageID != nil {
					nimg, nerr := h.images.FindByID(ctx, *track.CoverImageID)
					if nerr != nil {
						logger.Info("[cover] re-resolve track image %s: %v", *track.CoverImageID, nerr)
					} else {
						img = nimg
						p = metadata.ImageVariantPath(img, size)
					}
				}
			}
		case "album":
			// Restore from the first cover-bearing track of the album.
			if album, aerr := h.albumRepo.FindByID(ctx, img.OwnerID); aerr == nil {
				if err := h.covers.BackfillAlbumCover(ctx, album, false); err != nil {
					logger.Error("[cover] on-demand album cover restore for %s failed: %v", album.ID, err)
				} else if album.CoverImageID != nil {
					nimg, nerr := h.images.FindByID(ctx, *album.CoverImageID)
					if nerr != nil {
						logger.Info("[cover] re-resolve album image %s: %v", *album.CoverImageID, nerr)
					} else {
						img = nimg
						p = metadata.ImageVariantPath(img, size)
					}
				}
			}
		}
	}
	if !metadata.CoverFileExists(p) {
		http.Error(w, "cover not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, p)
}
