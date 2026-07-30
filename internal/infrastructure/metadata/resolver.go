package metadata

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"unicode"
)

type ArtistResult struct {
	Name    string
	MBID    string
	Country string
}

type EnrichmentResult struct {
	ArtistMBID    string
	AlbumMBID     string
	TrackMBID     string
	ArtistCountry string
	AlbumCountry  string
	Artists       []ArtistResult // all matched artists from MB recording
	Genre         string
	Year          int
	Biography     string
	CoverArtURL   string
	Title         string  // MB-corrected title (if ffprobe was wrong)
	Artist        string  // MB-corrected artist (first performer)
	Album         string  // MB-corrected album
}

type Resolver struct {
	mb *MBClient
}

func NewResolver(cfg MBConfig) *Resolver {
	return &Resolver{
		mb: NewMBClient(cfg),
	}
}

// Enrich performs the recognition chain.
func (r *Resolver) Enrich(ctx context.Context, meta *AudioMeta) (*EnrichmentResult, error) {
	title := meta.Title
	artist := meta.Artist
	if artist == "" {
		artist = meta.AlbumArtist
	}
	artists := meta.Artists
	if len(artists) == 0 && artist != "" && artist != "Unknown Artist" {
		artists = []string{artist}
	}
	album := meta.Album

	if title == "" {
		return nil, nil
	}

	if looksGarbled(title) || looksGarbled(artist) || looksGarbled(album) {
		log.Printf("[mb] skip garbled input: title=%q artist=%q album=%q", title, artist, album)
		return nil, nil
	}

	recordings := r.searchRecordings(title, artists, album)
	if len(recordings) == 0 {
		return nil, nil
	}

	log.Printf("[mb] search returned %d results for %q", len(recordings), title)

	var recording *MBRecording
	if meta.TitleFromFilename {
		recording = r.scoreMatch(title, recordings)
	} else {
		recording = r.exactMatch(title, artist, album, recordings)
	}
	// Fallback: pick best by artist + title substring match
	if recording == nil && len(artists) > 0 {
		recording = r.bestArtistMatch(title, artists, recordings)
	}

	if recording == nil {
		log.Printf("[mb] no match found")
		return nil, nil
	}

	log.Printf("[mb] recording matched: %q (MBID=%s)", recording.Title, recording.ID)

	result := &EnrichmentResult{
		TrackMBID: recording.ID,
	}

	result.Title = TrimParenSuffix(recording.Title)

	if len(recording.Artists) > 0 {
		// Collect all artists from the recording
		for _, ref := range recording.Artists {
			ar := ArtistResult{Name: TrimParenSuffix(ref.Name)}
			if ref.Artist != nil {
				ar.MBID = ref.Artist.ID
				ar.Country = ref.Artist.Country
			}
			if ref.Name != "" {
				result.Artists = append(result.Artists, ar)
			}
		}
		if recording.Artists[0].Artist != nil {
			result.ArtistMBID = recording.Artists[0].Artist.ID
			result.ArtistCountry = recording.Artists[0].Artist.Country
		}
		if meta.Artist == "" {
			result.Artist = recording.Artists[0].Name
		}
	}

	var relIdx = -1
	for i, rel := range recording.Releases {
		if rel.Status != "" && !strings.EqualFold(rel.Status, "official") {
			continue
		}
		relIdx = i
		result.AlbumMBID = rel.ID
		result.Album = rel.Title
		if len(rel.Date) >= 4 {
			fmt.Sscanf(rel.Date[:4], "%d", &result.Year)
		}
		break
	}
	// Pick year from any release if not found on official one
	if result.Year == 0 {
		for _, rel := range recording.Releases {
			if len(rel.Date) >= 4 {
				fmt.Sscanf(rel.Date[:4], "%d", &result.Year)
				break
			}
		}
	}
	// Pick genre from first official release
	if relIdx >= 0 {
		rel := recording.Releases[relIdx]
		full, err := r.mb.LookupRelease(rel.ID)
		if err == nil {
			if g := GenreFromTags(full.Tags); g != "" {
				result.Genre = g
			}
			result.AlbumCountry = full.Country
		}
		result.CoverArtURL = fmt.Sprintf("https://coverartarchive.org/release/%s/front", rel.ID)
	}

	if result.ArtistMBID != "" {
		full, err := r.mb.LookupArtist(result.ArtistMBID)
		if err == nil {
			if result.ArtistCountry == "" && full.Country != "" {
				result.ArtistCountry = full.Country
			}
			if len(full.Tags) > 0 {
				if result.Genre == "" {
					if g := GenreFromTags(full.Tags); g != "" {
						result.Genre = g
					}
				}
			}
		}
	}

	return result, nil
}

