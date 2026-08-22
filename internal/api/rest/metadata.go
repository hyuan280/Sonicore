package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/scanner"
	"github.com/sonicore/server/pkg/utils"
)

type MetadataHandler struct {
	db              *sql.DB
	trackRepo       *repository.TrackRepo
	albumRepo       *repository.AlbumRepo
	artistRepo      *repository.ArtistRepo
	umRepo          *repository.UserMetadataRepo
	settingsRepo    *repository.SettingsRepo
	mbCfg           metadata.MBConfig
	mbClient        *metadata.MBClient
	neteaseProvider *netease.Provider
	neteaseEnabled  bool
	entities        *metadata.EntityResolver
	covers          *metadata.CoverManager
}

func NewMetadataHandler(db *sql.DB, mbCfg metadata.MBConfig, mbClient *metadata.MBClient, neteaseProvider *netease.Provider, neteaseEnabled bool, covers *metadata.CoverManager) *MetadataHandler {
	return &MetadataHandler{
		db:              db,
		trackRepo:       repository.NewTrackRepo(db),
		albumRepo:       repository.NewAlbumRepo(db),
		artistRepo:      repository.NewArtistRepo(db),
		umRepo:          repository.NewUserMetadataRepo(db),
		settingsRepo:    repository.NewSettingsRepo(db),
		mbCfg:           mbCfg,
		mbClient:        mbClient,
		neteaseProvider: neteaseProvider,
		neteaseEnabled:  neteaseEnabled,
		entities:        metadata.NewEntityResolver(db),
		covers:          covers,
	}
}

// mbConfig returns the MB config with DB settings overrides (same logic as scanner service).
func (h *MetadataHandler) mbConfig(ctx context.Context) metadata.MBConfig {
	cfg := h.mbCfg
	// Get returns ("", nil) for missing keys; only override when a value is
	// actually stored. Enabled follows the runtime switch so manual identify
	// (SearchTrack/Identify/Reidentify/Save) stays consistent with the covers
	// and scanner paths.
	if enabled, err := h.settingsRepo.Get(ctx, "metadata_musicbrainz_enabled"); err == nil && enabled != "" {
		cfg.Enabled = enabled == "true"
	}
	if url, err := h.settingsRepo.Get(ctx, "metadata_musicbrainz_api_url"); err == nil && url != "" {
		cfg.APIURL = url
	}
	if rl, err := h.settingsRepo.Get(ctx, "metadata_musicbrainz_rate_limit"); err == nil && rl != "" {
		if n, err := strconv.Atoi(rl); err != nil || n <= 0 {
			logger.Warn("[metadata] invalid musicbrainz rate limit %q", rl)
		} else {
			cfg.RateLimit = n
		}
	}
	// Route every MusicBrainz request through the shared client; its config
	// fields are applied inside NewMBClient (MBConfig.Client).
	cfg.Client = h.mbClient
	return cfg
}

func (h *MetadataHandler) newResolver(ctx context.Context) *metadata.Resolver {
	return metadata.NewResolver(h.mbConfig(ctx))
}

func (h *MetadataHandler) newMBClient(ctx context.Context) *metadata.MBClient {
	return metadata.NewMBClient(h.mbConfig(ctx))
}

// newRegistry assembles the recognition chain from the latest settings.
func (h *MetadataHandler) newRegistry(ctx context.Context) *metadata.Registry {
	mbCfg := h.mbConfig(ctx)

	neteaseEnabled := h.neteaseEnabled
	if enabled, err := h.settingsRepo.Get(ctx, "metadata_netease_enabled"); err == nil && enabled != "" {
		neteaseEnabled = enabled == "true"
	}
	var sources []port.MetadataSource
	sources = append(sources, metadata.NewMBSource(mbCfg))
	if neteaseEnabled && h.neteaseProvider != nil {
		sources = append(sources, metadata.NewNeteaseSource(h.neteaseProvider, true))
	}
	sources = append(sources, metadata.NewUserSource(h.umRepo))
	return metadata.BuildRegistry(sources...)
}

// lookupEnrichment resolves an external ID through the registry. The caller
// must provide the source that owns the ID; an empty source falls back to the
// default source (user cache) rather than MusicBrainz, so callers that expect
// external identification must pass an explicit source.
func (h *MetadataHandler) lookupEnrichment(ctx context.Context, source, externalID string) (*metadata.EnrichmentResult, error) {
	if source == "" {
		source = utils.DefaultSource
	}
	c, err := h.newRegistry(ctx).Lookup(ctx, source, externalID)
	if err != nil || c == nil {
		return nil, err
	}
	return metadata.CandidateToEnrichment(c), nil
}

