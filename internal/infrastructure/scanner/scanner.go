package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Engine struct {
	db             *sql.DB
	trackRepo      *repository.TrackRepo
	albumRepo      *repository.AlbumRepo
	artistRepo     *repository.ArtistRepo
	coverExtractor *metadata.CoverExtractor
	covers         *metadata.CoverManager
	registry       *metadata.Registry
	// umRepo backs the user-metadata cache check that forces identification
	// when a saved user correction exists (nil disables the check).
	umRepo *repository.UserMetadataRepo
	// sourcePrio maps metadata source names to their Registry priority
	// (ascending, lower wins); unknown sources fall back to 100. Built once
	// from the registry so main-version selection follows source priority.
	sourcePrio  map[string]int
	entities    *metadata.EntityResolver
	lyricsStore *lyrics.Store
}

// NewEngine builds the scanner engine. registry carries the enabled
// metadata sources in priority order (nil or empty disables enrichment).
// covers is the shared cover manager (nil creates a private one; pass the
// server-wide instance to serialize extraction across scanner and HTTP
// cover requests). umRepo enables forcing identification when a saved user
// correction exists (may be nil).

func NewEngine(db *sql.DB, imagesDir string, registry *metadata.Registry, lyricsDir string, covers *metadata.CoverManager, umRepo *repository.UserMetadataRepo) *Engine {
	if covers == nil {
		covers = metadata.NewCoverManager(imagesDir, db, func() *metadata.Registry { return registry })
	}
	sourcePrio := make(map[string]int)
	if registry != nil {
		for _, s := range registry.Sources() {
			if s != nil {
				sourcePrio[s.Name()] = s.Priority()
			}
		}
	}
	return &Engine{
		db:             db,
		trackRepo:      repository.NewTrackRepo(db),
		albumRepo:      repository.NewAlbumRepo(db),
		artistRepo:     repository.NewArtistRepo(db),
		coverExtractor: metadata.NewCoverExtractor(imagesDir),
		covers:         covers,
		registry:       registry,
		umRepo:         umRepo,
		sourcePrio:     sourcePrio,
		entities:       metadata.NewEntityResolver(db),
		lyricsStore:    lyrics.NewStore(lyricsDir),
	}
}

type ScanOptions struct {
	Mode string
}

type ScanStats struct {
	TotalFiles      int
	Scanned         int
	NewTracks       int
	UpdatedTracks   int
	DeletedTracks   int
	CoversExtracted int
	Errors          []string
}

