package scanner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/lib/pq"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Engine struct {
	db             *sql.DB
	trackRepo      *repository.TrackRepo
	albumRepo      *repository.AlbumRepo
	artistRepo     *repository.ArtistRepo
	umRepo         *repository.UserMetadataRepo
	coverExtractor *metadata.CoverExtractor
	resolver       *metadata.Resolver
	entities       *metadata.EntityResolver
	lyricsStore    *lyrics.Store
}

func NewEngine(db *sql.DB, imagesDir string, mbCfg metadata.MBConfig, lyricsDir string) *Engine {
	var resolver *metadata.Resolver
	if mbCfg.Enabled {
		resolver = metadata.NewResolver(mbCfg)
		log.Printf("[scanner] MusicBrainz resolver enabled: %s (rate: %d/s)", mbCfg.APIURL, mbCfg.RateLimit)
	} else {
		log.Printf("[scanner] MusicBrainz resolver disabled")
	}
	return &Engine{
		db:             db,
		trackRepo:      repository.NewTrackRepo(db),
		albumRepo:      repository.NewAlbumRepo(db),
		artistRepo:     repository.NewArtistRepo(db),
		umRepo:         repository.NewUserMetadataRepo(db),
		coverExtractor: metadata.NewCoverExtractor(imagesDir),
		resolver:       resolver,
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

		// Apply user-saved metadata overrides (priority 1)
		if um, err := e.umRepo.FindByUserAndHash(ctx, lib.OwnerID, fileHash); err == nil {
			if um.Title != "" {
				meta.Title = um.Title
				meta.TitleFromFilename = false
			}
			if um.Artist != "" {
				meta.Artist = um.Artist
			}
			if um.Album != "" {
				meta.Album = um.Album
			}
			if um.Year != 0 {
				meta.Year = um.Year
			}
			if um.Genre != "" {
				meta.Genre = um.Genre
			}
		}

		seenPaths[path] = true

		var existing *domain.Track
		if e, ok := existingByPath[path]; ok {
			existing = e
		}

		var enrichment *metadata.EnrichmentResult
		if e.resolver != nil && (opts.Mode == "overwrite" || !e.metaComplete(ctx, existing)) {
			if result, err := e.resolver.Enrich(ctx, meta); err != nil {
				log.Printf("[scan] mb enrich error for %s: %v", path, err)
			} else {
				enrichment = result
				if enrichment != nil {
					log.Printf("[scan] mb enriched: artist_mbid=%s album_mbid=%s track_mbid=%s genre=%s year=%d",
						enrichment.ArtistMBID, enrichment.AlbumMBID, enrichment.TrackMBID, enrichment.Genre, enrichment.Year)
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
					album, err := e.findOrCreateAlbum(ctx, lib.ID, albumName, primaryID, meta.Year, meta.Genre, enrichment)
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

			if ApplyEnrichment(ctx, existing, meta, enrichment, lib.ID, overwrite, e.trackRepo, e.artistRepo, e.albumRepo, e.entities) {
				changed = true
			}

			// Backfill cover art when it is missing (e.g. after a database
			// rebuild, existing tracks are re-scanned without re-creating).
			if meta.HasCoverArt {
				albumID := ""
				if len(existing.Albums) > 0 {
					albumID = existing.Albums[0].AlbumID
				}
				if !mainCoverExists(e.coverExtractor, lib.ID, existing.ID) ||
					(albumID != "" && !mainAlbumCoverExists(e.coverExtractor, albumID)) {
					var album *domain.Album
					if albumID != "" {
						album, _ = e.albumRepo.FindByID(ctx, albumID)
					}
					if e.extractCover(ctx, lib.ID, existing, album, path, stats) {
						changed = true
					}
				}
			}

			if sContent, sFmt := e.findSidecarLyrics(path); sContent != "" {
				e.lyricsStore.Save(lib.ID, existing.ID, lyrics.PrioritySidecar, sContent)
				if existing.LyricsMask&lyrics.PriorityBit(lyrics.PrioritySidecar) == 0 || sFmt == "lrc" {
					existing.LyricsMask |= lyrics.PriorityBit(lyrics.PrioritySidecar)
					changed = true
				}
			}

			if changed {
				e.trackRepo.Update(ctx, existing)
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
			a, err := e.findOrCreateArtist(ctx, lib.ID, pn, enrich)
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
			a, err := e.findOrCreateArtist(ctx, lib.ID, name, enrich)
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

		album, err := e.findOrCreateAlbum(ctx, lib.ID, albumName, primaryPerformerID, year, genre, enrichment)
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
			ID:         trackID,
			LibraryID:  lib.ID,
			Title:      title,
			Artists:    trackArtists,
			Duration:   meta.Duration,
			BitRate:    meta.BitRate,
			SampleRate: meta.SampleRate,
			Channels:   meta.Channels,
			FilePath:    path,
			FileSize:    meta.FileSize,
			FileFormat:  meta.FileFormat,
			AudioCodec:  meta.AudioCodec,
			Hash:        fileHash,
			CreatedAt:  now,
			UpdatedAt:  now,
			Albums: []*domain.TrackAlbum{{
				AlbumID:     album.ID,
				TrackNumber: meta.TrackNumber,
				DiscNumber:  meta.DiscNumber,
			}},
		}

		if enrichment != nil {
			track.MBID = enrichment.TrackMBID
		}
		if meta.MBID != "" {
			track.MBID = meta.MBID
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

		if meta.HasCoverArt {
			if e.extractCover(ctx, lib.ID, track, album, path, stats) {
				// nothing to flag: the track record is written below anyway
			}
		}

		// Extract lyrics: embedded (priority 0) then sidecar (priority 1)
		mask := 0
		if meta.Lyrics != "" {
			e.lyricsStore.Save(lib.ID, trackID, lyrics.PriorityEmbedded, meta.Lyrics)
			mask |= lyrics.PriorityBit(lyrics.PriorityEmbedded)
		}
		if sContent, _ := e.findSidecarLyrics(path); sContent != "" {
			e.lyricsStore.Save(lib.ID, trackID, lyrics.PrioritySidecar, sContent)
			mask |= lyrics.PriorityBit(lyrics.PrioritySidecar)
		}
		track.LyricsMask = mask

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
			trackID, err := e.trackRepo.DeleteByFilePath(ctx, path, lib.ID)
			if err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete error %s: %v", path, err))
			} else if trackID != "" {
				deletedIDs = append(deletedIDs, trackID)
				stats.DeletedTracks++
			}
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
			metadata.RemoveAlbumCover(e.coverExtractor.ImagesDir(), id)
		}
	} else {
		log.Printf("[scan] orphan album query error: %v", err)
	}
	e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'album' AND item_id NOT IN (SELECT DISTINCT album_id FROM track_albums)`)
	e.db.ExecContext(ctx, `DELETE FROM albums WHERE id NOT IN (SELECT DISTINCT album_id FROM track_albums)`)
	e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'artist' AND item_id NOT IN (SELECT DISTINCT artist_id FROM track_artists)`)
	e.db.ExecContext(ctx, `DELETE FROM artists WHERE id NOT IN (SELECT DISTINCT artist_id FROM track_artists)`)

	if err := e.resolveVersions(ctx, lib.ID); err != nil {
		log.Printf("[scan] version resolution error for %s: %v", lib.Name, err)
	}

	lib.TrackCount = len(existingByPath) + stats.NewTracks - stats.DeletedTracks
	lib.LastScannedAt = timePtr(time.Now())

	log.Printf("[scan] library=%s total=%d new=%d updated=%d deleted=%d covers=%d errors=%d",
		lib.Name, stats.TotalFiles, stats.NewTracks, stats.UpdatedTracks, stats.DeletedTracks, stats.CoversExtracted, len(stats.Errors))

	return stats, nil
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

		if mainCoverExists(e.coverExtractor, lib.ID, t.ID) {
			continue
		}

		data, _, err := e.coverExtractor.ExtractFromFile(t.FilePath)
		if err != nil {
			continue
		}
		thumbDir := filepath.Join(e.coverExtractor.ImagesDir(), lib.ID)
		if err := os.MkdirAll(thumbDir, 0755); err != nil {
			continue
		}
		mainPath := metadata.CoverPath(e.coverExtractor.ImagesDir(), lib.ID, "track", t.ID, "jpg")
		if err := os.WriteFile(mainPath, data, 0644); err != nil {
			continue
		}
		thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", t.ID))
		metadata.ResizeToThumbnail(data, thumbPath, 64)

		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	return nil
}

// extractCover extracts embedded cover art for a track and its primary album
// when either is missing on disk. The track gate is the main cover file
// (always written at original size); the 64px thumbnail is only produced for
// sources larger than 64px, so it cannot serve as the existence check. Track
// and album state are judged by file existence, so deleted image files are
// restored on the next scan even when the DB pointers still reference them.
// It returns true when the track's cover state changed (CoverImageID set).
func (e *Engine) extractCover(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, audioPath string, stats *ScanStats) bool {
	trackMissing := !mainCoverExists(e.coverExtractor, libraryID, track.ID)
	albumMissing := album != nil && !mainAlbumCoverExists(e.coverExtractor, album.ID)
	if !trackMissing && !albumMissing {
		return false
	}

	data, _, err := e.coverExtractor.ExtractFromFile(audioPath)
	if err != nil {
		return false
	}

	changed := false
	if trackMissing {
		thumbDir := filepath.Join(e.coverExtractor.ImagesDir(), libraryID)
		if err := os.MkdirAll(thumbDir, 0755); err != nil {
			log.Printf("[scan] cover mkdir error for %s: %v", audioPath, err)
			return false
		}
		mainPath := metadata.CoverPath(e.coverExtractor.ImagesDir(), libraryID, "track", track.ID, "jpg")
		if err := os.WriteFile(mainPath, data, 0644); err != nil {
			log.Printf("[scan] cover write error for %s: %v", audioPath, err)
			return false
		}
		thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", track.ID))
		metadata.ResizeToThumbnail(data, thumbPath, 64)
		track.CoverImageID = &track.ID
		stats.CoversExtracted++
		changed = true
	}
	if albumMissing {
		if _, err := e.coverExtractor.Save("album", "album", album.ID, data, "jpg", 256); err != nil {
			log.Printf("[scan] album cover save error for %s: %v", audioPath, err)
			return changed
		}
		album.CoverImageID = &album.ID
		e.albumRepo.Update(ctx, album)
	}
	return changed
}

func mainCoverExists(ce *metadata.CoverExtractor, libraryID, trackID string) bool {
	p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s.jpg", trackID))
	_, err := os.Stat(p)
	return err == nil
}

func mainAlbumCoverExists(ce *metadata.CoverExtractor, albumID string) bool {
	p := filepath.Join(ce.ImagesDir(), "album", fmt.Sprintf("album_%s.jpg", albumID))
	_, err := os.Stat(p)
	return err == nil
}

func thumbnailExists(ce *metadata.CoverExtractor, libraryID, trackID string, size int) bool {
	p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s_%d.jpg", trackID, size))
	_, err := os.Stat(p)
	return err == nil
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
		metadata.ResizeToThumbnail(data, thumbPath, size)
	}
	return nil
}

