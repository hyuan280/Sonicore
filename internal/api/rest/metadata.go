package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/scanner"
)

type MetadataHandler struct {
	db           *sql.DB
	trackRepo    *repository.TrackRepo
	albumRepo    *repository.AlbumRepo
	artistRepo   *repository.ArtistRepo
	umRepo       *repository.UserMetadataRepo
	settingsRepo *repository.SettingsRepo
	mbCfg        metadata.MBConfig
}

func NewMetadataHandler(db *sql.DB, mbCfg metadata.MBConfig) *MetadataHandler {
	return &MetadataHandler{
		db:           db,
		trackRepo:    repository.NewTrackRepo(db),
		albumRepo:    repository.NewAlbumRepo(db),
		artistRepo:   repository.NewArtistRepo(db),
		umRepo:       repository.NewUserMetadataRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
		mbCfg:       mbCfg,
	}
}

// mbConfig returns the MB config with DB settings overrides (same logic as scanner service).
func (h *MetadataHandler) mbConfig(ctx context.Context) metadata.MBConfig {
	cfg := h.mbCfg
	if url, err := h.settingsRepo.Get(ctx, "metadata_musicbrainz_api_url"); err == nil && url != "" {
		cfg.APIURL = url
	}
	if rl, err := h.settingsRepo.Get(ctx, "metadata_musicbrainz_rate_limit"); err == nil && rl != "" {
		fmt.Sscanf(rl, "%d", &cfg.RateLimit)
	}
	return cfg
}

func (h *MetadataHandler) newResolver(ctx context.Context) *metadata.Resolver {
	return metadata.NewResolver(h.mbConfig(ctx))
}

func (h *MetadataHandler) newMBClient(ctx context.Context) *metadata.MBClient {
	return metadata.NewMBClient(h.mbConfig(ctx))
}