func (e *Engine) ScanLibrary(ctx context.Context, lib *domain.Library, opts ScanOptions, onProgress func(stats ScanStats)) (*ScanStats, error) {
	stats := &ScanStats{}

	// One artist-detail cache for the whole scan: the recognition chain calls
	// MusicBrainz per-artist for country/genre, and artists repeat across the
	// library's tracks. The cache lives only as long as this scan.
	ctx = metadata.WithArtistDetailCache(ctx, metadata.NewArtistDetailCache())

	_ = filepath.Walk(lib.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && metadata.IsAudioFile(path) {
			stats.TotalFiles++
		}
		return nil
	})
	onProgress(*stats)

	existingTracks, err := e.trackRepo.FindByLibraryID(ctx, lib.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing tracks: %w", err)
	}

	existingByPath := make(map[string]*domain.Track, len(existingTracks))
	for i := range existingTracks {
		existingByPath[existingTracks[i].FilePath] = &existingTracks[i]
	}

	seenPaths := make(map[string]bool)

	err = filepath.Walk(lib.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("walk error %s: %v", path, err))
			return nil
		}
		if info.IsDir() || !metadata.IsAudioFile(path) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		meta, err := metadata.Probe(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("probe error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		fileHash, err := hashFile(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("hash error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		seenPaths[path] = true

		var existing *domain.Track
		if e, ok := existingByPath[path]; ok {
			existing = e
		}

		var enrichment *metadata.EnrichmentResult
		var coverURL string
		identified := false
		// Identification also runs when the track is missing its cover and
		// the file carries no embedded art: the platform cover URL is the
		// only way to restore it. The recognition chain searches on the
		// original file tags, which may differ from the stored title after
		// a user edit.
		coverMissing := existing != nil && existing.CoverImageID == nil && !meta.HasCoverArt
		// A saved user correction must keep applying even when the stored
		// record already looks complete (the user source resolves by
		// (owner, hash), which only runs inside the identify block). The
		// lookup is short-circuited whenever the block already triggers.
		if e.registry != nil && (opts.Mode == "overwrite" || !e.metaComplete(ctx, existing) || coverMissing || e.hasUserCache(ctx, lib.OwnerID, fileHash)) {
			// The user metadata cache (userSource, priority 0) resolves by
			// (owner, file hash); the field-completion chain then fills
			// cover/lyrics from platform sources without touching the user's
			// fields. Lyrics/cover the file already carries (embedded or
			// sidecar) are excluded from the completion goals so no
			// platform lookup is wasted on them.
			sidecarLyrics, _ := e.findSidecarLyrics(path)
			q := metadata.QueryFromAudioMeta(meta)
			q.UserID = lib.OwnerID
			q.FileHash = fileHash
			// Fields the file already carries (embedded/sidecar lyrics,
			// embedded cover) count as present; the chain will not ask
			// platform sources for them.
			if meta.HasLyrics || sidecarLyrics != "" {
				q.FileFields |= port.FileFieldLyrics
			}
			if meta.HasCoverArt {
				q.FileFields |= port.FileFieldCover
			}
			candidate, err := e.registry.Identify(ctx, q)
			if err != nil {
				log.Printf("[scan] metadata identify error for %s: %v", path, err)
			} else if candidate != nil {
				identified = true
				coverURL = candidate.CoverArtURL
				enrichment = metadata.CandidateToEnrichment(candidate)
				if enrichment != nil {
					log.Printf("[scan] enriched: source=%s artist_ext=%s album_ext=%s track_ext=%s genre=%s year=%d lyrics=%t",
						enrichment.Source, enrichment.ArtistExternalID, enrichment.AlbumExternalID, enrichment.TrackExternalID, enrichment.Genre, enrichment.Year, enrichment.Lyrics != "")
				}
			}
		}

		if existing != nil && existing.Hash == fileHash {
			changed := false
			overwrite := opts.Mode == "overwrite"

			// Ensure track_albums entry exists (set before ApplyEnrichment so album enrichment can find it)
			if len(existing.Albums) == 0 {
				albumName := meta.Album
				if albumName == "" && enrichment != nil && enrichment.Album != "" {
					albumName = enrichment.Album
				}
				var primaryID string
				if tas, err := e.trackRepo.LoadTrackArtists(ctx, existing.ID); err == nil && len(tas) > 0 {
					primaryID = tas[0].ArtistID
				}
				if primaryID != "" && albumName != "" {
					album, err := e.findOrCreateAlbum(ctx, albumName, primaryID, meta.Year, meta.Genre, enrichment)
					if err == nil {
						existing.Albums = []*domain.TrackAlbum{{
							AlbumID:     album.ID,
							TrackNumber: meta.TrackNumber,
							DiscNumber:  meta.DiscNumber,
						}}
						changed = true
					}
				}
			}

			if ApplyEnrichment(ctx, existing, meta, enrichment, overwrite, e.trackRepo, e.artistRepo, e.albumRepo, e.entities) {
				changed = true
			}
			// (Source, external id) alignment with existing library records
			// runs once after the track row is persisted, below.

			// Backfill cover art when the DB pointer or (in overwrite mode)
			// the underlying file is missing. Only load the album row when a
			// track-side re-extraction is needed; an album-only gap reuses
			// the track's existing cover.
			// Cover: reuse the recognition chain's cover URL when the track
			// has none (its search ran on the original file tags, which may
			// differ from the stored title after a user edit); otherwise the
			// unified ensure flow (embedded extraction, then the platform
			// chain) restores missing covers in both modes.
			albumID := ""
			if len(existing.Albums) > 0 {
				albumID = existing.Albums[0].AlbumID
			}
			var album *domain.Album
			if albumID != "" {
				album, _ = e.albumRepo.FindByID(ctx, albumID)
			}
			if existing.CoverImageID == nil && coverURL != "" {
				if err := e.covers.ImportTrackCoverURL(ctx, lib.ID, existing, album, coverURL); err != nil {
					// A network import failure must not skip the unified
					// ensure flow (embedded extraction first, platform chain
					// as fallback).
					log.Printf("[scan] cover import error for %s: %v", path, err)
					if err := e.covers.EnsureTrackCover(ctx, lib.ID, existing, album, true, identified); err != nil {
						log.Printf("[scan] cover ensure error for %s: %v", path, err)
					} else {
						stats.CoversExtracted++
						changed = true
					}
				} else {
					stats.CoversExtracted++
					changed = true
				}
			} else if !e.covers.TrackCoverComplete(ctx, existing, overwrite) && (identified || meta.HasCoverArt) {
				// A track with neither an embedded cover nor a recognition hit can
				// never gain one here (no embedded bytes to extract, searchPlatform
				// is false): skip so scans do not spawn a failing ffmpeg
				// extraction and log an error on every pass.
				if err := e.covers.EnsureTrackCover(ctx, lib.ID, existing, album, true, identified); err != nil {
					log.Printf("[scan] cover ensure error for %s: %v", path, err)
				} else {
					stats.CoversExtracted++
					changed = true
				}
			} else if album != nil && !e.covers.AlbumCoverComplete(ctx, album, overwrite) {
				if err := e.covers.BackfillAlbumCover(ctx, album, true); err != nil {
					log.Printf("[scan] album cover backfill error for %s: %v", path, err)
				} else {
					changed = true
				}
			}

			if sContent, sFmt := e.findSidecarLyrics(path); sContent != "" {
				if err := e.lyricsStore.Save(ctx, lib.ID, existing.ID, lyrics.PrioritySidecar, sContent); err != nil {
					if ctx.Err() != nil {
						log.Printf("[scan] sidecar lyrics save cancelled for %s: %v (original: %v)", path, ctx.Err(), err)
					} else {
						log.Printf("[scan] sidecar lyrics save error for %s: %v", path, err)
					}
				} else if existing.LyricsMask&lyrics.PriorityBit(lyrics.PrioritySidecar) == 0 || sFmt == "lrc" {
					existing.LyricsMask |= lyrics.PriorityBit(lyrics.PrioritySidecar)
					changed = true
				}
			}

			if e.applyNetworkLyrics(ctx, lib.ID, existing, enrichment) {
				changed = true
			}

			if changed {
				e.trackRepo.Update(ctx, existing)
			}
			// The track is persisted; align to any existing (source,
			// external id) group so re-scans keep the primary source
			// stable (a fresh overwrite identification of a secondary
			// version must not re-crown the group).
			if e.tryMergeByIdentifiedID(ctx, lib.ID, existing) {
				if err := e.trackRepo.UpdateMergeFields(ctx, existing.ID, existing.MetadataSource, existing.ExternalID, existing.ExternalIDs); err != nil {
					log.Printf("[scan] merge write error for %s: %v", existing.ID, err)
				}
			}
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		title := meta.Title
		if enrichment != nil && enrichment.Title != "" {
			title = enrichment.Title
		}

		artistName := meta.Artist
		if meta.Artist == "" && enrichment != nil && enrichment.Artist != "" {
			artistName = enrichment.Artist
		}

		albumName := meta.Album
		if enrichment != nil && enrichment.Album != "" {
			albumName = enrichment.Album
		}

		year := meta.Year
		genre := meta.Genre

		// Record the producing source (MusicBrainz stays the implicit
		// default for legacy and non-enriched tracks).
		metadataSource := metadata.SourceMusicBrainz
		if enrichment != nil && enrichment.Source != "" {
			metadataSource = enrichment.Source
		}

		// Collect performer names from ffprobe multi-artist or fallback
		performerNames := meta.Artists
		// If no performers from ffprobe, use enrichment artists
		if len(performerNames) == 0 && enrichment != nil && len(enrichment.Artists) > 0 {
			for _, ar := range enrichment.Artists {
				if ar.Name != "" {
					performerNames = append(performerNames, ar.Name)
				}
			}
		}
		// Fallback to single artist name
		if len(performerNames) == 0 && artistName != "" {
			performerNames = []string{artistName}
		}
		// Last resort
		if len(performerNames) == 0 {
			performerNames = []string{"Unknown Artist"}
		}

		var trackArtists []*domain.TrackArtist
		var primaryPerformerID string
		sortOrder := 0
		for _, pn := range performerNames {
			enrich := matchArtistEnrichment(pn, enrichment)
			a, err := e.findOrCreateArtist(ctx, pn, enrich)
			if err != nil {
				log.Printf("[scan] failed to create artist %q: %v", pn, err)
				continue
			}
			if primaryPerformerID == "" {
				primaryPerformerID = a.ID
			}
			trackArtists = append(trackArtists, &domain.TrackArtist{
				ArtistID:  a.ID,
				Role:      "performer",
				SortOrder: sortOrder,
				Artist:    a,
			})
			sortOrder++
		}

		// Helper for optional roles: silently skip on error
		addArtist := func(name string, role string) {
			if name == "" || name == "Unknown Artist" {
				return
			}
			enrich := matchArtistEnrichment(name, enrichment)
			a, err := e.findOrCreateArtist(ctx, name, enrich)
			if err != nil {
				return
			}
			trackArtists = append(trackArtists, &domain.TrackArtist{
				ArtistID:  a.ID,
				Role:      role,
				SortOrder: sortOrder,
				Artist:    a,
			})
			sortOrder++
		}

		// Album artist
		if meta.AlbumArtist != "" && meta.AlbumArtist != meta.Artist {
			addArtist(meta.AlbumArtist, "album_artist")
		}
		// Composer / Lyricist / Arranger
		addArtist(meta.Composer, "composer")
		addArtist(meta.Lyricist, "lyricist")
		addArtist(meta.Arranger, "arranger")

		if primaryPerformerID == "" {
			stats.Errors = append(stats.Errors, fmt.Sprintf("no valid performer for %s", path))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		album, err := e.findOrCreateAlbum(ctx, albumName, primaryPerformerID, year, genre, enrichment)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("album error: %v", err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		now := time.Now()
		trackID := domain.NewID()
		if existing != nil {
			trackID = existing.ID
		}

		track := &domain.Track{
			ID:             trackID,
			LibraryID:      lib.ID,
			MetadataSource: metadataSource,
			Title:          title,
			Artists:        trackArtists,
			Duration:       meta.Duration,
			BitRate:        meta.BitRate,
			SampleRate:     meta.SampleRate,
			Channels:       meta.Channels,
			FilePath:       path,
			FileSize:       meta.FileSize,
			FileFormat:     meta.FileFormat,
			AudioCodec:     meta.AudioCodec,
			Hash:           fileHash,
			CreatedAt:      now,
			UpdatedAt:      now,
			Albums: []*domain.TrackAlbum{{
				AlbumID:     album.ID,
				TrackNumber: meta.TrackNumber,
				DiscNumber:  meta.DiscNumber,
			}},
		}

		if existing != nil {
			// Re-deriving this row from a changed file must not drop the
			// cross-source aliases accumulated by earlier passes (version-group
			// unions, ApplyEnrichment source-switch records, manual
			// associations): the update below replaces external_ids wholesale,
			// so carry the existing map into the fresh track and let the
			// derivation below merge into it.
			if len(existing.ExternalIDs) > 0 {
				track.ExternalIDs = maps.Clone(existing.ExternalIDs)
			}
			// A source switch (the changed file now identifies under a
			// different namespace) must keep the old primary id reachable as
			// an alias under its old source, mirroring ApplyEnrichment's track
			// branch — otherwise the previous (source, external_id) identity
			// is lost and the track leaves its old version group for good.
			if existing.ExternalID != "" {
				oldSrc := existing.MetadataSource
				if oldSrc == "" {
					oldSrc = metadata.SourceMusicBrainz
				}
				if oldSrc != metadataSource {
					if track.ExternalIDs == nil {
						track.ExternalIDs = map[string]string{}
					}
					track.ExternalIDs[oldSrc] = existing.ExternalID
				}
			}
		}

		if enrichment != nil {
			track.SetExternalID(enrichment.TrackExternalID)
		}
		if meta.MBID != "" {
			if metadataSource == metadata.SourceMusicBrainz {
				track.SetExternalID(meta.MBID)
			} else if track.ExternalID != "" {
				// The file's MBID is an alias of the non-MB primary (e.g. a
				// NetEase-sourced track), not a replacement: recording it
				// under musicbrainz keeps the (source, external_id) pair
				// consistent and makes the MBID reachable via aliases.
				if track.ExternalIDs == nil {
					track.ExternalIDs = map[string]string{}
				}
				track.ExternalIDs[metadata.SourceMusicBrainz] = meta.MBID
			}
			// No primary id and source is not MB: leave the MBID out of the
			// primary slot rather than write a (non-MB, mbid) mismatch.
		}

		if existing != nil {
			track.CreatedAt = existing.CreatedAt
			track.Heat = existing.Heat
			track.PlayCount = existing.PlayCount
			track.LastPlayedAt = existing.LastPlayedAt
			track.CoverImageID = existing.CoverImageID
			track.Version = existing.Version
			track.VersionLabel = existing.VersionLabel
		}

		// Cover: reuse the recognition chain's cover URL, else the unified
		// ensure flow (embedded extraction, then the platform chain through
		// the track's metadata source).
		if track.CoverImageID == nil && coverURL != "" {
			if err := e.covers.ImportTrackCoverURL(ctx, lib.ID, track, album, coverURL); err != nil {
				// Fall back to the unified ensure flow (embedded extraction,
				// then the platform chain) so a network failure does not leave
				// the track coverless for this scan.
				log.Printf("[scan] cover import error for %s: %v", path, err)
				if err := e.covers.EnsureTrackCover(ctx, lib.ID, track, album, true, identified); err != nil {
					log.Printf("[scan] cover ensure error for %s: %v", path, err)
				} else {
					stats.CoversExtracted++
				}
			} else {
				stats.CoversExtracted++
			}
		} else if identified || meta.HasCoverArt {
			// A track with neither an embedded cover nor a recognition hit can
			// never gain one here — skip so scans do not spawn a failing
			// ffmpeg extraction and log an error on every pass.
			if err := e.covers.EnsureTrackCover(ctx, lib.ID, track, album, true, identified); err != nil {
				log.Printf("[scan] cover ensure error for %s: %v", path, err)
			} else {
				stats.CoversExtracted++
				if album != nil && !e.covers.AlbumCoverComplete(ctx, album, false) {
					if err := e.covers.BackfillAlbumCover(ctx, album, true); err != nil {
						log.Printf("[scan] album cover backfill error for %s: %v", path, err)
					}
				}
			}
		}

		// Extract lyrics: embedded (priority 0) then sidecar (priority 1)
		mask := 0
		if meta.Lyrics != "" {
			if err := e.lyricsStore.Save(ctx, lib.ID, trackID, lyrics.PriorityEmbedded, meta.Lyrics); err != nil {
				if ctx.Err() != nil {
					log.Printf("[scan] embedded lyrics save cancelled for %s: %v (original: %v)", path, ctx.Err(), err)
				} else {
					log.Printf("[scan] embedded lyrics save error for %s: %v", path, err)
				}
			} else {
				mask |= lyrics.PriorityBit(lyrics.PriorityEmbedded)
			}
		}
		if sContent, _ := e.findSidecarLyrics(path); sContent != "" {
			if err := e.lyricsStore.Save(ctx, lib.ID, trackID, lyrics.PrioritySidecar, sContent); err != nil {
				if ctx.Err() != nil {
					log.Printf("[scan] sidecar lyrics save cancelled for %s: %v (original: %v)", path, ctx.Err(), err)
				} else {
					log.Printf("[scan] sidecar lyrics save error for %s: %v", path, err)
				}
			} else {
				mask |= lyrics.PriorityBit(lyrics.PrioritySidecar)
			}
		}
		track.LyricsMask = mask
		if e.applyNetworkLyrics(ctx, lib.ID, track, enrichment) {
			// mask already folded into track.LyricsMask by the helper
		}

		if existing != nil {
			if err := e.trackRepo.Update(ctx, track); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("update error %s: %v", path, err))
			} else {
				stats.UpdatedTracks++
			}
		} else {
			if err := e.trackRepo.BatchCreate(ctx, []domain.Track{*track}); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("create error %s: %v", path, err))
			} else {
				stats.NewTracks++
			}
		}

		// The track is persisted; the recognized (source, external id) may
		// already exist in the library (another version of the same song).
		// Align to that version group and share external ids.
		if e.tryMergeByIdentifiedID(ctx, lib.ID, track) {
			if err := e.trackRepo.UpdateMergeFields(ctx, track.ID, track.MetadataSource, track.ExternalID, track.ExternalIDs); err != nil {
				log.Printf("[scan] merge write error for %s: %v", track.ID, err)
			}
		}

		stats.Scanned++
		onProgress(*stats)
		return nil
	})
	if err != nil {
		return stats, err
	}

	var deletedIDs []string
	for path := range existingByPath {
		if !seenPaths[path] {
			trackID, err := e.trackRepo.FindIDByFilePath(ctx, path, lib.ID)
			if err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete lookup error %s: %v", path, err))
				continue
			}
			if trackID == "" {
				continue
			}
			// Clean the cover first; only delete the track row when that
			// succeeded, so a transient failure cannot orphan the images
			// rows and files (owner_id has no FK to tracks).
			if err := e.covers.DeleteTrackCovers(ctx, lib.ID, trackID); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("cover cleanup error %s: %v (track row kept)", trackID, err))
				continue
			}
			// Lyrics are plain files (no DB rows), so cleanup is best-effort
			// and never blocks the track deletion.
			if err := e.lyricsStore.DeleteAll(lib.ID, trackID); err != nil {
				log.Printf("[scan] lyrics cleanup error for %s: %v", trackID, err)
			}
			if err := e.trackRepo.DeleteByID(ctx, trackID); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete error %s: %v", trackID, err))
				continue
			}
			deletedIDs = append(deletedIDs, trackID)
			stats.DeletedTracks++
		}
	}

	if len(deletedIDs) > 0 {
		e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'track' AND item_id = ANY($1)`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx, `DELETE FROM play_history WHERE track_id = ANY($1)`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx,
			`UPDATE playlists SET track_ids = (
				SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
				FROM jsonb_array_elements_text(track_ids) AS elem
				WHERE NOT (elem = ANY($1::text[]))
			) WHERE track_ids ?| $1`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx,
			`UPDATE jukeboxes SET
				queue = COALESCE(
					(SELECT jsonb_agg(elem) FROM jsonb_array_elements_text(queue) AS elem
					 WHERE NOT (elem = ANY($1::text[]))),
					'[]'::jsonb
				),
				queue_idx = 0,
				shuffle_order = '[]'::jsonb,
				shuffle_idx = 0`, pq.Array(deletedIDs))
	}

	// Remove covers of albums that no longer have any track before pruning them.
	if rows, err := e.db.QueryContext(ctx,
		`SELECT id FROM albums WHERE id NOT IN (SELECT DISTINCT album_id FROM track_albums)`); err == nil {
		var orphanIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				log.Printf("[scan] orphan album scan error: %v", err)
				continue
			}
			orphanIDs = append(orphanIDs, id)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[scan] orphan album iteration error: %v", err)
		}
		rows.Close()
		for _, id := range orphanIDs {
			// Only prune the album row when its cover cleanup succeeded;
			// otherwise the images row and files would be orphaned forever.
			if err := e.covers.DeleteAlbumCovers(ctx, id); err != nil {
				log.Printf("[scan] delete album covers for %s: %v (album row kept)", id, err)
				continue
			}
			if _, err := e.db.ExecContext(ctx, `DELETE FROM albums WHERE id = $1`, id); err != nil {
				log.Printf("[scan] delete album row %s: %v (cover already removed)", id, err)
			}
		}
	} else {
		log.Printf("[scan] orphan album query error: %v", err)
	}
	e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'album' AND item_id NOT IN (SELECT DISTINCT album_id FROM track_albums)`)

	// Prune orphaned artists: cover rows first so a failure keeps the
	// artist row for a later retry instead of orphaning its images.
	if rows, err := e.db.QueryContext(ctx,
		`SELECT id FROM artists WHERE id NOT IN (SELECT DISTINCT artist_id FROM track_artists)`); err == nil {
		var orphanArtistIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				log.Printf("[scan] orphan artist scan error: %v", err)
				continue
			}
			orphanArtistIDs = append(orphanArtistIDs, id)
		}
		rows.Close()
		for _, id := range orphanArtistIDs {
			if err := e.covers.DeleteArtistCovers(ctx, id); err != nil {
				log.Printf("[scan] delete artist covers for %s: %v (artist row kept)", id, err)
				continue
			}
			if _, err := e.db.ExecContext(ctx, `DELETE FROM artists WHERE id = $1`, id); err != nil {
				log.Printf("[scan] delete artist row %s: %v", id, err)
			}
		}
	} else {
		log.Printf("[scan] orphan artist query error: %v", err)
	}

	// Full scans also sweep covers and lyrics orphaned by interrupted
	// deletions or manual database edits. Incremental (missing) scans skip
	// this maintenance work.
	if opts.Mode == "overwrite" {
		if err := e.covers.SweepOrphanCovers(ctx); err != nil {
			log.Printf("[scan] orphan cover sweep error: %v", err)
		}
		e.sweepOrphanLyrics(ctx, lib.ID)
	}

	if err := e.mergeDuplicates(ctx, lib.ID); err != nil {
		log.Printf("[scan] duplicate merge error for %s: %v", lib.Name, err)
	}

	if err := e.resolveVersions(ctx, lib.ID); err != nil {
		log.Printf("[scan] version resolution error for %s: %v", lib.Name, err)
	}

	// Recalculate album song_count and duration for all albums
	if err := e.recalcAlbumStats(ctx, lib.ID); err != nil {
		log.Printf("[scan] album stats recalculation error: %v", err)
	}

	lib.TrackCount = len(existingByPath) + stats.NewTracks - stats.DeletedTracks
	lib.LastScannedAt = timePtr(time.Now())

	log.Printf("[scan] library=%s total=%d new=%d updated=%d deleted=%d covers=%d errors=%d",
		lib.Name, stats.TotalFiles, stats.NewTracks, stats.UpdatedTracks, stats.DeletedTracks, stats.CoversExtracted, len(stats.Errors))

	return stats, nil
}

