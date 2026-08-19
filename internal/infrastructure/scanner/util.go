package scanner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
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

type TrackVersionInfo struct {
	ID, FilePath, FileFormat, Title, Artist, Album, VersionLabel string
	BitRate                                                      int
}

func ExtractVersionLabelFromInfo(info TrackVersionInfo) string {
	dir := strings.ToLower(filepath.Base(filepath.Dir(info.FilePath)))
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(info.FilePath), filepath.Ext(info.FilePath)))

	blacklist := make(map[string]bool)
	for _, raw := range []string{info.Title, info.Album, info.Artist} {
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
		if versionKeyword(lower) {
			return fmt.Sprintf("%s · %s%s", titleCase(lower), strings.ToUpper(info.FileFormat), versionBitRate(info.BitRate))
		}
	}

	if label := extractFromPath(dir, stem, info.Title, info.Artist, info.Album, info.FilePath); label != "" {
		return fmt.Sprintf("%s · %s%s", label, strings.ToUpper(info.FileFormat), versionBitRate(info.BitRate))
	}

	return fmt.Sprintf("%s%s", strings.ToUpper(info.FileFormat), versionBitRate(info.BitRate))
}

func ExtractVersionLabel(ctx context.Context, db *sql.DB, trackID string) (string, error) {
	var info TrackVersionInfo
	info.ID = trackID
	if err := db.QueryRowContext(ctx,
		`SELECT t.file_path, t.file_format, t.bit_rate, t.title,
		        COALESCE((SELECT string_agg(a2.name, ',' ORDER BY ta2.sort_order)
		                  FROM track_artists ta2 JOIN artists a2 ON a2.id = ta2.artist_id
		                  WHERE ta2.track_id = t.id), ''),
		        COALESCE((SELECT al.title FROM track_albums tal JOIN albums al ON al.id = tal.album_id WHERE tal.track_id = t.id ORDER BY tal.disc_number, tal.track_number LIMIT 1), '')
		 FROM tracks t WHERE t.id = $1`, trackID).Scan(&info.FilePath, &info.FileFormat, &info.BitRate, &info.Title, &info.Artist, &info.Album); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("query version label for %s: %w", trackID, err)
	}
	return ExtractVersionLabelFromInfo(info), nil
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
	// leaking their trailing parts into the version label. Title is always
	// excluded here (unlike the keyword pass in ExtractVersionLabel, which
	// deliberately keeps a filename-derived title searchable): this fallback
	// must not join a tag-less file's whole stem into a fake label.
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
	// The fallback joins every surviving token (deduplicated) as the label.
	// Version keywords are NOT matched here: ExtractVersionLabel already ran
	// the identical token scan against a smaller blacklist before calling
	// this fallback, so any version keyword that survives would have been
	// caught and returned upstream — matching it again would be dead code.
	var kept []string
	seen := make(map[string]bool)
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
		// Collapse duplicate tokens ("Song / Song") instead of repeating them.
		if seen[lower] {
			continue
		}
		seen[lower] = true
		kept = append(kept, titleCase(tok))
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, ", ")
}

// versionKeyword reports whether s is one of the version markers used for
// version labels.
func versionKeyword(s string) bool {
	switch s {
	case "live", "acoustic", "remaster", "remastered", "deluxe", "bonus",
		"demo", "instrumental", "edit", "extended", "mix", "radio", "karaoke",
		"unplugged", "anniversary", "orchestral", "piano", "reprise":
		return true
	}
	return false
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

type VersionUpdate struct {
	ID      string
	Version int
	Label   string
}

type VersionGroupInsert struct {
	Source     string
	ExternalID string
	LibraryID  string
	TrackID    string
}

type VersionGroupDelete struct {
	Source     string
	ExternalID string
	LibraryID  string
}

const batchSize = 500

type batchDB interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func BatchUpdateVersionLabels(ctx context.Context, db batchDB, updates []VersionUpdate) error {
	for i := 0; i < len(updates); i += batchSize {
		end := i + batchSize
		if end > len(updates) {
			end = len(updates)
		}
		batch := updates[i:end]
		var sb strings.Builder
		sb.WriteString("UPDATE tracks SET version = v.version, version_label = v.label FROM (VALUES ")
		args := make([]interface{}, 0, len(batch)*3)
		for j, u := range batch {
			if j > 0 {
				sb.WriteString(", ")
			}
			n := j * 3
			fmt.Fprintf(&sb, "($%d, $%d, $%d)", n+1, n+2, n+3)
			args = append(args, u.ID, u.Version, u.Label)
		}
		sb.WriteString(") AS v(id, version, label) WHERE tracks.id = v.id")
		if _, err := db.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func BatchInsertVersionGroups(ctx context.Context, db batchDB, inserts []VersionGroupInsert) error {
	for i := 0; i < len(inserts); i += batchSize {
		end := i + batchSize
		if end > len(inserts) {
			end = len(inserts)
		}
		batch := inserts[i:end]
		var sb strings.Builder
		sb.WriteString("INSERT INTO track_version_groups (metadata_source, external_id, library_id, track_id) VALUES ")
		args := make([]interface{}, 0, len(batch)*4)
		for j, ins := range batch {
			if j > 0 {
				sb.WriteString(", ")
			}
			n := j * 4
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d)", n+1, n+2, n+3, n+4)
			args = append(args, ins.Source, ins.ExternalID, ins.LibraryID, ins.TrackID)
		}
		sb.WriteString(" ON CONFLICT DO NOTHING")
		if _, err := db.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}
