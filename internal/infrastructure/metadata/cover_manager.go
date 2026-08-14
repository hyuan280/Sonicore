package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

// CoverManager owns the cover-image lifecycle: extraction of embedded cover
// art to disk, the images-table rows (original + variants), and cleanup.
//
// Layout conventions:
//   - track originals: {images}/{library}/track_{id}.jpg
//   - track thumbnails: {images}/{library}/track_{id}_64.jpg
//   - album originals reference a track's original path (no copy)
//   - album thumbnails: {images}/album/album_{id}_256.jpg
//
// The 64/256 variant is only produced when the source is larger than the
// target; otherwise the variant references the original path, so a missing
// thumbnail file is a normal state, not a missing cover.
type CoverManager struct {
	imagesDir string
	extractor *CoverExtractor
	images    *repository.ImageRepo
	albums    *repository.AlbumRepo
	tracks    *repository.TrackRepo

	// extractMu serializes re-extraction so concurrent cover requests for
	// the same missing file cannot race on file writes and row replacement.
	extractMu sync.Mutex

	// recentFails memoizes failed extractions (keyed by track/album id) so a
	// broken track (e.g. its audio file was deleted) is not re-attempted by
	// spawning ffmpeg on every cover request.
	recentFails map[string]time.Time
	failMu      sync.Mutex
}

const coverFailCooldown = 60 * time.Second

func NewCoverManager(imagesDir string, db *sql.DB) *CoverManager {
	return &CoverManager{
		imagesDir:   imagesDir,
		extractor:   NewCoverExtractor(imagesDir),
		images:      repository.NewImageRepo(db),
		albums:      repository.NewAlbumRepo(db),
		tracks:      repository.NewTrackRepo(db),
		recentFails: make(map[string]time.Time),
	}
}

func (m *CoverManager) recentFail(key string) bool {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	if t, ok := m.recentFails[key]; ok && time.Since(t) < coverFailCooldown {
		return true
	}
	return false
}

func (m *CoverManager) noteFail(key string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	// Opportunistically purge expired entries so the map stays bounded over
	// the server's lifetime.
	for k, t := range m.recentFails {
		if time.Since(t) >= coverFailCooldown {
			delete(m.recentFails, k)
		}
	}
	m.recentFails[key] = time.Now()
}

func (m *CoverManager) clearFail(key string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	delete(m.recentFails, key)
}

// TrackCoverComplete reports whether the track's cover is present. In
// non-overwrite mode the DB pointer alone is authoritative; in overwrite
// mode the stored original must also exist on disk. The original path is
// derived from the fixed layout, avoiding an images-row lookup per track
// during overwrite scans.
func (m *CoverManager) TrackCoverComplete(ctx context.Context, track *domain.Track, overwrite bool) bool {
	if track == nil || track.CoverImageID == nil {
		return false
	}
	if !overwrite {
		return true
	}
	p := CoverPath(m.imagesDir, track.LibraryID, "track", track.ID, "jpg")
	return CoverFileExists(p)
}

// AlbumCoverComplete mirrors TrackCoverComplete for albums.
func (m *CoverManager) AlbumCoverComplete(ctx context.Context, album *domain.Album, overwrite bool) bool {
	if album == nil || album.CoverImageID == nil {
		return false
	}
	if !overwrite {
		return true
	}
	img, err := m.images.FindByID(ctx, *album.CoverImageID)
	if err != nil {
		return false
	}
	return CoverFileExists(img.Path)
}