// recalcAlbumStats refreshes song_count and duration for all albums that have
// tracks in the given library.
func (e *Engine) recalcAlbumStats(ctx context.Context, libraryID string) error {
	ids, err := e.trackRepo.AlbumIDsByLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	n := len(ids)
	if err := e.trackRepo.UpdateAlbumStats(ctx, ids); err != nil {
		return err
	}
	log.Printf("[scan] recalc album stats: %d albums updated", n)
	return nil
}

func (e *Engine) ExtractCovers(ctx context.Context, lib *domain.Library, onProgress func(scanned, total int)) error {
	tracks, err := e.trackRepo.FindByLibraryID(ctx, lib.ID)
	if err != nil {
		return fmt.Errorf("failed to load tracks: %w", err)
	}

	total := len(tracks)
	for i, t := range tracks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if e.covers.TrackCoverComplete(ctx, &t, true) {
			continue
		}

		if err := e.covers.ExtractTrackCover(ctx, lib.ID, &t, nil, true); err != nil {
			continue
		}

		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	return nil
}

func (e *Engine) findSidecarLyrics(audioPath string) (content string, format string) {
	dir := filepath.Dir(audioPath)
	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))

	for _, ext := range []string{".lrc", ".txt"} {
		path := filepath.Join(dir, base+ext)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			f := "txt"
			if ext == ".lrc" {
				f = "lrc"
			}
			return string(data), f
		}
	}
	return "", ""
}