func (h *MetadataHandler) Save(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID      string `json:"track_id"`
		FileHash     string `json:"file_hash"`
		TrackExtID   string `json:"track_external_id"`
		Source       string `json:"source"`
		Title        string `json:"title"`
		Album        string `json:"album"`
		Year         int    `json:"year"`
		Genre        string `json:"genre"`
		AlbumExtID   string `json:"album_external_id"`
		VersionLabel string `json:"version_label"`
		Artists      []struct {
			Name       string `json:"name"`
			ExternalID string `json:"external_id"`
			Source     string `json:"source"`
		} `json:"artists"`
		Albums []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			ExternalID string `json:"external_id"`
			Artist     string `json:"artist"`
			Source     string `json:"source"`
		} `json:"albums"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_hash required"})
		return
	}

	// Reject unknown metadata sources up front so no code path (track update
	// or user-metadata cache) accepts a value that breaks equality checks
	// and version grouping.
	reg := h.newRegistry(r.Context())
	if req.Source != "" && !isValidSource(utils.NormalizeSource(req.Source), reg) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported metadata source"})
		return
	}
	for i := range req.Artists {
		if req.Artists[i].Source != "" {
			src := utils.NormalizeSource(req.Artists[i].Source)
			if !isValidSource(src, reg) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported artist metadata source"})
				return
			}
			req.Artists[i].Source = src
		}
	}
	for i := range req.Albums {
		if req.Albums[i].Source != "" {
			src := utils.NormalizeSource(req.Albums[i].Source)
			if !isValidSource(src, reg) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported album metadata source"})
				return
			}
			req.Albums[i].Source = src
		}
	}

	// The effective source for this save: an explicit request source wins,
	// else the track's current source, else the default source.
	effSource := utils.NormalizeSource(req.Source)
	if effSource == "" {
		effSource = utils.DefaultSource
	}
	if req.Source == "" && req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil && track.MetadataSource != "" {
			effSource = track.MetadataSource
		}
	}

	// If TrackExtID is provided but album data incomplete, look up from MB
	if req.TrackExtID != "" {
		needsEnrich := req.Album == "" || req.AlbumExtID == ""
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
			// Resolve the track through its source: a NetEase external id
			// must not be sent to the MusicBrainz resolver.
			result, err := h.lookupEnrichment(r.Context(), effSource, req.TrackExtID)
			if err != nil {
				logger.Error("[metadata] enrich failed for %s: %v", req.TrackExtID, err)
			} else if result != nil {
				if req.Album == "" && result.Album != "" {
					req.Album = result.Album
				}
				if req.AlbumExtID == "" && result.AlbumExternalID != "" {
					req.AlbumExtID = result.AlbumExternalID
				}
				if req.Year == 0 && result.Year != 0 {
					req.Year = result.Year
				}
				if req.Genre == "" && result.Genre != "" {
					req.Genre = result.Genre
				}
				if len(req.Artists) == 0 || (len(req.Artists) == 1 && req.Artists[0].Name == "Unknown Artist") {
					var filled []struct {
						Name       string `json:"name"`
						ExternalID string `json:"external_id"`
						Source     string `json:"source"`
					}
					for _, ar := range result.Artists {
						filled = append(filled, struct {
							Name       string `json:"name"`
							ExternalID string `json:"external_id"`
							Source     string `json:"source"`
						}{Name: ar.Name, ExternalID: ar.ExternalID, Source: effSource})
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

	// The cache records the source this save is based on (effSource: the
	// request's effective new source, else the track's current source) so the
	// user source re-provides a consistent (source, external id) pair. When
	// no new id is provided but the track already carries one, that id is
	// cached too — caching (source, "") would hand the registry a degenerate
	// pair that could re-identify the track back to an empty id.
	cacheSource := effSource
	cacheExtID := req.TrackExtID
	if cacheExtID == "" && req.TrackID != "" {
		if t, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			// Only reuse the track's current id when it lives in the same
			// namespace as the effective source: caching (newSource, oldId)
			// would feed the registry a pair the track itself no longer
			// carries (its id is cleared on a real source switch below).
			if sourceMatches(t.MetadataSource, effSource) {
				cacheExtID = t.ExternalID
			}
		}
	}

	err := h.umRepo.Upsert(r.Context(), &repository.UserMetadata{
		UserID:         userID,
		FileHash:       req.FileHash,
		MetadataSource: cacheSource,
		ExternalID:     cacheExtID,
		Title:          req.Title,
		Artist:         artistStr,
		Album:          req.Album,
		Year:           req.Year,
		Genre:          req.Genre,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Immediately update all metadata in DB
	if req.TrackID != "" {
		track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
		var oldExtID, oldSource string
		if err == nil {
			oldExtID = track.ExternalID
			oldSource = track.MetadataSource
			if req.TrackExtID != "" {
				// If the title changed, the track was re-identified as a
				// different song. Clear all old aliases so stale entries in
				// external_ids cannot match this track via @> queries.
				if req.Title != "" && track.Title != "" && track.Title != req.Title {
					track.ExternalIDs = map[string]string{}
				}
				// Adopt the effective source BEFORE SetExternalID so the id
				// lands under the correct namespace (SetExternalID keys the
				// alias table on MetadataSource). A new external id for a
				// legacy/empty track adopts effSource.
				if track.MetadataSource != effSource {
					track.MetadataSource = effSource
				}
				track.SetExternalID(req.TrackExtID)
			} else if req.Source != "" {
				newSource := utils.NormalizeSource(req.Source)
				// An empty stored source means a legacy musicbrainz row, so the
				// comparison must go through sourceMatches (like the cache-reuse
				// check above) or a legacy track saving source="musicbrainz"
				// without a new id would be misread as a real source switch and
				// have its MBID cleared.
				if !sourceMatches(track.MetadataSource, newSource) {
					// Real source switch without a new id: the old id lives
					// in the old source's namespace and cannot coexist with
					// the new source, so clear it (and its alias) to keep
					// (source, external_id) consistent. The version-group
					// rows are cleaned up below.
					if track.ExternalID != "" {
						track.SetExternalID("")
					}
					track.MetadataSource = newSource
					// Associated artists/albums must follow the source switch
					// too, or their stale old-namespace ids would keep the
					// SearchTrack completeness check failing forever.
					h.alignAssociatesToSource(r.Context(), track, newSource)
				}
				// req.Source equals the track's current source and no new id
				// was given: nothing to change (the id is left intact).
			}
			if req.Title != "" {
				track.Title = metadata.TrimParenSuffix(req.Title)
			}
			if req.VersionLabel != "" {
				track.VersionLabel = req.VersionLabel
			}
			if err := h.trackRepo.Update(r.Context(), track); err != nil {
				logger.Error("[metadata] save track update error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
				return
			}

			// Update artists association
			if len(req.Artists) == 0 {
				req.Artists = []struct {
					Name       string `json:"name"`
					ExternalID string `json:"external_id"`
					Source     string `json:"source"`
				}{{Name: "Unknown Artist"}}
			}
			var newArtists []*domain.TrackArtist
			for i, ar := range req.Artists {
				a := h.findOrCreateArtist(r.Context(), ar.Name, ar.ExternalID, ar.Source)
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
			resolveArtistID := func(artistName, artistExtID, source string) string {
				if artistExtID != "" {
					if a := h.findOrCreateArtist(r.Context(), artistName, artistExtID, source); a != nil {
						return a.ID
					}
				}
				if artistName != "" {
					if a := h.findOrCreateArtist(r.Context(), artistName, "", source); a != nil {
						return a.ID
					}
				}
				if len(track.Artists) > 0 {
					return track.Artists[0].ArtistID
				}
				if a := h.findOrCreateArtist(r.Context(), "Unknown Artist", "", ""); a != nil {
					return a.ID
				}
				return ""
			}
			// Resolve or create album from request entry.
			// When source is empty the album has no platform source — skip
			// LookupAlbum and save directly with the default source.
			resolveAlbum := func(al struct {
				ID         string `json:"id"`
				Title      string `json:"title"`
				ExternalID string `json:"external_id"`
				Artist     string `json:"artist"`
				Source     string `json:"source"`
			}) (string, bool) {
				if al.ID != "" {
					return al.ID, true
				}
				source := al.Source
				if source == "" {
					source = utils.DefaultSource
				}
				artistID := ""
				var year int
				var country, genre string
				// Look up album details from the platform source when an
				// external ID and a source are both provided.
				if al.ExternalID != "" && source != "" {
					for _, s := range reg.Sources() {
						if s.Name() != source {
							continue
						}
						detail, err := s.LookupAlbum(r.Context(), al.ExternalID)
						if err != nil {
							logger.Error("[metadata] resolveAlbum: %s LookupAlbum(%s) failed: %v", s.Name(), al.ExternalID, err)
						}
						if detail != nil {
							if detail.Year != 0 && year == 0 {
								year = detail.Year
							}
							if detail.Genre != "" {
								genre = detail.Genre
							}
							if detail.Country != "" {
								country = detail.Country
							}
							if detail.ArtistName != "" && artistID == "" {
								artistID = resolveArtistID(detail.ArtistName, detail.ArtistID, source)
							}
						}
						break
					}
				}
				if artistID == "" {
					artistID = resolveArtistID(al.Artist, "", al.Source)
				}
				if artistID == "" {
					return "", false
				}
				if al.ExternalID != "" || al.Title != "" {
					album, err := h.entities.FindAlbum(r.Context(), source, al.ExternalID, al.Title, artistID)
					if err == nil && album != nil {
						updated := false
						if album.Year == 0 && year != 0 {
							album.Year = year
							updated = true
						}
						if album.Country == "" && country != "" {
							album.Country = country
							updated = true
						}
						if album.Genre == "" && genre != "" {
							album.Genre = genre
							updated = true
						}
						if updated {
							h.albumRepo.Update(r.Context(), album)
						}
						return album.ID, true
					}
					album, err = h.entities.FindOrCreateAlbum(r.Context(), source, al.ExternalID, al.Title, artistID, year, genre, country)
					if err == nil {
						return album.ID, true
					}
					logger.Error("[metadata] resolveAlbum: FindOrCreateAlbum failed: %v", err)
					return "", false
				}
				return "", false
			}

			// Process albums: if user sent the field (even empty), replace track_albums
			if req.Albums != nil {
				var trackAlbums []*domain.TrackAlbum
				for _, al := range req.Albums {
					if albumID, ok := resolveAlbum(al); ok {
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{
							AlbumID:     albumID,
							TrackNumber: len(trackAlbums) + 1,
							DiscNumber:  1,
						})
					}
				}
				if len(trackAlbums) == 0 {
					// Fallback: ensure at least Unknown Album
					if album, err := h.albumRepo.FindByName(r.Context(), "Unknown Album"); err == nil {
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1})
					} else {
						artistID := resolveArtistID("", "", "")
						album := &domain.Album{ID: domain.NewID(), Title: "Unknown Album", ArtistID: artistID}
						h.albumRepo.BatchCreate(r.Context(), []domain.Album{*album})
						trackAlbums = append(trackAlbums, &domain.TrackAlbum{AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1})
					}
				}
				if err := h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, trackAlbums); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to save albums: %v", err)})
					return
				}
				// Backfill covers for newly associated albums that lack one
				if h.covers != nil {
					for _, tal := range trackAlbums {
						if album, err := h.albumRepo.FindByID(r.Context(), tal.AlbumID); err == nil && album.CoverImageID == nil && album.Title != "Unknown Album" {
							if err := h.covers.BackfillAlbumCover(r.Context(), album, false); err != nil {
								logger.Info("[metadata] save backfill cover for %s: %v", album.ID, err)
							}
						}
					}
				}
				// Apply user-edited year/genre to first album
				if len(trackAlbums) > 0 && (req.Year != 0 || req.Genre != "") {
					if album, err := h.albumRepo.FindByID(r.Context(), trackAlbums[0].AlbumID); err == nil {
						updated := false
						if req.Year != 0 && album.Year == 0 {
							album.Year = req.Year
							updated = true
						}
						if req.Genre != "" {
							album.Genre = req.Genre
							updated = true
						}
						if updated {
							if err := h.albumRepo.Update(r.Context(), album); err != nil {
								logger.Error("[metadata] save year/genre: Update error: %v", err)
								writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update album year/genre"})
								return
							}
						}
					} else {
						logger.Error("[metadata] save year/genre: FindByID error: %v", err)
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
						if req.AlbumExtID != "" {
							album.ExternalID = req.AlbumExtID
							// Keep the (source, external_id) pair consistent
							// with the effective save source so FindBySourceAndID
							// and version grouping stay aligned.
							if album.MetadataSource != effSource {
								album.MetadataSource = effSource
							}
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

		if err == nil && oldExtID != track.ExternalID && track.ExternalID != "" {
			if err := h.reResolveVersions(r.Context(), track.LibraryID, oldExtID, track.ExternalID, track.ID, oldSource); err != nil {
				logger.Info("[metadata] reResolveVersions: %v", err)
			}
		}
		// A source-only change cleared the id: the old (source, external_id)
		// pair is gone, so drop the track from its previous version group
		// atomically, then renumber the remaining members so no gaps or
		// orphaned secondary versions survive. Every step is error-checked:
		// a partially-failed cleanup must not renumber a group it failed to
		// fully detach the track from.
		if err == nil && oldExtID != "" && track.ExternalID == "" && oldSource != track.MetadataSource {
			cleanupOK := false
			if tx, terr := h.db.BeginTx(r.Context(), nil); terr != nil {
				logger.Info("[metadata] begin version cleanup for %s: %v", track.ID, terr)
			} else {
				if _, e1 := tx.ExecContext(r.Context(), `DELETE FROM track_version_groups WHERE track_id = $1`, track.ID); e1 == nil {
					if _, e2 := tx.ExecContext(r.Context(), `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, track.ID); e2 == nil {
						cleanupOK = tx.Commit() == nil
					} else {
						logger.Info("[metadata] reset version for %s: %v", track.ID, e2)
						tx.Rollback()
					}
				} else {
					logger.Info("[metadata] delete version group rows for %s: %v", track.ID, e1)
					tx.Rollback()
				}
			}
			if !cleanupOK {
				logger.Error("[metadata] version cleanup failed for %s; skipping renumber", track.ID)
			} else {
				// Normalize the old source like reResolveVersions does: legacy
				// empty sources are stored as musicbrainz in the version-group
				// rows, so a raw "" lookup would miss them (and renumberGroup
				// could insert duplicate '' rows).
				normOld := sourceOrDefaultSource(oldSource)
				if ids := h.externalIDGroupIDs(r.Context(), normOld, oldExtID, track.LibraryID); len(ids) >= 1 {
					if err := h.renumberGroup(r.Context(), normOld, ids, oldExtID, track.LibraryID); err != nil {
						logger.Info("[metadata] renumberGroup: %v", err)
					}
				}
			}
		}
		// Cover management: only when metadata actually changed
		if h.covers != nil && (oldExtID != track.ExternalID || req.Title != "") {
			if saved, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
				if oldExtID != saved.ExternalID && saved.CoverImageID != nil {
					if err := h.covers.DeleteTrackCovers(r.Context(), saved.LibraryID, saved.ID); err != nil {
						logger.Info("[metadata] save delete old cover for %s: %v", saved.ID, err)
						writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete old covers"})
						return
					}
					saved.CoverImageID = nil
				}
				if saved.CoverImageID == nil {
					var album *domain.Album
					if len(saved.Albums) > 0 {
						if a, err := h.albumRepo.FindByID(r.Context(), saved.Albums[0].AlbumID); err == nil {
							album = a
						}
					}
					q := port.MetadataQuery{Title: saved.Title}
					if album != nil && album.Title != "" {
						q.Album = album.Title
					}
					if len(saved.Artists) > 0 && saved.Artists[0].Artist != nil {
						q.Artist = saved.Artists[0].Artist.Name
					}
					candidates, cerr := h.newRegistry(r.Context()).SearchCandidates(r.Context(), q)
					if cerr != nil {
						logger.Info("[metadata] save cover search candidates for %s: %v", saved.ID, cerr)
					}
					for _, c := range candidates {
						if c.CoverArtURL != "" && c.Score >= 0.5 {
							if err := h.covers.ImportTrackCoverURL(r.Context(), saved.LibraryID, saved, album, c.CoverArtURL); err != nil {
								logger.Error("[metadata] save import cover error for %s: %v", saved.ID, err)
							}
							break
						}
					}
					if album != nil && album.CoverImageID == nil {
						if err := h.covers.BackfillAlbumCover(r.Context(), album, false); err != nil {
							logger.Error("[metadata] save backfill album cover error for %s: %v", album.ID, err)
						}
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *MetadataHandler) SearchTrack(w http.ResponseWriter, r *http.Request) {
	// AuthMiddleware on the /api subrouter ensures the request is authenticated.
	_ = middleware.GetUserID(r.Context())
	var req struct {
		TrackID    string `json:"track_id"`
		ExternalID string `json:"external_id"`
		Source     string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	var fileHash string

	// If track_id is provided, check DB completeness first
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			fileHash = track.Hash

			// If external_id differs from track's current id, skip DB check
			// and go directly to source lookup.
			if req.ExternalID == "" || req.ExternalID == track.ExternalID {
				// Check DB completeness using TrackHasBasicMeta (title, one
				// non-Unknown artist, one non-Unknown album). External_id and
				// album_external_id are deliberately excluded: if the DB has
				// song/artist/album names but no external IDs, querying a source
				// for those names would produce the same result as the existing
				// search path, so we skip the round-trip and let the user edit
				// manually or search via the external ID field.
				if ok, err := h.trackRepo.TrackHasBasicMeta(r.Context(), track.ID); err != nil {
					logger.Error("[metadata] SearchTrack TrackHasBasicMeta error for %s: %v", track.ID, err)
				} else if ok {
					// DB has all required fields → return directly
					tals, _ := h.trackRepo.LoadTrackAlbums(r.Context(), track.ID)
					resp := map[string]interface{}{
						"matched":           true,
						"cached":            true,
						"file_hash":         fileHash,
						"title":             track.Title,
						"track_external_id": track.ExternalID,
						"source":            track.MetadataSource,
						"artists":           h.buildTrackArtists(r.Context(), track.ID),
						"albums":            h.buildTrackAlbums(r.Context(), track),
					}
					if len(tals) > 0 && tals[0].Album != nil {
						resp["year"] = tals[0].Album.Year
						resp["genre"] = tals[0].Album.Genre
						resp["album_external_id"] = tals[0].Album.ExternalID
					}
					writeJSON(w, http.StatusOK, resp)
					return
				}

				// DB not complete → use track's own external_id if available
				if track.ExternalID != "" && track.MetadataSource != "" {
					source := sourceOrDefaultSource(track.MetadataSource)
					result, err := h.lookupEnrichment(r.Context(), source, track.ExternalID)
					if err != nil {
						logger.Error("[metadata] SearchTrack lookupEnrichment error: %v", err)
						writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to lookup enrichment data"})
						return
					}
					if result != nil {
						var artists []map[string]interface{}
						for _, ar := range result.Artists {
							artists = append(artists, map[string]interface{}{
								"name":        ar.Name,
								"external_id": ar.ExternalID,
								"role":        "performer",
								"source":      source,
							})
						}
						writeJSON(w, http.StatusOK, map[string]interface{}{
							"matched":           true,
							"file_hash":         fileHash,
							"title":             result.Title,
							"album":             result.Album,
							"year":              result.Year,
							"genre":             result.Genre,
							"track_external_id": result.TrackExternalID,
							"album_external_id": result.AlbumExternalID,
							"source":            source,
							"artists":           artists,
							"albums":            h.buildTrackAlbums(r.Context(), track),
						})
						return
					}
				}

				// No external_id on track → return current DB data as-is
				// so the user can see what's there and edit manually or
				// search via the external ID field.
				resp := map[string]interface{}{
					"matched":           true,
					"cached":            true,
					"file_hash":         fileHash,
					"title":             track.Title,
					"track_external_id": track.ExternalID,
					"source":            track.MetadataSource,
					"artists":           h.buildTrackArtists(r.Context(), track.ID),
					"albums":            h.buildTrackAlbums(r.Context(), track),
				}
				tals, _ := h.trackRepo.LoadTrackAlbums(r.Context(), track.ID)
				if len(tals) > 0 && tals[0].Album != nil {
					resp["year"] = tals[0].Album.Year
					resp["genre"] = tals[0].Album.Genre
					resp["album_external_id"] = tals[0].Album.ExternalID
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}
	}

	// If external_id is provided, source is required to avoid mismatching
	// the id against the wrong resolver (e.g. a NetEase id hitting MusicBrainz).
	if req.ExternalID != "" {
		if req.Source == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source required when external_id is provided"})
			return
		}
		source := sourceOrDefaultSource(utils.NormalizeSource(req.Source))
		result, err := h.lookupEnrichment(r.Context(), source, req.ExternalID)
		if err != nil {
			logger.Error("[metadata] SearchTrack lookupEnrichment error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to lookup enrichment data"})
			return
		}
		if result != nil {
			var artists []map[string]interface{}
			for _, ar := range result.Artists {
				artists = append(artists, map[string]interface{}{
					"name":        ar.Name,
					"external_id": ar.ExternalID,
					"role":        "performer",
					"source":      source,
				})
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"matched":           true,
				"file_hash":         fileHash,
				"title":             result.Title,
				"album":             result.Album,
				"year":              result.Year,
				"genre":             result.Genre,
				"track_external_id": result.TrackExternalID,
				"album_external_id": result.AlbumExternalID,
				"source":            source,
				"artists":           artists,
			})
		} else {
			writeJSON(w, http.StatusOK, map[string]interface{}{"matched": false, "file_hash": fileHash})
		}
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_id or external_id required"})
}
func (h *MetadataHandler) Identify(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		TrackID    string `json:"track_id"`
		ExternalID string `json:"external_id"`
		Source     string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" || req.ExternalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need track_id and external_id"})
		return
	}
	track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	// An explicit request source wins; otherwise the id is resolved against
	// the track's current source.
	source := utils.NormalizeSource(req.Source)
	if source == "" {
		source = sourceOrDefaultSource(track.MetadataSource)
	}
	result, err := h.lookupEnrichment(r.Context(), source, req.ExternalID)
	if err != nil {
		logger.Error("[metadata] Identify lookupEnrichment error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to lookup enrichment data"})
		return
	}
	if result == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found in source " + source})
		return
	}

	// A source switch drops the stale alias under the old source first, then
	// the new primary id is adopted under the resolved source.
	if track.MetadataSource != source && track.ExternalID != "" {
		track.SetExternalID("")
	}
	if track.MetadataSource != source {
		track.MetadataSource = source
	}
	if result.TrackExternalID != "" {
		track.SetExternalID(result.TrackExternalID)
	}
	if result.Title != "" {
		track.Title = metadata.TrimParenSuffix(result.Title)
	}
	if err := h.trackRepo.Update(r.Context(), track); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
		return
	}

	if result.ArtistExternalID != "" {
		if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID); err == nil && len(tas) > 0 {
			// Migrate every associated artist whose (source, external_id)
			// pair does not match the just-resolved source: a stale id from
			// another namespace must not survive a source switch. Only the
			// primary artist (i==0) adopts the enriched id; secondary artists
			// have no enriched id, so their stale id is cleared instead.
			for i, ta := range tas {
				artist, err := h.artistRepo.FindByID(r.Context(), ta.ArtistID)
				if err != nil {
					continue
				}
				if artist.ExternalID != "" && sourceMatches(artist.MetadataSource, source) {
					continue // already consistent
				}
				if i == 0 {
					artist.ExternalID = result.ArtistExternalID
				} else if artist.ExternalID != "" {
					artist.ExternalID = ""
				}
				artist.MetadataSource = source
				h.artistRepo.Update(r.Context(), artist)
			}
		}
	}

	if result.AlbumExternalID != "" {
		var albumID string
		if len(track.Albums) > 0 {
			albumID = track.Albums[0].AlbumID
		}
		if albumID != "" {
			if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil &&
				(album.ExternalID == "" || !sourceMatches(album.MetadataSource, source)) {
				album.ExternalID = result.AlbumExternalID
				album.MetadataSource = source
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
		"track_id":    track.ID,
		"external_id": result.TrackExternalID,
		"title":       result.Title,
		"source":      source,
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

	registry := h.newRegistry(r.Context())
	q := metadata.QueryFromAudioMeta(meta)
	if meta.HasLyrics {
		q.FileFields |= port.FileFieldLyrics
	}
	if meta.HasCoverArt {
		q.FileFields |= port.FileFieldCover
	}
	candidate, err := registry.Identify(r.Context(), q)
	if err != nil {
		logger.Error("[metadata] Reidentify Identify error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to identify track"})
		return
	}
	enrichment := metadata.CandidateToEnrichment(candidate)

	if enrichment != nil {
		oldExtID := track.ExternalID
		oldSource := track.MetadataSource
		changed := oldExtID != enrichment.TrackExternalID || !sourceMatches(oldSource, enrichment.Source)

		if changed {
			// The track was re-identified as a different song (different external
			// id or source). Clear all old associations and re-apply from scratch.
			// Reset in-memory fields before re-applying enrichment data.
			// DB associations are replaced atomically by the ReplaceTrack*
			// calls below — each does DELETE + INSERT in a single transaction,
			// so a failure rolls back everything and leaves the old data intact.
			track.Albums = nil
			track.Artists = nil
			track.ExternalID = ""
			track.ExternalIDs = map[string]string{}
			track.MetadataSource = ""
			track.Version = 0
			track.VersionLabel = ""
			track.CoverImageID = nil

			// Re-apply artists from enrichment
			var newArtists []*domain.TrackArtist
			for i, ar := range enrichment.Artists {
				a := h.findOrCreateArtist(r.Context(), ar.Name, ar.ExternalID, enrichment.Source)
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
			if len(newArtists) == 0 {
				unknown := h.findOrCreateArtist(r.Context(), "Unknown Artist", "", "")
				if unknown != nil {
					newArtists = append(newArtists, &domain.TrackArtist{
						ArtistID:  unknown.ID,
						Role:      "performer",
						SortOrder: 0,
						Artist:    unknown,
					})
				}
			}
			// ReplaceTrackArtists does DELETE + INSERT atomically.
			// When newArtists is empty it only deletes; when non-empty it
			// replaces — either way, a failure rolls back the entire operation.
			if err := h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists); err != nil {
				logger.Error("[metadata] reidentify set artists error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update artists"})
				return
			}
			track.Artists = nil // prevent Update from duplicating the work

			// Re-apply album from enrichment
			var trackAlbum []*domain.TrackAlbum
			if enrichment.AlbumExternalID != "" {
				if album, err := h.albumRepo.FindBySourceAndID(r.Context(), enrichment.Source, enrichment.AlbumExternalID); err == nil {
					trackAlbum = []*domain.TrackAlbum{{
						AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1,
					}}
				} else {
					logger.Error("[metadata] reidentify album: FindBySourceAndID(%q,%q) failed: %v", enrichment.Source, enrichment.AlbumExternalID, err)
					// Fallback: create album via enrichment data
					artistID := ""
					if len(enrichment.Artists) > 0 {
						if a := h.findOrCreateArtist(r.Context(), enrichment.Artists[0].Name, enrichment.Artists[0].ExternalID, enrichment.Source); a != nil {
							artistID = a.ID
						}
					}
					if album, err := h.entities.FindOrCreateAlbum(r.Context(), enrichment.Source, enrichment.AlbumExternalID, enrichment.Album, artistID, 0, "", ""); err == nil {
						trackAlbum = []*domain.TrackAlbum{{
							AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1,
						}}
					}
				}
			} else {
				// enrichment.AlbumExternalID is empty — no album to restore
			}
			// ReplaceTrackAlbums does DELETE + INSERT atomically.
			// When trackAlbum is nil it only deletes; when non-empty it replaces.
			if err := h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, trackAlbum); err != nil {
				logger.Error("[metadata] reidentify album: ReplaceTrackAlbums failed: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update album"})
				return
			}
			track.Albums = nil // prevent Update from duplicating the work

			// Update track metadata
			track.MetadataSource = enrichment.Source
			if enrichment.TrackExternalID != "" {
				track.SetExternalID(enrichment.TrackExternalID)
			}
			if enrichment.Title != "" {
				track.Title = enrichment.Title
			}
			if err := h.trackRepo.Update(r.Context(), track); err != nil {
				logger.Error("[metadata] reidentify (changed) update error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
				return
			}

			// Re-resolve version groups
			if err := h.reResolveVersions(r.Context(), track.LibraryID, oldExtID, track.ExternalID, track.ID, oldSource); err != nil {
				logger.Error("[metadata] reidentify reResolveVersions error: %v", err)
			}

			// Delete old covers before fetching new ones
			if h.covers != nil {
				if err := h.covers.DeleteTrackCovers(r.Context(), track.LibraryID, track.ID); err != nil {
					logger.Info("[metadata] reidentify delete old covers for %s: %v", track.ID, err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete old covers"})
					return
				}
			}

			// Fetch cover from platform
			if h.covers != nil {
				var album *domain.Album
				if tals, err := h.trackRepo.LoadTrackAlbums(r.Context(), track.ID); err == nil && len(tals) > 0 {
					if a, err := h.albumRepo.FindByID(r.Context(), tals[0].AlbumID); err == nil {
						album = a
					}
				}
				q := port.MetadataQuery{Title: track.Title}
				if album != nil && album.Title != "" {
					q.Album = album.Title
				}
				if len(track.Artists) > 0 && track.Artists[0].Artist != nil {
					q.Artist = track.Artists[0].Artist.Name
				}
				candidates, cerr := h.newRegistry(r.Context()).SearchCandidates(r.Context(), q)
				if cerr != nil {
					logger.Info("[metadata] reidentify cover search candidates for %s: %v", track.ID, cerr)
				}
				for _, c := range candidates {
					if c.CoverArtURL != "" && c.Score >= 0.5 {
						if err := h.covers.ImportTrackCoverURL(r.Context(), track.LibraryID, track, album, c.CoverArtURL); err != nil {
							logger.Error("[metadata] reidentify cover import error for %s: %v", track.ID, err)
						}
						break
					}
				}
				if album != nil && album.CoverImageID == nil {
					if err := h.covers.BackfillAlbumCover(r.Context(), album, false); err != nil {
						logger.Info("[metadata] reidentify backfill album cover for %s: %v", album.ID, err)
					}
				}
			}
		} else {
			// Same external id and source — apply enrichment incrementally.
			scanner.ApplyEnrichment(r.Context(), track, meta, enrichment, true, h.trackRepo, h.artistRepo, h.albumRepo, h.entities)
			if track.MetadataSource != enrichment.Source && track.ExternalID != "" {
				track.SetExternalID("")
			}
			track.MetadataSource = enrichment.Source
			if enrichment.TrackExternalID != "" {
				track.SetExternalID(enrichment.TrackExternalID)
			}
			if enrichment.Title != "" {
				track.Title = enrichment.Title
			}
			if err := h.trackRepo.Update(r.Context(), track); err != nil {
				logger.Error("[metadata] reidentify (incremental) update error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
				return
			}

			// Reset track_albums to the enriched album
			if enrichment.AlbumExternalID != "" {
				if album, err := h.albumRepo.FindBySourceAndID(r.Context(), enrichment.Source, enrichment.AlbumExternalID); err == nil {
					if err := h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, []*domain.TrackAlbum{{
						AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1,
					}}); err != nil {
						logger.Error("[metadata] reidentify (incremental) replace albums error: %v", err)
						writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update album"})
						return
					}
				}
			}

			// Restore cover
			if track.CoverImageID == nil && h.covers != nil {
				var album *domain.Album
				if len(track.Albums) > 0 {
					album, err = h.albumRepo.FindByID(r.Context(), track.Albums[0].AlbumID)
					if err != nil {
						logger.Error("[metadata] reidentify album load error for %s: %v", track.ID, err)
						album = nil
					}
				}
				if err := h.covers.EnsureTrackCover(r.Context(), track.LibraryID, track, album, true, true); err != nil {
					logger.Error("[metadata] reidentify cover ensure error for %s: %v", track.ID, err)
				}
			}
		}
	} else {
		// No MB match — apply probe data directly (fresh first-scan state).
		// The source is reset too: a stale source with an empty id would form
		// a degenerate (source, "") pair that pollutes source-inferred lookups,
		// and every alias is dropped so no stale id can match later.
		track.ExternalID = ""
		track.MetadataSource = ""
		track.ExternalIDs = map[string]string{}
		if meta.Title != "" {
			track.Title = meta.Title
		}
		if err := h.trackRepo.Update(r.Context(), track); err != nil {
			logger.Error("[metadata] reidentify (no-match) update error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
			return
		}

		if len(meta.Artists) > 0 {
			var newArtists []*domain.TrackArtist
			for i, name := range meta.Artists {
				a := h.findOrCreateArtist(r.Context(), name, "", "")
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
				if err := h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists); err != nil {
					logger.Error("[metadata] reidentify (no-match) set artists error: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update artists"})
					return
				}
			}
		} else {
			// No artists from probe either — reset to Unknown Artist
			unknown := h.findOrCreateArtist(r.Context(), "Unknown Artist", "", "")
			if unknown != nil {
				if err := h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, []*domain.TrackArtist{{
					ArtistID:  unknown.ID,
					Role:      "performer",
					SortOrder: 0,
					Artist:    unknown,
				}}); err != nil {
					logger.Error("[metadata] reidentify (no-match) set unknown artist error: %v", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update artists"})
					return
				}
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
			if a := h.findOrCreateArtist(r.Context(), "Unknown Artist", "", ""); a != nil {
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
			if err := h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, []*domain.TrackAlbum{{
				AlbumID:     album.ID,
				TrackNumber: 1,
				DiscNumber:  1,
			}}); err != nil {
				logger.Error("[metadata] reidentify (no-match) replace albums error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update album"})
				return
			}
			if album.CoverImageID == nil && album.Title != "Unknown Album" && h.covers != nil {
				if err := h.covers.BackfillAlbumCover(r.Context(), album, false); err != nil {
					logger.Info("[metadata] reidentify backfill cover for %s: %v", album.ID, err)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *MetadataHandler) SearchArtist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	source := utils.NormalizeSource(req.Source)
	reg := h.newRegistry(r.Context())

	var result []port.ArtistSearchResult
	for _, s := range reg.Sources() {
		if source != "" && s.Name() != source {
			continue
		}
		artists, err := s.SearchArtists(r.Context(), req.Name)
		if err != nil {
			logger.Error("[metadata] SearchArtist %s error: %v", s.Name(), err)
			continue
		}
		result = append(result, artists...)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"artists": result})
}

func (h *MetadataHandler) SearchRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	source := utils.NormalizeSource(req.Source)
	reg := h.newRegistry(r.Context())

	result := []port.ReleaseSearchResult{{
		Title:  req.Name,
		Source: "",
	}}
	for _, s := range reg.Sources() {
		if source != "" && s.Name() != source {
			continue
		}
		releases, err := s.SearchReleases(r.Context(), req.Name)
		if err != nil {
			logger.Error("[metadata] SearchRelease %s error: %v", s.Name(), err)
			continue
		}
		result = append(result, releases...)
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
			entry["external_id"] = ta.Artist.ExternalID
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
			entry["external_id"] = tal.Album.ExternalID
			entry["source"] = tal.Album.MetadataSource
			entry["year"] = tal.Album.Year
			if tal.Album.Genre != "" {
				entry["genre"] = tal.Album.Genre
			}
		}
		result[i] = entry
	}
	return result
}