// ExtractTrackCover extracts the embedded cover of a track, writes the
// original (and 64px thumbnail when the source is larger), records the
// images row, persists the track, and backfills the album cover from this
// track when the album has none.
//
// Files are written under temporary names and renamed only after the new
// images row exists, so a failure at any step leaves the previous row and
// files intact.
func (m *CoverManager) ExtractTrackCover(ctx context.Context, libraryID string, track *domain.Track, album *domain.Album, force bool) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	// The failure memo only applies to on-demand HTTP restores; scans pass
	// force=true so a previously failing track is retried.
	if !force && m.recentFail(track.ID) {
		return fmt.Errorf("cover extraction recently failed for %s", track.ID)
	}
	// Concurrent requests for the same missing cover serialize on the lock;
	// if a previous request already restored every file (original and
	// variants), nothing to do.
	if !force && track.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && imageFilesIntact(img) {
			return nil
		}
	}

	data, _, err := m.extractor.ExtractFromFile(ctx, track.FilePath)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}
	// Originals are stored as JPEG regardless of the embedded format.
	data, err = ensureJPEG(data)
	if err != nil {
		m.noteFail(track.ID)
		return err
	}

	thumbDir := filepath.Join(m.imagesDir, libraryID)
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		m.noteFail(track.ID)
		return err
	}
	mainPath := CoverPath(m.imagesDir, libraryID, "track", track.ID, "jpg")
	thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", track.ID))
	format, w, h, hash := imageInfo(data)

	// Content unchanged (a plain restore of a deleted file): the existing
	// images row and cover_image_id stay untouched, only the files are
	// written back. Content changed (replaced file / overwrite scan) or a
	// missing row: rebuild the row and refresh album rows referencing it.
	contentChanged := true
	if track.CoverImageID != nil {
		if old, err := m.images.FindByID(ctx, *track.CoverImageID); err == nil && old.Hash == hash {
			contentChanged = false
		}
	}

	// Stage files under temporary names; nothing on disk is mutated until
	// the new row exists (or, for a plain restore, until we are ready to
	// commit the files).
	tmpMain := mainPath + ".tmp"
	if err := os.WriteFile(tmpMain, data, 0644); err != nil {
		m.noteFail(track.ID)
		return err
	}
	cleanupTmp := func() {
		os.Remove(tmpMain)
		os.Remove(thumbPath + ".tmp")
	}

	var tmpThumb string
	if w > 64 || h > 64 {
		tmpThumb = thumbPath + ".tmp"
		if err := ResizeToThumbnail(data, tmpThumb, 64); err != nil {
			os.Remove(tmpThumb)
			log.Printf("[cover] thumbnail resize error for %s: %v", track.ID, err)
		}
	}
	thumbWritten := tmpThumb != "" && fileSize(tmpThumb) > 0

	commitFiles := func() error {
		if err := os.Rename(tmpMain, mainPath); err != nil {
			cleanupTmp()
			return err
		}
		if thumbWritten {
			if err := os.Rename(tmpThumb, thumbPath); err != nil {
				os.Remove(tmpThumb)
				cleanupTmp()
				return err
			}
		} else {
			// Small source: drop any stale thumbnail and reference the
			// original.
			os.Remove(thumbPath)
		}
		return nil
	}

	// rollbackNewRow undoes a committed pointer change and removes the new
	// row after a file-commit failure, so the track returns to the previous
	// row (its files may hold new bytes; the next scan re-syncs via hash).
	rollbackNewRow := func(newImgID, prevImageID string) {
		if prevImageID != "" {
			m.tracks.SetCoverImage(ctx, track.ID, prevImageID)
		}
		m.images.Delete(ctx, newImgID)
	}

	if contentChanged {
		variants := domain.ImageVariants{}
		if thumbWritten {
			sw, sh := scaledDims(w, h, 64)
			variants = append(variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(tmpThumb)})
		} else {
			variants = append(variants, domain.ImageVariant{Path: mainPath, Width: w, Height: h, Size: int64(len(data))})
		}

		img := &domain.Image{
			ID:        domain.NewID(),
			LibraryID: libraryID,
			OwnerType: "track",
			OwnerID:   track.ID,
			Source:    "embedded",
			Path:      mainPath,
			Format:    format,
			Width:     w,
			Height:    h,
			Size:      int64(len(data)),
			Hash:      hash,
			Variants:  variants,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := m.images.Create(ctx, img); err != nil {
			cleanupTmp()
			m.noteFail(track.ID)
			return err
		}

		// Repoint the track before touching the files: a commit failure then
		// rolls back the pointer and drops the new row, leaving the previous
		// row authoritative.
		prevImageID := ""
		if track.CoverImageID != nil {
			prevImageID = *track.CoverImageID
		}
		if err := m.tracks.SetCoverImage(ctx, track.ID, img.ID); err != nil {
			m.images.Delete(ctx, img.ID)
			cleanupTmp()
			m.noteFail(track.ID)
			return err
		}

		if err := commitFiles(); err != nil {
			rollbackNewRow(img.ID, prevImageID)
			m.noteFail(track.ID)
			return err
		}

		// Files and pointer are committed; retire the stale row now.
		if prevImageID != "" {
			if err := m.images.Delete(ctx, prevImageID); err != nil {
				log.Printf("[cover] delete stale track image row: %v", err)
			}
		}
		track.CoverImageID = &img.ID

		m.refreshAlbumsReferencing(ctx, mainPath, data, w, h)
	} else {
		// Plain restore: content identical, only the files were missing.
		if err := commitFiles(); err != nil {
			m.noteFail(track.ID)
			return err
		}
	}
	m.clearFail(track.ID)

	if album != nil && album.CoverImageID == nil {
		if id, err := m.createAlbumImage(ctx, album, mainPath, data, w, h); err == nil {
			album.CoverImageID = &id
			if err := m.albums.Update(ctx, album); err != nil {
				log.Printf("[cover] album cover update error for %s: %v", album.ID, err)
				if derr := m.images.Delete(ctx, id); derr != nil {
					log.Printf("[cover] rollback album image row %s: %v", id, derr)
				}
				album.CoverImageID = nil
				m.noteFail("album:" + album.ID)
			} else {
				m.clearFail("album:" + album.ID)
			}
		} else {
			log.Printf("[cover] album cover create error for %s: %v", album.ID, err)
			m.noteFail("album:" + album.ID)
		}
	}
	return nil
}