func (h *MetadataHandler) Save(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID    string `json:"track_id"`
		FileHash   string `json:"file_hash"`
		TrackMBID  string `json:"track_mbid"`
		Title      string `json:"title"`
		Album      string `json:"album"`
		Year       int    `json:"year"`
		Genre      string `json:"genre"`
		AlbumMBID  string `json:"album_mbid"`
		VersionLabel string `json:"version_label"`
		Artists   []struct {
			Name string `json:"name"`
			MBID string `json:"mbid"`
		} `json:"artists"`
		Albums    []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			MBID   string `json:"mbid"`
			Artist string `json:"artist"`
		} `json:"albums"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_hash required"})
		return
	}

	// If TrackMBID is provided but album data incomplete, look up from MB
	if req.TrackMBID != "" {
		needsEnrich := req.Album == "" || req.AlbumMBID == ""
		if !needsEnrich && len(req.Artists) > 0 {
			allKnown := true
			for _, a := range req.Artists {
				if a.Name == "" || a.Name == "Unknown Artist" {
					allKnown = false
					break
				}
			}
			if !allKnown {
				needsEnrich = true
			}
		}
		if needsEnrich {
			resolver := h.newResolver(r.Context())
			result, err := resolver.IdentifyTrack(r.Context(), req.TrackMBID)
			resolver.Close()
			if err == nil && result != nil {
				if req.Album == "" && result.Album != "" {
					req.Album = result.Album
				}
				if req.AlbumMBID == "" && result.AlbumMBID != "" {
					req.AlbumMBID = result.AlbumMBID
				}
				if req.Year == 0 && result.Year != 0 {
					req.Year = result.Year
				}
				if req.Genre == "" && result.Genre != "" {
					req.Genre = result.Genre
				}
				if len(req.Artists) == 0 || (len(req.Artists) == 1 && req.Artists[0].Name == "Unknown Artist") {
					var filled []struct {
						Name string `json:"name"`
						MBID string `json:"mbid"`
					}
					for _, ar := range result.Artists {
						filled = append(filled, struct {
							Name string `json:"name"`
							MBID string `json:"mbid"`
						}{Name: ar.Name, MBID: ar.MBID})
					}
					if len(filled) > 0 {
						req.Artists = filled
					}
				}
			}
		}
	}

	// Build artist string from artists array
	artistStr := ""
	for i, a := range req.Artists {
		if i > 0 {
			artistStr += ","
		}
		artistStr += a.Name
	}

	err := h.umRepo.Upsert(r.Context(), &repository.UserMetadata{
		UserID:    userID,
		FileHash:  req.FileHash,
		TrackMBID: req.TrackMBID,
		Title:     req.Title,
		Artist:    artistStr,
		Album:     req.Album,
		Year:      req.Year,
		Genre:     req.Genre,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Immediately update all metadata in DB
	if req.TrackID != "" {
		track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
		var oldMBID string
		if err == nil {
			oldMBID = track.MBID
			if req.TrackMBID != "" {
				track.MBID = req.TrackMBID
			}
			if req.Title != "" {
				track.Title = metadata.TrimParenSuffix(req.Title)
			}
			if req.VersionLabel != "" {
				track.VersionLabel = req.VersionLabel
			}
			h.trackRepo.Update(r.Context(), track)

			// Update artists association
			if len(req.Artists) == 0 {
				req.Artists = []struct {
					Name string `json:"name"`
					MBID string `json:"mbid"`
				}{{Name: "Unknown Artist"}}
			}
			var newArtists []*domain.TrackArtist
			for i, ar := range req.Artists {
				a := h.findOrCreateArtist(r.Context(), ar.Name, ar.MBID)
				if a == nil {
					continue
				}
				newArtists = append(newArtists, &domain.TrackArtist{
					ArtistID:  a.ID,
					Role:      "performer",
					SortOrder: i,
					Artist:    a,
				})
			}
			if len(newArtists) > 0 {
				h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists)
			}

			// Resolve artist helper for new albums
			resolveArtistID := func(artistName, artistMBID string) string {
				if artistMBID != "" {
					if a := h.findOrCreateArtist(r.Context(), artistName, artistMBID); a != nil {
						return a.ID
					}
				}
				if artistName != "" {
					if a := h.findOrCreateArtist(r.Context(), artistName, ""); a != nil {
						return a.ID
					}
				}
				if len(track.Artists) > 0 {
					return track.Artists[0].ArtistID
				}
				if a := h.findOrCreateArtist(r.Context(), "Unknown Artist", ""); a != nil {
					return a.ID
				}
				return ""
			}
			// Resolve or create album from request entry
			resolveAlbum := func(al struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				MBID   string `json:"mbid"`
				Artist string `json:"artist"`
			}) (string, bool) {
				if al.ID != "" {
					return al.ID, true
				}
				artistID := ""
				var year int
				var country, genre string
				if al.MBID != "" {
					if release, err := h.newMBClient(r.Context()).LookupRelease(al.MBID); err == nil {
						if len(release.Date) >= 4 {
							fmt.Sscanf(release.Date[:4], "%d", &year)
						}
						country = release.Country
						genre = metadata.GenreFromTags(release.Tags)
						if len(release.Artists) > 0 {
							mbid := ""
							if release.Artists[0].Artist != nil {
								mbid = release.Artists[0].Artist.ID
							}
							artistID = resolveArtistID(release.Artists[0].Name, mbid)
						}
					}
				} else {
					artistID = resolveArtistID(al.Artist, "")
				}
				if artistID == "" {
					return "", false
				}
				if al.MBID != "" {
					if album, err := h.albumRepo.FindByMBID(r.Context(), al.MBID); err == nil {
						// Update missing metadata
						updated := false
						if album.Year == 0 && year != 0 {
							album.Year = year; updated = true
						}
						if album.Country == "" && country != "" {
							album.Country = country; updated = true
						}
						if album.Genre == "" && genre != "" {
							album.Genre = genre; updated = true
						}
						if updated {
							h.albumRepo.Update(r.Context(), album)
						}
						return album.ID, true
					}
					album := &domain.Album{
						ID: domain.NewID(), Title: metadata.TrimParenSuffix(al.Title),
						MBID: al.MBID, ArtistID: artistID,
						Year: year, Country: country, Genre: genre,
					}
					if err := h.albumRepo.BatchCreate(r.Context(), []domain.Album{*album}); err != nil {
						return "", false
					}
					return album.ID, true
				}
				if al.Title != "" {
					if album, err := h.albumRepo.FindByName(r.Context(), al.Title); err == nil {
						return album.ID, true
					}
					album := &domain.Album{ID: domain.NewID(), Title: metadata.TrimParenSuffix(al.Title), ArtistID: artistID}
					if err := h.albumRepo.BatchCreate(r.Context(), []domain.Album{*album}); err != nil {
						return "", false
					}
					return album.ID, true
				}
				return "", false
			}

			// Process albums: if user sent the field (even empty), replace track_albums
			if req.Albums != nil {
				var trackAlbums []*domain.TrackAlbum
				for _, al := range req.Albums {
					if albumID, ok := resolveAlbum(al); ok {
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{
							AlbumID:      albumID,
							TrackNumber:  len(trackAlbums) + 1,
							DiscNumber:   1,
						})
					}
				}
				if len(trackAlbums) == 0 {
					// Fallback: ensure at least Unknown Album
					if album, err := h.albumRepo.FindByName(r.Context(), "Unknown Album"); err == nil {
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1})
					} else {
						artistID := resolveArtistID("", "")
						album := &domain.Album{ID: domain.NewID(), Title: "Unknown Album", ArtistID: artistID}
						h.albumRepo.BatchCreate(r.Context(), []domain.Album{*album})
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1})
					}
				}
				if err := h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, trackAlbums); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save albums: %v", err)})
					return
				}
				// Apply user-edited year/genre to first album
				if len(trackAlbums) > 0 && (req.Year != 0 || req.Genre != "") {
					if album, err := h.albumRepo.FindByID(r.Context(), trackAlbums[0].AlbumID); err == nil {
						updated := false
						if req.Year != 0 && album.Year == 0 {
							album.Year = req.Year; updated = true
						}
						if req.Genre != "" {
							album.Genre = req.Genre; updated = true
						}
						if updated {
							h.albumRepo.Update(r.Context(), album)
						}
					}
				}
			} else {
				// Backward compat: update first album metadata
				var albumID string
				if len(track.Albums) > 0 {
					albumID = track.Albums[0].AlbumID
				}
				if albumID != "" {
					if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil {
						if req.Album != "" {
							album.Title = req.Album
						}
						if req.AlbumMBID != "" {
							album.MBID = req.AlbumMBID
						}
						if req.Year != 0 {
							album.Year = req.Year
						}
						if req.Genre != "" {
							album.Genre = req.Genre
						}
						h.albumRepo.Update(r.Context(), album)
					}
				}
			}
		}

		if err == nil && oldMBID != track.MBID && track.MBID != "" {
			h.reResolveVersions(r.Context(), track.LibraryID, oldMBID, track.MBID, track.ID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *MetadataHandler) SearchTrack(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID string `json:"track_id"`
		Title   string `json:"title"`
		Artist  string `json:"artist"`
		Album   string `json:"album"`
		MBID    string `json:"mbid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	var fileHash string

	// If track_id is provided and metadata is already complete, return DB data directly
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			fileHash = track.Hash
			if track.MBID != "" {
				trackArtists, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID)
				if err == nil && len(trackArtists) > 0 {
					allComplete := true
					var artists []map[string]interface{}
					for _, ta := range trackArtists {
						artist, err := h.artistRepo.FindByID(r.Context(), ta.ArtistID)
						if err != nil || artist.MBID == "" || artist.Country == "" ||
							artist.Name == "" || artist.Name == "Unknown Artist" {
							allComplete = false
							break
						}
						artists = append(artists, map[string]interface{}{
							"name": ta.Artist.Name,
							"mbid": ta.Artist.MBID,
							"role": ta.Role,
						})
					}
					albumID := ""
					if len(track.Albums) > 0 {
						albumID = track.Albums[0].AlbumID
					}
					if allComplete && albumID != "" {
						if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil &&
							album.MBID != "" && album.Country != "" && album.Year != 0 && album.Genre != "" &&
							album.Title != "" && album.Title != "Unknown Album" {
							albums := h.buildTrackAlbums(r.Context(), track)
							writeJSON(w, http.StatusOK, map[string]interface{}{
								"matched":    true,
								"cached":     true,
								"file_hash":  fileHash,
								"title":      track.Title,
								"year":       album.Year,
								"genre":      album.Genre,
								"track_mbid": track.MBID,
								"album_mbid": album.MBID,
								"artists":    artists,
								"albums":     albums,
							})
							return
						}
					}
				}
			}
		}
	}

	// If mbid is provided, do direct MB lookup
	if req.MBID != "" {
		resolver := h.newResolver(r.Context())
		result, err := resolver.IdentifyTrack(r.Context(), req.MBID)
		resolver.Close()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if result != nil {
			var artists []map[string]interface{}
			for _, ar := range result.Artists {
				artists = append(artists, map[string]interface{}{
					"name": ar.Name,
					"mbid": ar.MBID,
					"role": "performer",
				})
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"matched":    true,
				"file_hash":  fileHash,
				"title":      result.Title,
				"album":      result.Album,
				"year":       result.Year,
				"genre":      result.Genre,
				"track_mbid": result.TrackMBID,
				"album_mbid": result.AlbumMBID,
				"artists":    artists,
			})
		} else {
			writeJSON(w, http.StatusOK, map[string]interface{}{"matched": false, "file_hash": fileHash})
		}
		return
	}

	// Check user_metadata cache first
	var cached *repository.UserMetadata
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			fileHash = track.Hash
			if um, err := h.umRepo.FindByUserAndHash(r.Context(), userID, track.Hash); err == nil {
				cached = um
			}
		}
	}

	// If cached data has all key fields, return it directly
	if cached != nil && cached.Title != "" && cached.Artist != "" && cached.Album != "" {
		var artists []map[string]interface{}
		if req.TrackID != "" {
			if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), req.TrackID); err == nil {
				for _, ta := range tas {
					artists = append(artists, map[string]interface{}{
						"name":      ta.Artist.Name,
						"artist_id": ta.ArtistID,
						"mbid":      ta.Artist.MBID,
						"role":      ta.Role,
					})
				}
			}
		}
		resp := map[string]interface{}{
			"matched":    true,
			"cached":     true,
			"file_hash":  fileHash,
			"title":      cached.Title,

			"year":       cached.Year,
			"genre":      cached.Genre,
			"track_mbid": cached.TrackMBID,
			"artists":    artists,
		}
		if req.TrackID != "" {
			if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
				if als := h.buildTrackAlbums(r.Context(), track); len(als) > 0 {
					resp["albums"] = als
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Fill search params from cached data if available
	if cached != nil {
		if req.Title == "" {
			req.Title = cached.Title
		}
		if req.Artist == "" {
			req.Artist = cached.Artist
		}
		if req.Album == "" {
			req.Album = cached.Album
		}
	}

	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}

	resolver := h.newResolver(r.Context())
	result, err := resolver.Enrich(r.Context(), &metadata.AudioMeta{
		Title:  req.Title,
		Artist: req.Artist,
		Album:  req.Album,
	})
	resolver.Close()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		resp := map[string]interface{}{"matched": false, "file_hash": fileHash}
		if req.TrackID != "" {
			if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
				resp["title"] = track.Title
				resp["artists"] = h.buildTrackArtists(r.Context(), track.ID)
				if als := h.buildTrackAlbums(r.Context(), track); len(als) > 0 {
					resp["albums"] = als
				}
		var albumID string
		if len(track.Albums) > 0 {
			albumID = track.Albums[0].AlbumID
		}
		if albumID != "" {
			if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil {
				resp["year"] = album.Year
				resp["genre"] = album.Genre
			}
		}
		if cached != nil {
			if cached.Title != "" {
				resp["title"] = cached.Title
			}
					if cached.Year != 0 {
						resp["year"] = cached.Year
					}
					if cached.Genre != "" {
						resp["genre"] = cached.Genre
					}
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	t := result.Title
	if t == "" {
		t = req.Title
	}
	if cached != nil && cached.Title != "" {
		t = cached.Title
	}
	al := result.Album
	if al == "" {
		al = req.Album
	}
	if cached != nil && cached.Album != "" {
		al = cached.Album
	}
	yr := result.Year
	if cached != nil && cached.Year != 0 {
		yr = cached.Year
	}
	if yr == 0 && req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			var albumID string
			if len(track.Albums) > 0 {
				albumID = track.Albums[0].AlbumID
			}
			if albumID != "" {
				if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil {
					yr = album.Year
				}
			}
		}
	}
	gen := result.Genre
	if cached != nil && cached.Genre != "" {
		gen = cached.Genre
	}
	tmbid := result.TrackMBID
	if cached != nil && cached.TrackMBID != "" {
		tmbid = cached.TrackMBID
	}

	var artists []map[string]interface{}
	for _, ar := range result.Artists {
		artists = append(artists, map[string]interface{}{
			"name": ar.Name,
			"mbid": ar.MBID,
			"role": "performer",
		})
	}

	resp := map[string]interface{}{
		"matched":    true,
		"file_hash":  fileHash,
		"title":      t,
		"year":       yr,
		"genre":      gen,
		"track_mbid": tmbid,
		"album_mbid": result.AlbumMBID,
		"artists":    artists,
	}
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			if als := h.buildTrackAlbums(r.Context(), track); len(als) > 0 {
				resp["albums"] = als
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
func (h *MetadataHandler) Identify(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		TrackID string `json:"track_id"`
		MBID    string `json:"mbid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" || req.MBID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need track_id and mbid"})
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	resolver := h.newResolver(r.Context())
	result, err := resolver.IdentifyTrack(r.Context(), req.MBID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resolver.Close()

	track.MBID = result.TrackMBID
	if result.Title != "" {
		track.Title = metadata.TrimParenSuffix(result.Title)
	}
	if err := h.trackRepo.Update(r.Context(), track); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
		return
	}

	if result.ArtistMBID != "" {
		if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID); err == nil && len(tas) > 0 {
			if artist, err := h.artistRepo.FindByID(r.Context(), tas[0].ArtistID); err == nil && artist.MBID == "" {
				artist.MBID = result.ArtistMBID
				h.artistRepo.Update(r.Context(), artist)
			}
		}
	}

	if result.AlbumMBID != "" {
		var albumID string
		if len(track.Albums) > 0 {
			albumID = track.Albums[0].AlbumID
		}
		if albumID != "" {
			if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil && album.MBID == "" {
				album.MBID = result.AlbumMBID
				if result.Year != 0 {
					album.Year = result.Year
				}
				if result.Genre != "" {
					album.Genre = result.Genre
				}
				h.albumRepo.Update(r.Context(), album)
			}
		}
	}

	resp := map[string]interface{}{
		"track_id": track.ID,
		"mbid":     result.TrackMBID,
		"title":    result.Title,
	}
	if result.Artist != "" {
		resp["artist"] = result.Artist
	}
	if result.Album != "" {
		resp["album"] = result.Album
	}
	if result.Genre != "" {
		resp["genre"] = result.Genre
	}
	if result.Year != 0 {
		resp["year"] = result.Year
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *MetadataHandler) Reidentify(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		TrackID  string `json:"track_id"`
		FileHash string `json:"file_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need track_id"})
		return
	}

	// Delete user metadata cache so fresh data is used next time
	if req.FileHash != "" {
		h.umRepo.DeleteByUserAndHash(r.Context(), userID, req.FileHash)
	}

	track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	// Probe the file to get fresh metadata (same as scanner flow)
	meta, err := metadata.Probe(track.FilePath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to probe file"})
		return
	}
	meta.TitleFromFilename = true

	resolver := h.newResolver(r.Context())
	enrichment, err := resolver.Enrich(r.Context(), meta)
	resolver.Close()

	if enrichment != nil {
		// MB found a match — apply with overwrite (uses MB data)
		scanner.ApplyEnrichment(r.Context(), track, meta, enrichment, track.LibraryID, true, h.trackRepo, h.artistRepo, h.albumRepo)

		track.MBID = enrichment.TrackMBID
		if enrichment.Title != "" {
			track.Title = enrichment.Title
		}
		h.trackRepo.Update(r.Context(), track)

		// Reset track_albums to the enriched album
		if enrichment.AlbumMBID != "" {
			if album, err := h.albumRepo.FindByMBID(r.Context(), enrichment.AlbumMBID); err == nil {
				h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, []*domain.TrackAlbum{{
					AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1,
				}})
			}
		}
	} else {
		// No MB match — apply probe data directly (fresh first-scan state)
		track.MBID = ""
		if meta.Title != "" {
			track.Title = meta.Title
		}
		h.trackRepo.Update(r.Context(), track)

		if len(meta.Artists) > 0 {
			var newArtists []*domain.TrackArtist
			for i, name := range meta.Artists {
				a := h.findOrCreateArtist(r.Context(), name, "")
				if a == nil {
					continue
				}
				newArtists = append(newArtists, &domain.TrackArtist{
					ArtistID:  a.ID,
					Role:      "performer",
					SortOrder: i,
					Artist:    a,
				})
			}
			if len(newArtists) > 0 {
				h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists)
			}
		} else {
			// No artists from probe either — reset to Unknown Artist
			unknown := h.findOrCreateArtist(r.Context(), "Unknown Artist", "")
			if unknown != nil {
				h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, []*domain.TrackArtist{{
					ArtistID:  unknown.ID,
					Role:      "performer",
					SortOrder: 0,
					Artist:    unknown,
				}})
			}
		}

		// Reset track_albums to single "Unknown Album"
		albumName := meta.Album
		if albumName == "" {
			albumName = "Unknown Album"
		}
		artistID := ""
		if len(track.Artists) > 0 {
			artistID = track.Artists[0].ArtistID
		}
		if artistID == "" {
			if a := h.findOrCreateArtist(r.Context(), "Unknown Artist", ""); a != nil {
				artistID = a.ID
			}
		}
		var album *domain.Album
		album, _ = h.albumRepo.FindByName(r.Context(), albumName)
		if album == nil {
			album = &domain.Album{ID: domain.NewID(), Title: albumName, ArtistID: artistID}
			h.albumRepo.BatchCreate(r.Context(), []domain.Album{*album})
		}
		if album != nil {
			h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, []*domain.TrackAlbum{{
				AlbumID:     album.ID,
				TrackNumber: 1,
				DiscNumber:  1,
			}})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *MetadataHandler) SearchArtist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	resolver := h.newResolver(r.Context())
	defer resolver.Close()
	client := h.newMBClient(r.Context())
	artists, _ := client.SearchArtists(req.Name)
	var result []map[string]interface{}
	for _, a := range artists {
		result = append(result, map[string]interface{}{
			"name":    a.Name,
			"mbid":    a.ID,
			"country": a.Country,
			"type":    a.Type,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"artists": result})
}