func (h *MetadataHandler) findOrCreateArtist(ctx context.Context, name string, externalID, source string) *domain.Artist {
	// Resolve through the shared cross-source chain (primary ID → alias →
	// normalized name → create).
	a, err := h.entities.FindOrCreateArtist(ctx, source, externalID, name, "")
	if err != nil {
		return nil
	}

	// Backfill country from the source when the artist has an external ID
	// but no country yet.
	if externalID != "" && a.Country == "" && source != "" {
		for _, s := range h.newRegistry(ctx).Sources() {
			if s.Name() != source {
				continue
			}
			detail, err := s.LookupArtist(ctx, externalID)
			if err != nil {
				logger.Error("[metadata] LookupArtist %s error: %v", source, err)
				break
			}
			if detail != nil && detail.Country != "" {
				a.Country = detail.Country
				h.artistRepo.Update(ctx, a)
			}
			break
		}
	}
	return a
}

func (h *MetadataHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	reg := h.newRegistry(r.Context())
	var sources []map[string]string
	for _, s := range reg.Sources() {
		sources = append(sources, map[string]string{"name": s.Name(), "label": s.Label()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func sourceOrDefaultSource(s string) string {
	if s == "" {
		return utils.DefaultSource
	}
	return s
}

// sourceMatches reports whether two metadata source values are the same,
// treating an empty value as the default source.
func sourceMatches(a, b string) bool {
	return sourceOrDefaultSource(a) == sourceOrDefaultSource(b)
}

// alignAssociatesToSource rewrites the (MetadataSource, ExternalID) pair of
// every associated artist/album that still carries an id from another source
// namespace, so a source switch leaves no stale-namespace id behind (the
// track itself has no new id to hand out, so the stale ids are cleared).
func (h *MetadataHandler) alignAssociatesToSource(ctx context.Context, track *domain.Track, source string) {
	for _, ta := range track.Artists {
		if ta == nil || ta.ArtistID == "" {
			continue
		}
		artist, err := h.artistRepo.FindByID(ctx, ta.ArtistID)
		if err != nil || artist.ExternalID == "" || sourceMatches(artist.MetadataSource, source) {
			continue
		}
		artist.MetadataSource = source
		artist.ExternalID = ""
		h.artistRepo.Update(ctx, artist)
	}
	for _, tal := range track.Albums {
		if tal == nil || tal.AlbumID == "" {
			continue
		}
		album, err := h.albumRepo.FindByID(ctx, tal.AlbumID)
		if err != nil || album.ExternalID == "" || sourceMatches(album.MetadataSource, source) {
			continue
		}
		album.MetadataSource = source
		album.ExternalID = ""
		h.albumRepo.Update(ctx, album)
	}
}

func isValidSource(s string, reg *metadata.Registry) bool {
	if s == "" {
		return false
	}
	for _, src := range reg.Sources() {
		if src.Name() == s {
			return true
		}
	}
	return false
}

// reResolveVersions handles version grouping after a track's external id
// changes. oldSource is the metadata source the track had before this save;
// when it differs from the new source the old group rows are cleaned without
// a source predicate so no stale (source, external id) rows survive.
func (h *MetadataHandler) reResolveVersions(ctx context.Context, libraryID, oldExtID, newExtID, trackID, oldSource string) error {
	// The previous source may have been empty on legacy rows; normalize it
	// the same way the new source is normalized below.
	oldSource = sourceOrDefaultSource(oldSource)
	newSource, err := h.trackSource(ctx, trackID)
	if err != nil {
		return fmt.Errorf("read source for %s: %w", trackID, err)
	}
	if oldExtID != "" {
		ids := h.externalIDGroupIDs(ctx, oldSource, oldExtID, libraryID)
		if len(ids) < 2 {
			for _, id := range ids {
				h.db.ExecContext(ctx, `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, id)
			}
			// The source may have changed in this same save; drop every
			// group row for the old external id regardless of source.
			h.db.ExecContext(ctx, `DELETE FROM track_version_groups WHERE external_id = $1 AND library_id = $2`, oldExtID, libraryID)
		} else {
			if err := h.renumberGroup(ctx, oldSource, ids, oldExtID, libraryID); err != nil {
				return fmt.Errorf("renumber old group %s/%s: %w", oldSource, oldExtID, err)
			}
		}
	}

	ids := h.externalIDGroupIDs(ctx, newSource, newExtID, libraryID)
	if len(ids) >= 2 {
		if err := h.renumberGroup(ctx, newSource, ids, newExtID, libraryID); err != nil {
			return fmt.Errorf("renumber new group %s/%s: %w", newSource, newExtID, err)
		}
	} else if len(ids) == 1 {
		// Single track with this external id: reset version so it shows
		// in the main list instead of being hidden as a secondary version.
		if _, err := h.db.ExecContext(ctx, `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, ids[0]); err != nil {
			return fmt.Errorf("reset version for %s: %w", ids[0], err)
		}
	}
	return nil
}

// trackSource reads the metadata source of a track. A missing track is not
// an error at this point; the value defaults to musicbrainz on empty.
func (h *MetadataHandler) trackSource(ctx context.Context, trackID string) (string, error) {
	var source string
	if err := h.db.QueryRowContext(ctx, `SELECT metadata_source FROM tracks WHERE id = $1`, trackID).Scan(&source); err != nil {
		return "", err
	}
	return sourceOrDefaultSource(source), nil
}

func (h *MetadataHandler) externalIDGroupIDs(ctx context.Context, source, externalID, libraryID string) []string {
	rows, err := h.db.QueryContext(ctx,
		`SELECT id FROM tracks WHERE metadata_source = $1 AND external_id = $2 AND library_id = $3 ORDER BY
		 CASE file_format
		 WHEN 'flac' THEN 0 WHEN 'alac' THEN 1 WHEN 'wav' THEN 2
		 WHEN 'aiff' THEN 3 WHEN 'mp3' THEN 4 WHEN 'aac' THEN 5
		 WHEN 'ogg' THEN 6 WHEN 'opus' THEN 7 ELSE 8 END,
		 bit_rate DESC, file_path`, source, externalID, libraryID)
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

func (h *MetadataHandler) renumberGroup(ctx context.Context, source string, ids []string, externalID, libraryID string) error {
	if len(ids) == 0 {
		return nil
	}

	infoMap, err := h.loadVersionTrackInfos(ctx, ids)
	if err != nil {
		return fmt.Errorf("load version track infos: %w", err)
	}

	updates := make([]scanner.VersionUpdate, 0, len(ids))
	inserts := make([]scanner.VersionGroupInsert, 0, len(ids))

	for i, id := range ids {
		version := 1
		if i > 0 {
			version = i + 1
		}
		info, ok := infoMap[id]
		label := ""
		if ok {
			label = info.VersionLabel
		}
		if label == "" {
			if ok {
				label = scanner.ExtractVersionLabelFromInfo(info)
			} else {
				label, err = scanner.ExtractVersionLabel(ctx, h.db, id)
				if err != nil {
					return fmt.Errorf("fallback version label for %s: %w", id, err)
				}
			}
		}
		updates = append(updates, scanner.VersionUpdate{ID: id, Version: version, Label: label})
		inserts = append(inserts, scanner.VersionGroupInsert{Source: source, ExternalID: externalID, LibraryID: libraryID, TrackID: id})
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := scanner.BatchUpdateVersionLabels(ctx, tx, updates); err != nil {
		return fmt.Errorf("batch update version labels: %w", err)
	}
	if err := scanner.BatchInsertVersionGroups(ctx, tx, inserts); err != nil {
		return fmt.Errorf("batch insert version groups: %w", err)
	}

	return tx.Commit()
}

func (h *MetadataHandler) loadVersionTrackInfos(ctx context.Context, ids []string) (map[string]scanner.TrackVersionInfo, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT t.id, t.file_path, t.file_format, t.bit_rate, t.title, COALESCE(t.version_label, ''),
		        COALESCE((SELECT string_agg(a2.name, ',' ORDER BY ta2.sort_order)
		                  FROM track_artists ta2 JOIN artists a2 ON a2.id = ta2.artist_id
		                  WHERE ta2.track_id = t.id), ''),
		        COALESCE((SELECT al.title FROM track_albums tal JOIN albums al ON al.id = tal.album_id WHERE tal.track_id = t.id ORDER BY tal.disc_number, tal.track_number LIMIT 1), '')
		 FROM tracks t WHERE t.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	infoMap := make(map[string]scanner.TrackVersionInfo, len(ids))
	for rows.Next() {
		var info scanner.TrackVersionInfo
		if err := rows.Scan(&info.ID, &info.FilePath, &info.FileFormat, &info.BitRate, &info.Title, &info.VersionLabel, &info.Artist, &info.Album); err != nil {
			return nil, fmt.Errorf("scan version track info: %w", err)
		}
		infoMap[info.ID] = info
	}
	return infoMap, rows.Err()
}