// BackfillAlbumCover creates the album cover from its first cover-bearing
// track's original file without re-extracting the track. Non-destructive:
// the previous album row is only removed after the replacement row exists.
func (m *CoverManager) BackfillAlbumCover(ctx context.Context, album *domain.Album, force bool) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	key := "album:" + album.ID
	if !force && m.recentFail(key) {
		return fmt.Errorf("album cover backfill recently failed for %s", album.ID)
	}
	// Concurrent requests serialize on the lock; if a previous request
	// already restored every file (original and variants), nothing to do.
	if !force && album.CoverImageID != nil {
		if img, err := m.images.FindByID(ctx, *album.CoverImageID); err == nil && imageFilesIntact(img) {
			return nil
		}
	}

	tracks, err := m.tracks.FindByAlbumID(ctx, album.ID)
	if err != nil {
		return err
	}
	var trackPath string
	var data []byte
	var w, h int
	for i := range tracks {
		if tracks[i].CoverImageID == nil {
			continue
		}
		img, err := m.images.FindByID(ctx, *tracks[i].CoverImageID)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(img.Path)
		if err != nil {
			continue
		}
		trackPath = img.Path
		data = b
		_, w, h, _ = imageInfo(b)
		break
	}
	if trackPath == "" {
		m.noteFail(key)
		return fmt.Errorf("no usable cover-bearing track for album %s", album.ID)
	}

	// Content unchanged (a plain restore of deleted thumbnail files): keep
	// the existing row and cover_image_id, only rewrite the 256px file so
	// clients holding the current image id keep working.
	newHash := contentHash(data)
	if album.CoverImageID != nil {
		if stale, serr := m.images.FindByID(ctx, *album.CoverImageID); serr == nil && stale.Hash == newHash {
			if w > 256 || h > 256 {
				thumbDir := filepath.Join(m.imagesDir, "album")
				if err := os.MkdirAll(thumbDir, 0755); err != nil {
					log.Printf("[cover] album thumbnail dir create error for %s: %v", album.ID, err)
				} else {
					thumbPath := filepath.Join(thumbDir, fmt.Sprintf("album_%s_256.jpg", album.ID))
					if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
						log.Printf("[cover] album thumbnail resize error for %s: %v", album.ID, err)
					}
				}
			}
			m.clearFail(key)
			return nil
		}
	}

	// Content changed: build the replacement first (the new 256px file
	// overwrites the deterministic path — the old bytes are being replaced
	// anyway), then repoint the album, and only then retire the stale row.
	prevImageID := ""
	if album.CoverImageID != nil {
		prevImageID = *album.CoverImageID
	}
	newID, err := m.createAlbumImage(ctx, album, trackPath, data, w, h)
	if err != nil {
		m.noteFail(key)
		return err
	}

	album.CoverImageID = &newID
	if err := m.albums.Update(ctx, album); err != nil {
		// The new row would be orphaned; drop it and keep the previous row
		// authoritative so repeated requests do not accumulate rows.
		if derr := m.images.Delete(ctx, newID); derr != nil {
			log.Printf("[cover] rollback album image row %s: %v", newID, derr)
		}
		m.noteFail(key)
		return err
	}

	// Album pointer persisted; retire the stale row (never the referenced
	// track original).
	if prevImageID != "" {
		if err := m.images.Delete(ctx, prevImageID); err != nil {
			log.Printf("[cover] delete stale album image row: %v", err)
		}
	}
	m.clearFail(key)
	return nil
}

