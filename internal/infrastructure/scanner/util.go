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
)

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

func ExtractVersionLabel(ctx context.Context, db *sql.DB, trackID string) string {
	var filePath, fileFormat, title, artist, album string
	var bitRate int
	if err := db.QueryRowContext(ctx,
		`SELECT t.file_path, t.file_format, t.bit_rate, t.title,
		        COALESCE((SELECT string_agg(a2.name, ',' ORDER BY ta2.sort_order)
		                  FROM track_artists ta2 JOIN artists a2 ON a2.id = ta2.artist_id
		                  WHERE ta2.track_id = t.id), ''),
		        COALESCE((SELECT al.title FROM track_albums tal JOIN albums al ON al.id = tal.album_id WHERE tal.track_id = t.id ORDER BY tal.disc_number, tal.track_number LIMIT 1), '')
		 FROM tracks t WHERE t.id = $1`, trackID).Scan(&filePath, &fileFormat, &bitRate, &title, &artist, &album); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[scan] version label query error for %s: %v", trackID, err)
		}
		return ""
	}

	keywords := []string{"live", "acoustic", "remaster", "remastered", "deluxe", "bonus",
		"demo", "instrumental", "edit", "extended", "mix", "radio", "karaoke", "unplugged",
		"anniversary", "orchestral", "piano", "reprise"}

	dir := strings.ToLower(filepath.Base(filepath.Dir(filePath)))
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))

	// Version keywords must match whole tokens, not substrings: `stem` is the
	// track title, so substring matching would mislabel ordinary titles
	// ("Radio Ga Ga"→radio, "Live Forever"→live, "Piano Man"→piano) and words
	// merely containing a keyword ("Deliverance"→live, "Credits"→edit,
	// "Remix"/"Mixtape"→mix). The title/album/artist tokens are blacklisted
	// (the same way extractFromPath does) so the track's own name never
	// produces a label.
	blacklist := make(map[string]bool)
	for _, raw := range []string{title, album, artist} {
		for _, tok := range splitByPunct(raw) {
			if len(tok) > 1 {
				blacklist[strings.ToLower(tok)] = true
			}
		}
	}
	for _, tok := range splitByPunct(dir + " " + stem) {
		lower := strings.ToLower(tok)
		if lower == "" || len(lower) <= 1 || isYear(lower) || blacklist[lower] {
			continue
		}
		for _, kw := range keywords {
			if lower == kw {
				return fmt.Sprintf("%s · %s%s", titleCase(kw), strings.ToUpper(fileFormat), versionBitRate(bitRate))
			}
		}
	}

	if label := extractFromPath(dir, stem, title, artist, album, filePath); label != "" {
		return fmt.Sprintf("%s · %s%s", label, strings.ToUpper(fileFormat), versionBitRate(bitRate))
	}

	return fmt.Sprintf("%s%s", strings.ToUpper(fileFormat), versionBitRate(bitRate))
}

// versionBitRate renders the bit-rate suffix for a version label. A zero
// bit_rate (ffprobe did not report one for formats like WAV/DSD) must not
// surface as a bogus "0kbps", so the suffix is omitted entirely.
func versionBitRate(bitRate int) string {
	if bitRate > 0 {
		return fmt.Sprintf(" %dkbps", bitRate/1000)
	}
	return ""
}

func extractFromPath(dir, stem, title, artist, album, filePath string) string {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")

	blacklist := make(map[string]bool)
	// Blacklist the known fields (title/album/artist) split the same way the
	// path tokens are (punctuation-aware), so names like "司夏,河图" or
	// "Poppin'Party,Glitter*Green" are excluded piece by piece instead of
	// leaking their trailing parts into the version label.
	for _, raw := range []string{title, album, artist} {
		for _, tok := range splitByPunct(raw) {
			if len(tok) > 1 {
				blacklist[strings.ToLower(tok)] = true
			}
		}
	}
	blacklist[strings.TrimPrefix(strings.ToLower(ext), ".")] = true
	// Disc/track markers ("Disc 1", "Track 01") describe placement, not a
	// version, and would otherwise leak a "Disc, Track" label.
	blacklist["disc"] = true
	blacklist["disk"] = true
	blacklist["cd"] = true
	blacklist["track"] = true

	tokens := splitByPunct(dir + " " + stem)
	var kept []string
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		if lower == "" || len(lower) <= 1 {
			continue
		}
		if isYear(lower) || isTrackNumber(lower) {
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
			r == '"' || r == ':' || r == ';' || r == '~' || r == '#' ||
			r == '*' || r == '·' || r == '，' || r == '。' || r == '、' ||
			r == '（' || r == '）' || r == '【' || r == '】' || r == '：' || r == '！' || r == '？'
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

// isTrackNumber reports whether s looks like a track/disc number (1-3 digits,
// optionally zero-padded like "01"). Such tokens are placement markers, not
// version info, and must not leak into a version label.
func isTrackNumber(s string) bool {
	if len(s) == 0 || len(s) > 3 {
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