func (h *MetadataHandler) SearchRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	client := h.newMBClient(r.Context())
	releases, _ := client.SearchReleases(req.Name)
	var result []map[string]interface{}
	// Always prepend the query text as an unmatched entry (no MBID)
	result = append(result, map[string]interface{}{
		"title":  req.Name,
		"mbid":   "",
		"artist": "",
		"status": "",
	})
	for _, rel := range releases {
		artistName := ""
		if len(rel.Artists) > 0 {
			artistName = rel.Artists[0].Name
		}
		result = append(result, map[string]interface{}{
			"title":    rel.Title,
			"mbid":     rel.ID,
			"artist":   artistName,
			"status":   rel.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"releases": result})
}

func (h *MetadataHandler) buildTrackArtists(ctx context.Context, trackID string) []map[string]interface{} {
	tas, err := h.trackRepo.LoadTrackArtists(ctx, trackID)
	if err != nil || len(tas) == 0 {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(tas))
	for i, ta := range tas {
		entry := map[string]interface{}{
			"artist_id": ta.ArtistID,
			"role":      ta.Role,
		}
		if ta.Artist != nil {
			entry["name"] = ta.Artist.Name
			entry["mbid"] = ta.Artist.MBID
		}
		result[i] = entry
	}
	return result
}

func (h *MetadataHandler) buildTrackAlbums(ctx context.Context, track *domain.Track) []map[string]interface{} {
	tals, err := h.trackRepo.LoadTrackAlbums(ctx, track.ID)
	if err != nil || len(tals) == 0 {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(tals))
	for i, tal := range tals {
		entry := map[string]interface{}{
			"id":          tal.AlbumID,
			"track":       tal.TrackNumber,
			"disc_number": tal.DiscNumber,
		}
		if tal.Album != nil {
			entry["title"] = tal.Album.Title
		}
		result[i] = entry
	}
	return result
}