// createAlbumImage builds an album cover row that references a track's
// original file. The 256px thumbnail is written to the album directory only
// when the source is larger and the resize succeeds; otherwise the variant
// references the original. Album covers are shared across libraries and
// carry no library id. Returns the new images-row id.
func (m *CoverManager) createAlbumImage(ctx context.Context, album *domain.Album, trackMainPath string, data []byte, w, h int) (string, error) {
	variants := domain.ImageVariants{}
	thumbPath := filepath.Join(m.imagesDir, "album", fmt.Sprintf("album_%s_256.jpg", album.ID))
	if w > 256 || h > 256 {
		thumbDir := filepath.Join(m.imagesDir, "album")
		if err := os.MkdirAll(thumbDir, 0755); err != nil {
			return "", err
		}
		if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
			log.Printf("[cover] album thumbnail resize error for %s: %v", album.ID, err)
		}
		if fileSize(thumbPath) > 0 {
			sw, sh := scaledDims(w, h, 256)
			variants = append(variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(thumbPath)})
		}
	} else {
		// Small source: drop any stale thumbnail and reference the original.
		os.Remove(thumbPath)
	}
	if len(variants) == 0 {
		variants = append(variants, domain.ImageVariant{Path: trackMainPath, Width: w, Height: h, Size: int64(len(data))})
	}

	img := &domain.Image{
		ID:        domain.NewID(),
		OwnerType: "album",
		OwnerID:   album.ID,
		Source:    "embedded",
		Path:      trackMainPath,
		Format:    imageFormat(data),
		Width:     w,
		Height:    h,
		Size:      int64(len(data)),
		Hash:      contentHash(data),
		Variants:  variants,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := m.images.Create(ctx, img); err != nil {
		return "", err
	}
	return img.ID, nil
}

// refreshAlbumsReferencing updates album rows whose original points at the
// given track path after the track was re-extracted: metadata (format,
// dimensions, hash, size) is refreshed and the 256px thumbnail regenerated
// so album covers do not serve stale data.
func (m *CoverManager) refreshAlbumsReferencing(ctx context.Context, path string, data []byte, w, h int) {
	imgs, err := m.images.FindByPath(ctx, "album", path)
	if err != nil {
		log.Printf("[cover] find albums referencing %s: %v", path, err)
		return
	}
	for i := range imgs {
		img := &imgs[i]
		img.Format = imageFormat(data)
		img.Width = w
		img.Height = h
		img.Size = int64(len(data))
		img.Hash = contentHash(data)
		img.UpdatedAt = time.Now()
		img.Variants = domain.ImageVariants{}
		thumbPath := filepath.Join(m.imagesDir, "album", fmt.Sprintf("album_%s_256.jpg", img.OwnerID))
		if w > 256 || h > 256 {
			if err := ResizeToThumbnail(data, thumbPath, 256); err != nil {
				log.Printf("[cover] refresh thumbnail resize error for %s: %v", img.OwnerID, err)
			}
			if fileSize(thumbPath) > 0 {
				sw, sh := scaledDims(w, h, 256)
				img.Variants = append(img.Variants, domain.ImageVariant{Path: thumbPath, Width: sw, Height: sh, Size: fileSize(thumbPath)})
			}
		} else {
			// Small source: drop any stale thumbnail and reference the
			// original.
			os.Remove(thumbPath)
		}
		if len(img.Variants) == 0 {
			img.Variants = append(img.Variants, domain.ImageVariant{Path: path, Width: w, Height: h, Size: int64(len(data))})
		}
		if err := m.images.Update(ctx, img); err != nil {
			log.Printf("[cover] refresh album row %s: %v", img.ID, err)
		}
	}
}