func (e *Engine) metaComplete(ctx context.Context, track *domain.Track) bool {
	if track == nil {
		return false
	}

	// Non-MusicBrainz sources (e.g. NetEase) do not expose country/genre and
	// may not carry the source ID in mbid; require only the fields they
	// actually provide so the track is not re-identified on every scan.
	// Checked before the MBID guard below.
	if track.MetadataSource != "" && track.MetadataSource != metadata.SourceMusicBrainz {
		if track.Title == "" {
			return false
		}
		trackArtists, err := e.trackRepo.LoadTrackArtists(ctx, track.ID)
		if err != nil || len(trackArtists) == 0 {
			return false
		}
		for _, ta := range trackArtists {
			artist, err := e.artistRepo.FindByID(ctx, ta.ArtistID)
			if err != nil || artist.Name == "" || artist.Name == "Unknown Artist" {
				return false
			}
		}
		for _, tal := range track.Albums {
			album, err := e.albumRepo.FindByID(ctx, tal.AlbumID)
			if err != nil || album.Title == "" || album.Title == "Unknown Album" {
				return false
			}
		}
		return true
	}

	if track.MBID == "" {
		return false
	}

	// MusicBrainz completeness requires the full MB profile.
	// Check artists via track_artists
	trackArtists, err := e.trackRepo.LoadTrackArtists(ctx, track.ID)
	if err != nil || len(trackArtists) == 0 {
		return false
	}
	for _, ta := range trackArtists {
		artist, err := e.artistRepo.FindByID(ctx, ta.ArtistID)
		if err != nil || artist.Name == "" || artist.Name == "Unknown Artist" ||
			artist.MBID == "" || artist.Country == "" {
			return false
		}
	}
	for _, tal := range track.Albums {
		album, err := e.albumRepo.FindByID(ctx, tal.AlbumID)
		if err != nil || album.Title == "" || album.Title == "Unknown Album" ||
			album.MBID == "" || album.Country == "" ||
			album.Year == 0 || album.Genre == "" {
			return false
		}
	}
	return true
}