func (e *Engine) ensureThumbnail(libraryID, trackID string) error {
	mainPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s.jpg", trackID))
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("main cover not found for track %s", trackID)
	}
	for _, size := range []int{64, 256} {
		thumbPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s_%d.jpg", trackID, size))
		if _, err := os.Stat(thumbPath); err == nil {
			continue
		}
		if err := metadata.ResizeToThumbnail(data, thumbPath, size); err != nil {
			log.Printf("[scan] thumbnail resize error %s: %v", thumbPath, err)
		}
	}
	return nil
}

func (e *Engine) metaComplete(ctx context.Context, track *domain.Track) bool {
	if track == nil {
		return false
	}

	// Common basic check: title + at least one non-Unknown artist + album
	if ok, err := e.trackRepo.TrackHasBasicMeta(ctx, track.ID); err != nil {
		log.Printf("[scan] metaComplete TrackHasBasicMeta error for %s: %v", track.ID, err)
		return false
	} else if !ok {
		return false
	}

	// Non-MusicBrainz sources (e.g. NetEase) do not expose country/genre and
	// may not carry the source ID in external_id; require only the fields they
	// actually provide so the track is not re-identified on every scan.
	if track.MetadataSource != "" && track.MetadataSource != metadata.SourceMusicBrainz {
		return true
	}

	// MusicBrainz completeness requires the full MB profile.
	if track.ExternalID == "" {
		return false
	}
	trackArtists, err := e.trackRepo.LoadTrackArtists(ctx, track.ID)
	if err != nil || len(trackArtists) == 0 {
		return false
	}
	for _, ta := range trackArtists {
		artist, err := e.artistRepo.FindByID(ctx, ta.ArtistID)
		if err != nil || artist.Name == "" || artist.Name == "Unknown Artist" ||
			artist.ExternalID == "" || artist.Country == "" {
			return false
		}
	}
	for _, tal := range track.Albums {
		album, err := e.albumRepo.FindByID(ctx, tal.AlbumID)
		if err != nil || album.Title == "" || album.Title == "Unknown Album" ||
			album.ExternalID == "" || album.Country == "" ||
			album.Year == 0 || album.Genre == "" {
			return false
		}
	}
	return true
}

// hasUserCache reports whether a saved user metadata row exists for the
// (owner, file hash) pair. Only queried when the identify block would
// otherwise be skipped, so incremental scans of complete tracks still apply
// the user's corrections.
func (e *Engine) hasUserCache(ctx context.Context, userID, fileHash string) bool {
	if e.umRepo == nil || fileHash == "" {
		return false
	}
	_, err := e.umRepo.FindByUserAndHash(ctx, userID, fileHash)
	if err == nil {
		return true
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	// A DB failure must not silently skip the identify block (which would
	// also skip platform enrichment and cover recovery): log it and err
	// toward re-running identification.
	log.Printf("[scan] user metadata cache lookup error: %v", err)
	return true
}