func (h *MetadataHandler) findOrCreateArtist(ctx context.Context, name string, mbid string) *domain.Artist {
	// If MBID is provided, try to look up by MBID first and fetch country
	if mbid != "" {
		if a, err := h.artistRepo.FindByMBID(ctx, mbid); err == nil {
			if a.Name == "" || a.Name == "Unknown Artist" {
				a.Name = name
				a.SortName = name
			}
			if a.Country == "" {
				full, err := h.newMBClient(ctx).LookupArtist(mbid)
				if err == nil && full.Country != "" {
					a.Country = full.Country
				}
			}
			h.artistRepo.Update(ctx, a)
			return a
		}
	}
	// Look up by name
	a, err := h.artistRepo.FindByName(ctx, name)
	if err != nil {
		a = &domain.Artist{
			ID:        domain.NewID(),
			Name:      name,
			SortName:  name,
			MBID:      mbid,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if mbid != "" {
			full, err := h.newMBClient(ctx).LookupArtist(mbid)
			if err == nil {
				a.Country = full.Country
				if a.Name == "" || a.Name == "Unknown Artist" {
					a.Name = full.Name
					a.SortName = full.SortName
				}
			}
		}
		h.artistRepo.BatchCreate(ctx, []domain.Artist{*a})
		a, _ = h.artistRepo.FindByName(ctx, name)
	} else if mbid != "" && a.MBID == "" {
		a.MBID = mbid
		if a.Country == "" {
			full, err := h.newMBClient(ctx).LookupArtist(mbid)
			if err == nil && full.Country != "" {
				a.Country = full.Country
			}
		}
		h.artistRepo.Update(ctx, a)
	}
	return a
}

// reResolveVersions handles version grouping after a track's MBID changes.
func (h *MetadataHandler) reResolveVersions(ctx context.Context, libraryID, oldMBID, newMBID, trackID string) {
	if oldMBID != "" {
		ids := h.mbidGroupIDs(ctx, oldMBID, libraryID)
		if len(ids) < 2 {
			for _, id := range ids {
				h.db.ExecContext(ctx, `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, id)
			}
			h.db.ExecContext(ctx, `DELETE FROM track_version_groups WHERE mbid = $1 AND library_id = $2`, oldMBID, libraryID)
		} else {
			h.renumberGroup(ctx, ids, oldMBID, libraryID)
		}
	}

	ids := h.mbidGroupIDs(ctx, newMBID, libraryID)
	if len(ids) >= 2 {
		h.renumberGroup(ctx, ids, newMBID, libraryID)
	}
}

func (h *MetadataHandler) mbidGroupIDs(ctx context.Context, mbid, libraryID string) []string {
	rows, err := h.db.QueryContext(ctx,
		`SELECT id FROM tracks WHERE mbid = $1 AND library_id = $2 ORDER BY
		 CASE file_format
		 WHEN 'flac' THEN 0 WHEN 'alac' THEN 1 WHEN 'wav' THEN 2
		 WHEN 'aiff' THEN 3 WHEN 'mp3' THEN 4 WHEN 'aac' THEN 5
		 WHEN 'ogg' THEN 6 WHEN 'opus' THEN 7 ELSE 8 END,
		 bit_rate DESC, file_path`, mbid, libraryID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func (h *MetadataHandler) renumberGroup(ctx context.Context, ids []string, mbid, libraryID string) {
	for i, id := range ids {
		version := 1
		if i > 0 {
			version = i + 1
		}
		var existingLabel string
		h.db.QueryRowContext(ctx, `SELECT version_label FROM tracks WHERE id = $1`, id).Scan(&existingLabel)
		if existingLabel == "" {
			existingLabel = scanner.ExtractVersionLabel(ctx, h.db, id)
		}
		h.db.ExecContext(ctx, `UPDATE tracks SET version = $1, version_label = $2 WHERE id = $3`, version, existingLabel, id)
		h.db.ExecContext(ctx,
			`INSERT INTO track_version_groups (mbid, library_id, track_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			mbid, libraryID, id)
	}
}