func matchArtistEnrichment(name string, enrichment *metadata.EnrichmentResult) *metadata.EnrichmentResult {
	if enrichment == nil || len(enrichment.Artists) == 0 {
		return nil
	}
	n := strings.ToLower(strings.TrimSpace(name))
	for _, ar := range enrichment.Artists {
		if strings.ToLower(strings.TrimSpace(ar.Name)) == n {
			return &metadata.EnrichmentResult{
				ArtistMBID:    ar.MBID,
				ArtistCountry: ar.Country,
				Artist:        ar.Name,
			}
		}
	}
	return nil
}

func (e *Engine) findOrCreateArtist(ctx context.Context, libraryID, name string, enrichment *metadata.EnrichmentResult) (*domain.Artist, error) {
	return findOrCreateArtist(ctx, e.entities, e.artistRepo, libraryID, name, enrichment)
}

// findOrCreateArtist resolves an artist through the shared cross-source
// lookup chain (primary ID → alias → normalized name) and backfills
// enrichment fields (MBID, country) on an existing record.
func findOrCreateArtist(ctx context.Context, er *metadata.EntityResolver, artistRepo *repository.ArtistRepo, libraryID, name string, enrichment *metadata.EnrichmentResult) (*domain.Artist, error) {
	source := metadata.SourceMusicBrainz
	externalID := ""
	if enrichment != nil {
		externalID = enrichment.ArtistMBID
	}

	artist, err := er.FindOrCreateArtist(ctx, source, externalID, name)
	if err != nil {
		return nil, err
	}

	if enrichment != nil {
		updated := false
		if artist.Name == "" && enrichment.Artist != "" {
			artist.Name = enrichment.Artist
			artist.SortName = enrichment.Artist
			updated = true
		}
		if artist.Country == "" && enrichment.ArtistCountry != "" {
			artist.Country = enrichment.ArtistCountry
			updated = true
		}
		if updated {
			artistRepo.Update(ctx, artist)
		}
	}
	return artist, nil
}

