package rest

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type CoverHandler struct {
	db           *sql.DB
	imagesDir    string
	sessionStore *cache.SessionStore
	trackRepo    *repository.TrackRepo
	albumRepo    *repository.AlbumRepo
	artistRepo   *repository.ArtistRepo
}

func NewCoverHandler(db *sql.DB, imagesDir string, sessionStore *cache.SessionStore) *CoverHandler {
	return &CoverHandler{
		db:           db,
		imagesDir:    imagesDir,
		sessionStore: sessionStore,
		trackRepo:    repository.NewTrackRepo(db),
		albumRepo:    repository.NewAlbumRepo(db),
		artistRepo:   repository.NewArtistRepo(db),
	}
}

func (h *CoverHandler) Serve(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	ownerType := vars["ownerType"]
	ownerID := vars["ownerId"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	if _, err := h.sessionStore.Validate(r.Context(), session); err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if ownerType == "" || ownerID == "" {
		http.Error(w, "missing ownerType or ownerId", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	tryCover := func(libID, otype, oid string) bool {
		// Try: 64 → 256 → original (regardless of requested size)
		for _, s := range []int{64, 256} {
			p := metadata.CoverPathWithSuffix(h.imagesDir, libID, otype, oid, fmt.Sprintf("_%d", s), "jpg")
			if _, err := os.Stat(p); err == nil {
				http.ServeFile(w, r, p)
				return true
			}
		}
		p := metadata.CoverPathWithSuffix(h.imagesDir, libID, otype, oid, "", "jpg")
		if _, err := os.Stat(p); err == nil {
			http.ServeFile(w, r, p)
			return true
		}
		return false
	}

	switch ownerType {
	case "track":
		track, err := h.trackRepo.FindByID(ctx, ownerID)
		if err != nil {
			break
		}
		if track.CoverImageID != nil && tryCover(track.LibraryID, "track", track.ID) {
			return
		}
		if album, err := h.albumRepo.FindByID(ctx, track.AlbumID); err == nil {
			if album.CoverImageID != nil && tryCover("album", "album", album.ID) {
				return
			}
			if tracks, err := h.trackRepo.FindByAlbumID(ctx, album.ID); err == nil {
				for i := range tracks {
					if tracks[i].CoverImageID != nil && tryCover("album", "track", tracks[i].ID) {
						return
					}
				}
			}
		}

	case "album":
		album, err := h.albumRepo.FindByID(ctx, ownerID)
		if err != nil {
			break
		}
		if tryCover("album", "album", album.ID) {
			return
		}
		if tracks, err := h.trackRepo.FindByAlbumID(ctx, album.ID); err == nil {
			for i := range tracks {
				if tryCover("album", "track", tracks[i].ID) {
					return
				}
			}
		}

	case "artist":
		artist, err := h.artistRepo.FindByID(ctx, ownerID)
		if err != nil {
			break
		}
		if tryCover("artist", "artist", artist.ID) {
			return
		}
	}

	http.Error(w, "cover not found", http.StatusNotFound)
}
