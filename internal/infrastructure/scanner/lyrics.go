package scanner

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
)

func (e *Engine) applyNetworkLyrics(ctx context.Context, libraryID string, track *domain.Track, enrichment *metadata.EnrichmentResult) bool {
	if enrichment == nil || enrichment.Lyrics == "" {
		return false
	}
	lower := lyrics.PriorityBit(lyrics.PriorityEmbedded) |
		lyrics.PriorityBit(lyrics.PrioritySidecar) |
		lyrics.PriorityBit(lyrics.PriorityUser)
	if track.LyricsMask&lyrics.PriorityBit(lyrics.PriorityNetwork) != 0 || track.LyricsMask&lower != 0 {
		return false
	}
	if err := e.lyricsStore.Save(ctx, libraryID, track.ID, lyrics.PriorityNetwork, enrichment.Lyrics); err != nil {
		if ctx.Err() != nil {
			log.Printf("[scan] network lyrics save cancelled for %s: %v (original: %v)", track.FilePath, ctx.Err(), err)
		} else {
			log.Printf("[scan] network lyrics save error for %s: %v", track.FilePath, err)
		}
		return false
	}
	track.LyricsMask |= lyrics.PriorityBit(lyrics.PriorityNetwork)
	return true
}

// sweepOrphanLyrics removes lyrics files whose track row no longer exists in
// the owning library (interrupted deletions or manual database edits). Lyrics
// are plain files with no DB rows, so they can only be swept by walking the
// filesystem. Best-effort: every failure path logs and moves on, so the sweep
// never blocks or aborts the surrounding scan. Missing library dirs are
// skipped.
//
// The sweep is scoped to the library being scanned: lyrics are written by the
// scan of the SAME library (Save writes the file, then the track row commits),
// so scanning only this library's dir cannot race a concurrent scan of
// another library that has written a fresh lyric file whose track row is not
// committed yet.
func (e *Engine) sweepOrphanLyrics(ctx context.Context, libraryID string) {
	libDir := filepath.Join(e.lyricsStore.Dir(), libraryID)
	matches, err := filepath.Glob(filepath.Join(libDir, "*_p*.*"))
	if err != nil {
		log.Printf("[scan] orphan lyrics glob error %s: %v", libDir, err)
		return
	}
	if len(matches) == 0 {
		return
	}
	// Batch-load the existing track ids once instead of one query per file.
	existing := make(map[string]bool)
	rows, err := e.db.QueryContext(ctx, `SELECT id FROM tracks WHERE library_id = $1`, libraryID)
	if err != nil {
		log.Printf("[scan] orphan lyrics load track ids: %v", err)
		return
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			log.Printf("[scan] orphan lyrics scan track id: %v", err)
			return
		}
		existing[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("[scan] orphan lyrics iterate track ids: %v", err)
		return
	}

	for _, f := range matches {
		if !strings.HasSuffix(f, ".txt") && !strings.HasSuffix(f, ".lrc") {
			continue
		}
		base := filepath.Base(f)
		// Only files matching the store's own layout are candidates:
		// "{trackID}_p{0-3}.{txt|lrc}". The "_p" must be the LAST one and
		// followed by a single priority digit, and the prefix must be a
		// valid track id, so a hand-made file like "my_song_playlist.lrc"
		// is never deleted.
		idx := strings.LastIndex(base, "_p")
		if idx <= 0 || idx+3 > len(base) || base[idx+2] < '0' || base[idx+2] > '9' {
			continue
		}
		trackID := base[:idx]
		if !validTrackID(trackID) {
			continue
		}
		if existing[trackID] {
			continue
		}
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			log.Printf("[scan] remove orphan lyrics %s: %v", f, err)
		}
	}
}

// validTrackID reports whether s looks like a program-generated track id
// (domain.NewID: 26 lowercase hex chars).
func validTrackID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