func (e *Engine) findOrCreateAlbum(ctx context.Context, libraryID, title, artistID string, year int, genre string, enrichment *metadata.EnrichmentResult) (*domain.Album, error) {
	return findOrCreateAlbum(ctx, e.entities, e.albumRepo, libraryID, title, artistID, year, genre, enrichment)
}

// findOrCreateAlbum resolves an album through the shared cross-source lookup
// chain (primary ID → alias → normalized title within artist) and backfills
// enrichment fields (MBID, year, genre, country).
func findOrCreateAlbum(ctx context.Context, er *metadata.EntityResolver, albumRepo *repository.AlbumRepo, libraryID, title, artistID string, year int, genre string, enrichment *metadata.EnrichmentResult) (*domain.Album, error) {
	source := metadata.SourceMusicBrainz
	externalID := ""
	if enrichment != nil {
		externalID = enrichment.AlbumMBID
	}

	album, err := er.FindOrCreateAlbum(ctx, source, externalID, title, artistID, year, genre, albumCountry(enrichment))
	if err != nil {
		return nil, err
	}

	if enrichment != nil {
		updated := false
		if album.Title == "Unknown Album" && enrichment.Album != "" {
			album.Title = enrichment.Album
			updated = true
		}
		if album.Year == 0 && enrichment.Year != 0 {
			album.Year = enrichment.Year
			updated = true
		}
		if album.Genre == "" && enrichment.Genre != "" {
			album.Genre = enrichment.Genre
			updated = true
		}
		if album.Country == "" && enrichment.AlbumCountry != "" {
			album.Country = enrichment.AlbumCountry
			updated = true
		}
		if updated {
			albumRepo.Update(ctx, album)
		}
	}
	return album, nil
}