func normalizeForMatch(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	repl := []string{"&", "and", ",", ".", "!", "?", "-", "\u2013", "  "}
	for _, r := range repl {
		s = strings.ReplaceAll(s, r, " ")
	}
	return strings.TrimSpace(s)
}

func splitByDash(title string) []string {
	parts := strings.FieldsFunc(title, func(r rune) bool {
		return r == '-' || r == '\u2013' || r == ',' || r == '(' || r == ')'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (r *Resolver) scoreMatch(title string, recordings []MBRecording) *MBRecording {
	parts := splitByDash(title)
	if len(parts) == 0 {
		return nil
	}

	for i := range recordings {
		if matchPartsScore(parts, &recordings[i]) >= 2 {
			return &recordings[i]
		}
	}
	return nil
}

func matchPartsScore(parts []string, rec *MBRecording) int {
	mbTitle := normalizeForMatch(TrimParenSuffix(rec.Title))
	mbArtist := ""
	if len(rec.Artists) > 0 {
		mbArtist = normalizeForMatch(TrimParenSuffix(rec.Artists[0].Name))
	}
	mbAlbum := ""
	if len(rec.Releases) > 0 {
		mbAlbum = normalizeForMatch(TrimParenSuffix(rec.Releases[0].Title))
	}

	hits := 0
	for _, p := range parts {
		n := normalizeForMatch(p)
		if n == "" {
			continue
		}
		if mbTitle != "" && n == mbTitle {
			hits++
			continue
		}
		if mbArtist != "" && n == mbArtist {
			hits++
			continue
		}
		if mbAlbum != "" && n == mbAlbum {
			hits++
			continue
		}
	}
	return hits
}

func (r *Resolver) exactMatch(title, artist, album string, recordings []MBRecording) *MBRecording {
	for i := range recordings {
		if fieldsMatch(title, artist, album, &recordings[i]) {
			return &recordings[i]
		}
	}
	return nil
}

func splitArtists(s string) []string {
	var parts []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ',' || r == '、' || r == '&'
	}) {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, normalizeForMatch(p))
		}
	}
	return parts
}

