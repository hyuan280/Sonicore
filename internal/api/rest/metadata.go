package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
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
			log.Printf("[metadata] invalid musicbrainz rate limit %q", rl)
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

// newRegistry assembles the recognition chain from the latest settings:
// MusicBrainz first, NetEase as the fallback when both the metadata switch
// and the platform provider are available.
func (h *MetadataHandler) newRegistry(ctx context.Context) *metadata.Registry {
	mbCfg := h.mbConfig(ctx)

	neteaseEnabled := h.neteaseEnabled
	// Get returns ("", nil) for missing keys; only override when a value
	// is actually stored.
	if enabled, err := h.settingsRepo.Get(ctx, "metadata_netease_enabled"); err == nil && enabled != "" {
		neteaseEnabled = enabled == "true"
	}
	return metadata.BuildRegistry(mbCfg, h.neteaseProvider, neteaseEnabled, h.umRepo)
}

// lookupEnrichment resolves an external ID through the registry, using the
// track's own metadata source so NetEase-sourced tracks look up NetEase ids
// (and legacy rows default to MusicBrainz).
func (h *MetadataHandler) lookupEnrichment(ctx context.Context, source, externalID string) (*metadata.EnrichmentResult, error) {
	c, err := h.newRegistry(ctx).Lookup(ctx, utils.SourceOrDefault(source), externalID)
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
	if req.Source != "" && !isValidSource(utils.NormalizeSource(req.Source)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported metadata source"})
		return
	}
	for i := range req.Artists {
		if req.Artists[i].Source != "" {
			src := utils.NormalizeSource(req.Artists[i].Source)
			if !isValidSource(src) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported artist metadata source"})
				return
			}
			req.Artists[i].Source = src
		}
	}
	for i := range req.Albums {
		if req.Albums[i].Source != "" {
			src := utils.NormalizeSource(req.Albums[i].Source)
			if !isValidSource(src) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported album metadata source"})
				return
			}
			req.Albums[i].Source = src
		}
	}

	// The effective source for this save: an explicit request source wins,
	// else the track's current source, else the MusicBrainz default. It is
	// used consistently by the enrichment lookup, the user-metadata cache and
	// the track row update so (source, external_id) pairs never mismatch.
	effSource := utils.SourceOrDefault(utils.NormalizeSource(req.Source))
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
				log.Printf("[metadata] enrich failed for %s: %v", req.TrackExtID, err)
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
			h.trackRepo.Update(r.Context(), track)

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
			// Resolve or create album from request entry
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
					source = metadata.SourceMusicBrainz
				}
				artistID := ""
				var year int
				var country, genre string
				if al.ExternalID != "" && source == metadata.SourceMusicBrainz {
					if release, err := h.newMBClient(r.Context()).LookupRelease(r.Context(), al.ExternalID); err == nil {
						if len(release.Date) >= 4 {
							fmt.Sscanf(release.Date[:4], "%d", &year)
						}
						country = release.Country
						genre = metadata.GenreFromTags(release.Tags)
						if len(release.Artists) > 0 {
							extID := ""
							if release.Artists[0].Artist != nil {
								extID = release.Artists[0].Artist.ID
							}
							artistID = resolveArtistID(release.Artists[0].Name, extID, metadata.SourceMusicBrainz)
						}
					}
				}
				if artistID == "" {
					// al.ExternalID is the album's id, not an artist id —
					// passing it here would corrupt the artist's external id,
					// so fall back to the name-based lookup/create only.
					artistID = resolveArtistID(al.Artist, "", al.Source)
				}
				if artistID == "" {
					return "", false
				}
				if al.ExternalID != "" || al.Title != "" {
					album, err := h.entities.FindAlbum(r.Context(), source, al.ExternalID, al.Title, artistID)
					if err == nil && album != nil {
						// Update missing metadata
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
			h.reResolveVersions(r.Context(), track.LibraryID, oldExtID, track.ExternalID, track.ID, oldSource)
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
				log.Printf("[metadata] begin version cleanup for %s: %v", track.ID, terr)
			} else {
				if _, e1 := tx.ExecContext(r.Context(), `DELETE FROM track_version_groups WHERE track_id = $1`, track.ID); e1 == nil {
					if _, e2 := tx.ExecContext(r.Context(), `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, track.ID); e2 == nil {
						cleanupOK = tx.Commit() == nil
					} else {
						log.Printf("[metadata] reset version for %s: %v", track.ID, e2)
						tx.Rollback()
					}
				} else {
					log.Printf("[metadata] delete version group rows for %s: %v", track.ID, e1)
					tx.Rollback()
				}
			}
			if !cleanupOK {
				log.Printf("[metadata] version cleanup failed for %s; skipping renumber", track.ID)
			} else {
				// Normalize the old source like reResolveVersions does: legacy
				// empty sources are stored as musicbrainz in the version-group
				// rows, so a raw "" lookup would miss them (and renumberGroup
				// could insert duplicate '' rows).
				normOld := sourceOrDefaultSource(oldSource)
				if ids := h.externalIDGroupIDs(r.Context(), normOld, oldExtID, track.LibraryID); len(ids) >= 1 {
					h.renumberGroup(r.Context(), normOld, ids, oldExtID, track.LibraryID)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *MetadataHandler) SearchTrack(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID    string `json:"track_id"`
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		Album      string `json:"album"`
		ExternalID string `json:"external_id"`
		Source     string `json:"source"`
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
			if track.ExternalID != "" {
				trackArtists, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID)
				if err == nil && len(trackArtists) > 0 {
					allComplete := true
					var artists []map[string]interface{}
					for _, ta := range trackArtists {
						artist, err := h.artistRepo.FindByID(r.Context(), ta.ArtistID)
						if err != nil || artist.ExternalID == "" || artist.Country == "" ||
							artist.Name == "" || artist.Name == "Unknown Artist" ||
							!sourceMatches(artist.MetadataSource, track.MetadataSource) {
							allComplete = false
							break
						}
						artists = append(artists, map[string]interface{}{
							"name":        ta.Artist.Name,
							"external_id": ta.Artist.ExternalID,
							"role":        ta.Role,
						})
					}
					albumID := ""
					if len(track.Albums) > 0 {
						albumID = track.Albums[0].AlbumID
					}
					if allComplete && albumID != "" {
						if album, err := h.albumRepo.FindByID(r.Context(), albumID); err == nil &&
							album.ExternalID != "" && album.Country != "" && album.Year != 0 && album.Genre != "" &&
							album.Title != "" && album.Title != "Unknown Album" &&
							sourceMatches(album.MetadataSource, track.MetadataSource) {
							albums := h.buildTrackAlbums(r.Context(), track)
							writeJSON(w, http.StatusOK, map[string]interface{}{
								"matched":           true,
								"cached":            true,
								"file_hash":         fileHash,
								"title":             track.Title,
								"year":              album.Year,
								"genre":             album.Genre,
								"track_external_id": track.ExternalID,
								"album_external_id": album.ExternalID,
								"source":            track.MetadataSource,
								"artists":           artists,
								"albums":            albums,
							})
							return
						}
					}
				}
			}
		}
	}

	// If external_id is provided, resolve it against the explicit request
	// source, else the track's source when known (a NetEase-sourced track
	// carries a platform id), else MusicBrainz.
	if req.ExternalID != "" {
		source := req.Source
		if source != "" && !isValidSource(utils.NormalizeSource(source)) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported metadata source"})
			return
		}
		if source == "" && req.TrackID != "" {
			if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil && track.MetadataSource != "" {
				source = track.MetadataSource
			}
		}
		if source == "" {
			source = utils.SourceMusicBrainz
		}
		source = utils.SourceOrDefault(utils.NormalizeSource(source))
		result, err := h.lookupEnrichment(r.Context(), source, req.ExternalID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if result != nil {
			var artists []map[string]interface{}
			for _, ar := range result.Artists {
				artists = append(artists, map[string]interface{}{
					"name":        ar.Name,
					"external_id": ar.ExternalID,
					"role":        "performer",
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
				"artists":           artists,
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
						"name":        ta.Artist.Name,
						"artist_id":   ta.ArtistID,
						"external_id": ta.Artist.ExternalID,
						"role":        ta.Role,
					})
				}
			}
		}
		resp := map[string]interface{}{
			"matched":           true,
			"cached":            true,
			"file_hash":         fileHash,
			"title":             cached.Title,
			"source":            utils.SourceOrDefault(cached.MetadataSource),
			"year":              cached.Year,
			"genre":             cached.Genre,
			"track_external_id": cached.ExternalID,
			"artists":           artists,
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

	registry := h.newRegistry(r.Context())
	candidate, err := registry.Identify(r.Context(), port.MetadataQuery{
		Title:  req.Title,
		Artist: req.Artist,
		Album:  req.Album,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := metadata.CandidateToEnrichment(candidate)

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
	tExtID := result.TrackExternalID
	extSource := result.Source
	if cached != nil && cached.ExternalID != "" {
		// The cached id lives in the cache record's namespace; the source
		// must follow it or the response would hand back a mismatched
		// (source, track_external_id) pair that Save would persist under the
		// wrong namespace.
		tExtID = cached.ExternalID
		extSource = utils.SourceOrDefault(cached.MetadataSource)
	}

	var artists []map[string]interface{}
	for _, ar := range result.Artists {
		artists = append(artists, map[string]interface{}{
			"name":        ar.Name,
			"external_id": ar.ExternalID,
			"role":        "performer",
		})
	}

	resp := map[string]interface{}{
		"matched":           true,
		"file_hash":         fileHash,
		"title":             t,
		"year":              yr,
		"genre":             gen,
		"track_external_id": tExtID,
		"album_external_id": result.AlbumExternalID,
		"artists":           artists,
		"source":            extSource,
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
		TrackID    string `json:"track_id"`
		ExternalID string `json:"external_id"`
		Source     string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" || req.ExternalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need track_id and external_id"})
		return
	}
	if req.Source != "" && !isValidSource(utils.NormalizeSource(req.Source)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported metadata source"})
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	// An explicit request source (e.g. a NetEase id on a legacy/empty track)
	// wins; otherwise the id is resolved against the track's current source.
	source := utils.SourceOrDefault(utils.NormalizeSource(req.Source))
	if req.Source == "" {
		source = utils.SourceOrDefault(track.MetadataSource)
	}
	result, err := h.lookupEnrichment(r.Context(), source, req.ExternalID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	enrichment := metadata.CandidateToEnrichment(candidate)

	if enrichment != nil {
		// A source matched — apply with overwrite. The metadata source
		// follows the producing source so version grouping and completeness
		// checks see a consistent (source, external id) pair.
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
		h.trackRepo.Update(r.Context(), track)

		// Reset track_albums to the enriched album (scoped to the producing
		// source — a NetEase album id must not be looked up as a MusicBrainz
		// release).
		if enrichment.AlbumExternalID != "" {
			if album, err := h.albumRepo.FindBySourceAndID(r.Context(), enrichment.Source, enrichment.AlbumExternalID); err == nil {
				h.trackRepo.ReplaceTrackAlbums(r.Context(), track.ID, []*domain.TrackAlbum{{
					AlbumID: album.ID, TrackNumber: 1, DiscNumber: 1,
				}})
			}
		}

		// Restore a deleted cover through the unified ensure flow (embedded
		// first, then the platform chain via the track's source). This branch
		// only runs when the chain matched (searchPlatform=true); the
		// no-match path applies fresh probe data and does not restore covers.
		if track.CoverImageID == nil && h.covers != nil {
			var album *domain.Album
			if len(track.Albums) > 0 {
				album, err = h.albumRepo.FindByID(r.Context(), track.Albums[0].AlbumID)
				if err != nil {
					// A real DB failure must not be hidden behind a nil album.
					log.Printf("[metadata] reidentify album load error for %s: %v", track.ID, err)
					album = nil
				}
			}
			if err := h.covers.EnsureTrackCover(r.Context(), track.LibraryID, track, album, true, true); err != nil {
				log.Printf("[metadata] reidentify cover ensure error for %s: %v", track.ID, err)
			}
		}
	} else {
		// No MB match — apply probe data directly (fresh first-scan state).
		// The source is reset too: a stale source with an empty id would form
		// a degenerate (source, "") pair that pollutes source-inferred lookups,
		// and every alias is dropped so no stale id can match later.
		track.ExternalID = ""
		track.MetadataSource = metadata.SourceMusicBrainz
		track.ExternalIDs = nil
		if meta.Title != "" {
			track.Title = meta.Title
		}
		h.trackRepo.Update(r.Context(), track)

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
				h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists)
			}
		} else {
			// No artists from probe either — reset to Unknown Artist
			unknown := h.findOrCreateArtist(r.Context(), "Unknown Artist", "", "")
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
	artists, _ := client.SearchArtists(r.Context(), req.Name)
	var result []map[string]interface{}
	for _, a := range artists {
		result = append(result, map[string]interface{}{
			"name":        a.Name,
			"external_id": a.ID,
			"country":     a.Country,
			"type":        a.Type,
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
	releases, _ := client.SearchReleases(r.Context(), req.Name)
	var result []map[string]interface{}
	// Always prepend the query text as an unmatched entry (no external id)
	result = append(result, map[string]interface{}{
		"title":       req.Name,
		"external_id": "",
		"artist":      "",
		"status":      "",
	})
	for _, rel := range releases {
		artistName := ""
		if len(rel.Artists) > 0 {
			artistName = rel.Artists[0].Name
		}
		result = append(result, map[string]interface{}{
			"title":       rel.Title,
			"external_id": rel.ID,
			"artist":      artistName,
			"status":      rel.Status,
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

	// Backfill country from MusicBrainz when the external ID is a MBID.
	if externalID != "" && a.Country == "" && sourceOrDefaultSource(source) == metadata.SourceMusicBrainz {
		if full, err := h.newMBClient(ctx).LookupArtist(ctx, externalID); err == nil && full.Country != "" {
			a.Country = full.Country
			h.artistRepo.Update(ctx, a)
		}
	}
	return a
}

func sourceOrDefaultSource(s string) string {
	return utils.SourceOrDefault(s)
}

// sourceMatches reports whether two metadata source values are the same,
// treating an empty value as the musicbrainz default (legacy rows carry no
// source).
func sourceMatches(a, b string) bool {
	return utils.SourceOrDefault(a) == utils.SourceOrDefault(b)
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

// validSources lists the metadata sources the Save endpoint accepts. Extend
// when a new source joins the recognition chain.
var validSources = map[string]bool{
	utils.SourceMusicBrainz: true,
	utils.SourceNetease:     true,
}

func isValidSource(s string) bool {
	return validSources[s]
}

// reResolveVersions handles version grouping after a track's external id
// changes. oldSource is the metadata source the track had before this save;
// when it differs from the new source the old group rows are cleaned without
// a source predicate so no stale (source, external id) rows survive.
func (h *MetadataHandler) reResolveVersions(ctx context.Context, libraryID, oldExtID, newExtID, trackID, oldSource string) {
	// The previous source may have been empty on legacy rows; normalize it
	// the same way the new source is normalized below.
	oldSource = sourceOrDefaultSource(oldSource)
	newSource, err := h.trackSource(ctx, trackID)
	if err != nil {
		log.Printf("[metadata] reResolveVersions: read source for %s: %v", trackID, err)
		return
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
			h.renumberGroup(ctx, oldSource, ids, oldExtID, libraryID)
		}
	}

	ids := h.externalIDGroupIDs(ctx, newSource, newExtID, libraryID)
	if len(ids) >= 2 {
		h.renumberGroup(ctx, newSource, ids, newExtID, libraryID)
	}
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

func (h *MetadataHandler) renumberGroup(ctx context.Context, source string, ids []string, externalID, libraryID string) {
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
			`INSERT INTO track_version_groups (metadata_source, external_id, library_id, track_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			source, externalID, libraryID, id)
	}
}