// DeleteTrackCovers removes the track's images row(s) and files, plus any
// album rows that referenced the track original (their covers become null
// through the foreign key).
func (m *CoverManager) DeleteTrackCovers(ctx context.Context, libraryID, trackID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	for _, ext := range []string{"jpg", "png"} {
		mainPath := CoverPath(m.imagesDir, libraryID, "track", trackID, ext)
		if err := m.deleteAlbumsReferencing(ctx, mainPath); err != nil {
			return err
		}
	}
	if err := m.images.DeleteByOwner(ctx, "track", trackID); err != nil {
		return err
	}
	os.Remove(CoverPath(m.imagesDir, libraryID, "track", trackID, "jpg"))
	os.Remove(CoverPath(m.imagesDir, libraryID, "track", trackID, "png"))
	os.Remove(CoverPathWithSuffix(m.imagesDir, libraryID, "track", trackID, "_64", "jpg"))
	return nil
}

// DeleteAlbumCovers removes the album's images row(s) and files.
func (m *CoverManager) DeleteAlbumCovers(ctx context.Context, albumID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	if err := m.images.DeleteByOwner(ctx, "album", albumID); err != nil {
		return err
	}
	RemoveAlbumCover(m.imagesDir, albumID)
	return nil
}

// DeleteArtistCovers removes the artist's images row(s) and files.
func (m *CoverManager) DeleteArtistCovers(ctx context.Context, artistID string) error {
	m.extractMu.Lock()
	defer m.extractMu.Unlock()

	if err := m.images.DeleteByOwner(ctx, "artist", artistID); err != nil {
		return err
	}
	os.Remove(CoverPath(m.imagesDir, "artist", "artist", artistID, "jpg"))
	return nil
}

func (m *CoverManager) deleteAlbumsReferencing(ctx context.Context, path string) error {
	imgs, err := m.images.FindByPath(ctx, "album", path)
	if err != nil {
		return err
	}
	for _, img := range imgs {
		if err := m.images.Delete(ctx, img.ID); err != nil {
			return err
		}
		// The albums.cover_image_id column clears via ON DELETE SET NULL.
		RemoveAlbumCover(m.imagesDir, img.OwnerID)
	}
	return nil
}

// imageFilesIntact reports whether the original and every recorded variant
// of an images row exist on disk. Used by the locked re-check so a
// concurrent request that just restored the files short-circuits.
func imageFilesIntact(img *domain.Image) bool {
	if !CoverFileExists(img.Path) {
		return false
	}
	for _, v := range img.Variants {
		if !CoverFileExists(v.Path) {
			return false
		}
	}
	return true
}

// ImageVariantPath picks the file to serve for a requested size: the largest
// variant that fits within the target (64/256); when the best fitting variant
// is far below the target (e.g. a track's 64px thumb for a 256 request) or no
// variant fits, the original is served. A missing or zero size resolves to
// the original.
func ImageVariantPath(img *domain.Image, size int) string {
	target := 0
	switch {
	case size > 256:
		target = 0
	case size > 64:
		target = 256
	case size > 0:
		target = 64
	default:
		target = 0
	}
	if target == 0 {
		return img.Path
	}
	best := ""
	bestWidth := 0
	for _, v := range img.Variants {
		if v.Width > 0 && v.Width <= target && v.Width > bestWidth {
			best = v.Path
			bestWidth = v.Width
		}
	}
	if best != "" && bestWidth >= target/2 {
		return best
	}
	return img.Path
}

// ensureJPEG re-encodes non-JPEG cover bytes (PNG/GIF/WebP/...) as JPEG so
// every original is stored as .jpg. Payloads that cannot be decoded (e.g.
// AVIF) are rejected so they are never persisted under a .jpg path with a
// wrong content type.
func ensureJPEG(data []byte) ([]byte, error) {
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode cover image: %w", err)
	}
	if format == "jpeg" {
		return data, nil
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode cover as jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func scaledDims(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	scale := float64(max) / float64(w)
	if h > w {
		scale = float64(max) / float64(h)
	}
	return int(float64(w) * scale), int(float64(h) * scale)
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func imageFormat(data []byte) string {
	format, _, _, _ := imageInfo(data)
	return format
}

func imageInfo(data []byte) (format string, w, h int, hash string) {
	if cfg, f, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		format, w, h = f, cfg.Width, cfg.Height
	}
	return format, w, h, contentHash(data)
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