func fieldsMatch(title, artist, album string, rec *MBRecording) bool {
	nTitle := normalizeForMatch(TrimParenSuffix(title))
	mbTitle := normalizeForMatch(TrimParenSuffix(rec.Title))
	if nTitle != mbTitle {
		return false
	}

	if artist != "" && artist != "Unknown Artist" {
		queryArtists := splitArtists(artist)
		if len(rec.Artists) == 0 {
			return false
		}
		for _, qa := range queryArtists {
			matched := false
			for _, ra := range rec.Artists {
				if normalizeForMatch(TrimParenSuffix(ra.Name)) == qa {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}

	if album != "" && album != "Unknown Album" {
		found := false
		for _, rel := range rec.Releases {
			if titlesMatch(album, rel.Title) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// titlesMatch checks whether two track titles should be considered a match.
// For strings with CJK characters, minimum meaningful length is 2; for others, 4.
// When both are long enough, does substring containment check.
// Otherwise, splits the longer string by separators and checks equality with the shorter.
func titlesMatch(a, b string) bool {
	na := normalizeForMatch(a)
	nb := normalizeForMatch(b)

	minLen := func(s string) int {
		for _, r := range s {
			if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
				unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
				return 2
			}
		}
		return 4
	}

	if len(na) >= minLen(na) && len(nb) >= minLen(nb) {
		return strings.Contains(na, nb) || strings.Contains(nb, na)
	}

	longer, shorter := na, nb
	if len(longer) < len(shorter) {
		longer, shorter = shorter, longer
	}
	for _, tok := range strings.FieldsFunc(longer, func(r rune) bool {
		return r == ' ' || r == '-' || r == '\u2013' || r == '_' || r == '/' ||
			r == '.' || r == ',' || r == '(' || r == ')' || r == '[' || r == ']'
	}) {
		if tok == shorter {
			return true
		}
	}
	return false
}

// bestArtistMatch scores recordings by how many of the query artists appear in the recording.
// The recording title must pass titlesMatch against the query title.
func (r *Resolver) bestArtistMatch(queryTitle string, artists []string, recordings []MBRecording) *MBRecording {
	if len(artists) == 0 {
		return nil
	}
	artistSet := make(map[string]bool)
	for _, a := range artists {
		artistSet[normalizeForMatch(a)] = true
	}

	var best *MBRecording
	bestScore := 0
	for i := range recordings {
		if !titlesMatch(queryTitle, TrimParenSuffix(recordings[i].Title)) {
			continue
		}
		score := 0
		for _, ra := range recordings[i].Artists {
			n := normalizeForMatch(ra.Name)
			if artistSet[n] {
				score++
			} else {
				for qa := range artistSet {
					if strings.Contains(n, qa) || strings.Contains(qa, n) {
						score++
						break
					}
				}
			}
		}
		if score > 0 && score > bestScore {
			bestScore = score
			best = &recordings[i]
		}
	}
	return best
}

func (r *Resolver) searchRecordings(title string, artists []string, album string) []MBRecording {
	recs, err := r.mb.SearchRecordings(title, artists, album)
	if err != nil {
		return nil
	}
	return recs
}

// IdentifyTrack does a full MB lookup by recording MBID for manual identification.
func (r *Resolver) IdentifyTrack(ctx context.Context, mbid string) (*EnrichmentResult, error) {
	var rec MBRecording
	q := url.Values{}
	q.Set("inc", "artists+releases+tags")
	if err := r.mb.get("/recording/"+mbid, q, &rec); err != nil {
		return nil, err
	}

	result := &EnrichmentResult{
		TrackMBID: rec.ID,
		Title:     TrimParenSuffix(rec.Title),
	}

	if len(rec.Artists) > 0 {
		for _, ref := range rec.Artists {
			ar := ArtistResult{Name: TrimParenSuffix(ref.Name)}
			if ref.Artist != nil {
				ar.MBID = ref.Artist.ID
				ar.Country = ref.Artist.Country
			}
			if ref.Name != "" {
				result.Artists = append(result.Artists, ar)
			}
		}
		if rec.Artists[0].Artist != nil {
			result.ArtistMBID = rec.Artists[0].Artist.ID
			result.ArtistCountry = rec.Artists[0].Artist.Country
		}
		result.Artist = rec.Artists[0].Name
	}

	for _, rel := range rec.Releases {
		if rel.Status != "" && !strings.EqualFold(rel.Status, "official") {
			continue
		}
		result.AlbumMBID = rel.ID
		result.Album = TrimParenSuffix(rel.Title)
		if len(rel.Date) >= 4 {
			fmt.Sscanf(rel.Date[:4], "%d", &result.Year)
		}

		full, err := r.mb.LookupRelease(rel.ID)
		if err == nil {
			if g := GenreFromTags(full.Tags); g != "" {
				result.Genre = g
			}
			result.AlbumCountry = full.Country
		}

		result.CoverArtURL = fmt.Sprintf("https://coverartarchive.org/release/%s/front", rel.ID)
		break
	}

	if result.ArtistMBID != "" {
		full, err := r.mb.LookupArtist(result.ArtistMBID)
		if err == nil {
			if result.ArtistCountry == "" && full.Country != "" {
				result.ArtistCountry = full.Country
			}
			if len(full.Tags) > 0 {
				if result.Genre == "" {
					if g := GenreFromTags(full.Tags); g != "" {
						result.Genre = g
					}
				}
			}
		}
	}

	return result, nil
}

func (r *Resolver) FetchCoverArt(ctx context.Context, mbid string) ([]byte, string, error) {
	return r.mb.FetchCoverArt(mbid)
}

// looksGarbled detects if a string contains replacement characters
// or excessive non-ASCII bytes that indicate encoding issues.
func looksGarbled(s string) bool {
	replacement := 0
	for _, r := range s {
		if r == 0xFFFD {
			replacement++
		}
	}
	if replacement > 0 {
		return true
	}
	return false
}

func (r *Resolver) Close() {
	r.mb.Close()
}

// TrimParenSuffix removes parenthetical descriptions from MB data.
// "奢香夫人 (展现一代彝族巾帼英雄奢香夫人的异域风情歌曲)" → "奢香夫人"
// "自由飞翔 （album version）" → "自由飞翔"
func TrimParenSuffix(s string) string {
	for _, sep := range []string{" (", "（"} {
		if idx := strings.Index(s, sep); idx > 0 {
			s = s[:idx]
		}
	}
	return s
}