func albumCountry(enrichment *metadata.EnrichmentResult) string {
	if enrichment != nil {
		return enrichment.AlbumCountry
	}
	return ""
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// ApplyEnrichment applies enrichment results to an existing track. When overwrite is true,
// all fields are replaced; when false, only empty/unknown fields are filled.
func ApplyEnrichment(ctx context.Context, track *domain.Track, meta *metadata.AudioMeta, enrichment *metadata.EnrichmentResult, libraryID string, overwrite bool, trackRepo *repository.TrackRepo, artistRepo *repository.ArtistRepo, albumRepo *repository.AlbumRepo, er *metadata.EntityResolver) (changed bool) {
	if meta.MBID != "" && (track.MBID == "" || overwrite) {
		track.MBID = meta.MBID
		changed = true
	}
	if enrichment != nil {
		if enrichment.Title != "" && (track.Title != enrichment.Title || overwrite) {
			track.Title = enrichment.Title
			changed = true
		}
		if enrichment.TrackMBID != "" && (track.MBID == "" || overwrite) {
			track.MBID = enrichment.TrackMBID
			changed = true
		}
		if len(enrichment.Artists) > 0 {
			trackArtists, err := trackRepo.LoadTrackArtists(ctx, track.ID)
			if err == nil {
				allUnknown := true
				for _, ta := range trackArtists {
					artist, err := artistRepo.FindByID(ctx, ta.ArtistID)
					if err != nil {
						continue
					}
					if artist.Name != "Unknown Artist" {
						allUnknown = false
					}
					match := matchArtistEnrichment(artist.Name, enrichment)
					updated := false
					if match != nil && match.ArtistMBID != "" && (artist.MBID == "" || overwrite) {
						artist.MBID = match.ArtistMBID
						updated = true
					}
					if match != nil && match.ArtistCountry != "" && (artist.Country == "" || overwrite) {
						artist.Country = match.ArtistCountry
						updated = true
					}
					if match != nil && match.Artist != "" && (artist.Name == "" || artist.Name == "Unknown Artist" || overwrite) {
						artist.Name = match.Artist
						artist.SortName = match.Artist
						updated = true
					}
					if updated {
						changed = true
						artistRepo.Update(ctx, artist)
					}
				}
				if allUnknown {
					var newArtists []*domain.TrackArtist
					for i, ar := range enrichment.Artists {
						if ar.Name == "" {
							continue
						}
						enrich := &metadata.EnrichmentResult{
							ArtistMBID:    ar.MBID,
							ArtistCountry: ar.Country,
							Artist:        ar.Name,
						}
						artist, err := findOrCreateArtist(ctx, er, artistRepo, libraryID, ar.Name, enrich)
						if err != nil {
							continue
						}
						newArtists = append(newArtists, &domain.TrackArtist{
							ArtistID:  artist.ID,
							Role:      "performer",
							SortOrder: i,
							Artist:    artist,
						})
					}
					if len(newArtists) > 0 {
						trackRepo.ReplaceTrackArtists(ctx, track.ID, newArtists)
						changed = true
					}
				}
			}
		}
		if enrichment.AlbumMBID != "" && len(track.Albums) > 0 {
			album, err := albumRepo.FindByID(ctx, track.Albums[0].AlbumID)
			if err == nil {
				updated := false
				if enrichment.AlbumMBID != "" && (album.MBID == "" || overwrite) {
					album.MBID = enrichment.AlbumMBID
					updated = true
				}
				if enrichment.Album != "" && album.Title != enrichment.Album {
					album.Title = enrichment.Album
					updated = true
				}
				if enrichment.Year != 0 && (album.Year == 0 || overwrite) {
					album.Year = enrichment.Year
					updated = true
				}
				if enrichment.Genre != "" && (album.Genre == "" || overwrite) {
					album.Genre = enrichment.Genre
					updated = true
				}
				if enrichment.AlbumCountry != "" && (album.Country == "" || overwrite) {
					album.Country = enrichment.AlbumCountry
					updated = true
				}
				if updated {
					changed = true
					albumRepo.Update(ctx, album)
				}
			}
		}
	}
	return
}

// resolveVersions groups tracks by (metadata_source, mbid) and sets
// version/version_label.
func (e *Engine) resolveVersions(ctx context.Context, libraryID string) error {
	rows, err := e.db.QueryContext(ctx,
		`SELECT metadata_source, mbid, array_agg(id ORDER BY
		 CASE file_format
		 WHEN 'flac' THEN 0 WHEN 'alac' THEN 1 WHEN 'wav' THEN 2
		 WHEN 'aiff' THEN 3 WHEN 'mp3' THEN 4 WHEN 'aac' THEN 5
		 WHEN 'ogg' THEN 6 WHEN 'opus' THEN 7 ELSE 8 END,
		 bit_rate DESC,
		 file_path
		 ) AS ids
		 FROM tracks WHERE library_id = $1 AND mbid != ''
		 GROUP BY metadata_source, mbid
		 ORDER BY metadata_source, mbid`, libraryID)
	if err != nil {
		return fmt.Errorf("query tracks by mbid: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var mbid string
		var source string
		var ids []string
		if err := rows.Scan(&source, &mbid, pq.Array(&ids)); err != nil {
			return fmt.Errorf("scan mbid group: %w", err)
		}

		if len(ids) < 2 {
			e.db.ExecContext(ctx, `UPDATE tracks SET version = 0, version_label = '' WHERE id = $1`, ids[0])
			e.db.ExecContext(ctx, `DELETE FROM track_version_groups WHERE metadata_source = $1 AND mbid = $2 AND library_id = $3`, source, mbid, libraryID)
			continue
		}

		for i, id := range ids {
			version := 0
			if i == 0 {
				version = 1
			} else {
				version = i + 1
			}
			label := ExtractVersionLabel(ctx, e.db, id)
			res, err := e.db.ExecContext(ctx, `UPDATE tracks SET version = $1, version_label = $2 WHERE id = $3`, version, label, id)
			if err != nil {
				log.Printf("[scan] version update error: source=%s mbid=%s ver=%d id=%s err=%v", source, mbid, version, id, err)
			} else if n, _ := res.RowsAffected(); n == 0 {
				log.Printf("[scan] version update affected 0 rows: source=%s mbid=%s ver=%d id=%s", source, mbid, version, id)
			}
			e.db.ExecContext(ctx,
				`INSERT INTO track_version_groups (metadata_source, mbid, library_id, track_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				source, mbid, libraryID, id)
		}
	}

	// Clean up: tracks that used to be in a group but no longer have a group mbid
	e.db.ExecContext(ctx,
		`DELETE FROM track_version_groups WHERE library_id = $1 AND (metadata_source, mbid) NOT IN (SELECT DISTINCT metadata_source, mbid FROM tracks WHERE library_id = $1 AND mbid != '')`, libraryID)

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate mbid groups: %w", err)
	}
	return nil
}

// ExtractVersionLabel extracts a human-readable version description from a track's file path.
func ExtractVersionLabel(ctx context.Context, db *sql.DB, trackID string) string {
	var filePath, fileFormat, title, artist, album string
	var bitRate int
	if err := db.QueryRowContext(ctx,
		`SELECT t.file_path, t.file_format, t.bit_rate, t.title,
		        COALESCE((SELECT a2.name FROM track_artists ta2 JOIN artists a2 ON a2.id = ta2.artist_id WHERE ta2.track_id = t.id ORDER BY ta2.sort_order LIMIT 1), ''),
		        COALESCE((SELECT al.title FROM track_albums tal JOIN albums al ON al.id = tal.album_id WHERE tal.track_id = t.id ORDER BY tal.disc_number, tal.track_number LIMIT 1), '')
		 FROM tracks t WHERE t.id = $1`, trackID).Scan(&filePath, &fileFormat, &bitRate, &title, &artist, &album); err != nil {
		return ""
	}

	keywords := []string{"live", "acoustic", "remaster", "remastered", "deluxe", "bonus",
		"demo", "instrumental", "edit", "extended", "mix", "radio", "karaoke", "unplugged",
		"anniversary", "orchestral", "piano", "reprise"}

	dir := strings.ToLower(filepath.Base(filepath.Dir(filePath)))
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))

	for _, kw := range keywords {
		if strings.Contains(dir, kw) || strings.Contains(stem, kw) {
			return fmt.Sprintf("%s · %s %dkbps", titleCase(kw), strings.ToUpper(fileFormat), bitRate/1000)
		}
	}

	if label := extractFromPath(dir, stem, title, artist, album, filePath); label != "" {
		return fmt.Sprintf("%s · %s %dkbps", label, strings.ToUpper(fileFormat), bitRate/1000)
	}

	return fmt.Sprintf("%s %dkbps", strings.ToUpper(fileFormat), bitRate/1000)
}

func extractFromPath(dir, stem, title, artist, album, filePath string) string {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")

	blacklist := make(map[string]bool)
	for _, w := range strings.Fields(title) {
		if len(w) > 1 {
			blacklist[strings.ToLower(w)] = true
		}
	}
	for _, w := range strings.Fields(album) {
		if len(w) > 1 {
			blacklist[strings.ToLower(w)] = true
		}
	}
	for _, w := range strings.Fields(artist) {
		if len(w) > 1 {
			blacklist[strings.ToLower(w)] = true
		}
	}
	blacklist[ext] = true

	tokens := splitByPunct(dir + " " + stem)
	var kept []string
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		if lower == "" || len(lower) <= 1 {
			continue
		}
		if isYear(lower) {
			continue
		}
		if blacklist[lower] {
			continue
		}
		kept = append(kept, titleCase(tok))
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, ", ")
}

func splitByPunct(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '(' || r == ')' ||
			r == '[' || r == ']' || r == ',' || ' ' == r ||
			r == '/' || r == '\\' || r == '&' || r == '!' || r == '\'' ||
			r == '"' || r == ':' || r == ';' || r == '~' || r == '#'
	})
}

func isYear(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func titleCase(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	runes[0] = rune(unicode.ToUpper(runes[0]))
	return string(runes)
}
