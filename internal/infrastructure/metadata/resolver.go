package metadata

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

// andWordRe matches the word "and" (lowercased input) so only the standalone
// conjunction is dropped, never the substring inside another word. Go regexp
// \b is ASCII-only, so explicit Unicode letter/number boundaries are used:
// without them "and" between CJK characters (e.g. "梦想and现实") would be
// treated as standalone despite being inside a mixed-script word. Underscore
// is excluded from the boundary class to match \b's word semantics (a word
// char), so "And_Justice_For_All" keeps its "and".
var andWordRe = regexp.MustCompile(`(^|[^\pL\pN_])and([^\pL\pN_]|$)`)

type ArtistResult struct {
	Name       string
	ExternalID string
	Country    string
}

type EnrichmentResult struct {
	Source           string // metadata source that produced this result ("musicbrainz"/"netease")
	ArtistExternalID string
	AlbumExternalID  string
	TrackExternalID  string
	ArtistCountry    string
	AlbumCountry     string
	Artists          []ArtistResult // all matched artists from MB recording
	Genre            string
	Year             int
	Biography        string
	Lyrics           string // network lyrics (NetEase), stored at network priority
	Title            string // MB-corrected title (if ffprobe was wrong)
	Artist           string // MB-corrected artist (first performer)
	Album            string // MB-corrected album
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
		logger.Debug("[mb] skip garbled input: title=%q artist=%q album=%q", title, artist, album)
		return nil, nil
	}

	recordings := r.searchRecordings(ctx, title, artists, album)
	if len(recordings) == 0 {
		return nil, nil
	}

	logger.Debug("[mb] search returned %d results for %q", len(recordings), title)

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
		logger.Debug("[mb] no match found for %q", title)
		return nil, nil
	}

	logger.Debug("[mb] recording matched: %q (MBID=%s)", recording.Title, recording.ID)

	result := &EnrichmentResult{
		TrackExternalID: recording.ID,
		Source:          SourceMusicBrainz,
	}

	result.Title = TrimParenSuffix(recording.Title)

	if len(recording.Artists) > 0 {
		// Collect all artists from the recording
		for _, ref := range recording.Artists {
			ar := ArtistResult{Name: TrimParenSuffix(ref.Name)}
			if ref.Artist != nil {
				ar.ExternalID = ref.Artist.ID
				ar.Country = ref.Artist.Country
			}
			if ref.Name != "" {
				result.Artists = append(result.Artists, ar)
			}
		}
		if recording.Artists[0].Artist != nil {
			result.ArtistExternalID = recording.Artists[0].Artist.ID
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
		result.AlbumExternalID = rel.ID
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
		full, err := r.mb.LookupRelease(ctx, rel.ID)
		if err == nil {
			if g := GenreFromTags(full.Tags); g != "" {
				result.Genre = g
			}
			result.AlbumCountry = full.Country
		}
	}

	// Enrich the main artist (country + genre fallback) and its country
	// entry; fillArtistCountries below then handles only the secondary
	// artists.
	r.enrichMainArtist(ctx, result)

	// Country for the remaining (secondary) artists: recording responses only
	// carry the artist id, and only the main artist is enriched above. A
	// secondary artist left without a country makes the scanner's metaComplete
	// (every MB artist needs one) fail, so missing scans keep re-identifying.
	r.fillArtistCountries(ctx, result)

	return result, nil
}

// lookupArtist resolves an artist detail by MBID, serving repeat lookups from
// the scan-scoped ArtistDetailCache when present (a nil cache in the context
// means no caching). Failures are not cached so a transient error is retried
// on the next occurrence.
func (r *Resolver) lookupArtist(ctx context.Context, mbid string) (*MBArtistFull, error) {
	if cache := artistDetailCacheFrom(ctx); cache != nil {
		if full := cache.Get(mbid); full != nil {
			return full, nil
		}
	}
	full, err := r.mb.LookupArtist(ctx, mbid)
	if err != nil {
		return nil, err
	}
	if cache := artistDetailCacheFrom(ctx); cache != nil {
		cache.Set(mbid, full)
	}
	return full, nil
}

// enrichMainArtist fills the main artist's country, the result-level
// ArtistCountry and the genre fallback with a single artist lookup. Shared by
// Enrich and IdentifyTrack so the two main-artist blocks cannot drift. The
// artist is identified by its external id, not by index — result.Artists only
// collects credits with a non-empty name. A lookup failure is logged (and not
// cached, so it is retried on the next occurrence) rather than silently
// skipped.
func (r *Resolver) enrichMainArtist(ctx context.Context, result *EnrichmentResult) {
	if result.ArtistExternalID == "" {
		return
	}
	full, err := r.lookupArtist(ctx, result.ArtistExternalID)
	if err != nil {
		logger.Error("[mb] artist lookup failed for %s: %v", result.ArtistExternalID, err)
		return
	}
	if full == nil {
		return
	}
	if result.ArtistCountry == "" && full.Country != "" {
		result.ArtistCountry = full.Country
	}
	for i := range result.Artists {
		ar := &result.Artists[i]
		if ar.ExternalID == result.ArtistExternalID && ar.Country == "" && full.Country != "" {
			ar.Country = full.Country
		}
	}
	if result.Genre == "" {
		if g := GenreFromTags(full.Tags); g != "" {
			result.Genre = g
		}
	}
}

// fillArtistCountries looks up country for every secondary artist in
// result.Artists still missing one, via per-artist MusicBrainz lookups
// (rate-limited by the client, memoized per scan via the context cache). The
// main artist is enriched separately by enrichMainArtist (called before this
// in Enrich/IdentifyTrack); skipping it here avoids a second lookup.
func (r *Resolver) fillArtistCountries(ctx context.Context, result *EnrichmentResult) {
	for i := range result.Artists {
		ar := &result.Artists[i]
		if ar.Country != "" || ar.ExternalID == "" || ar.ExternalID == result.ArtistExternalID {
			continue
		}
		full, err := r.lookupArtist(ctx, ar.ExternalID)
		if err != nil {
			// A failure is not cached, so it is retried on the next
			// occurrence; log it so a flaky upstream is distinguishable from
			// an artist that genuinely has no country.
			logger.Error("[mb] artist lookup failed for %s: %v", ar.ExternalID, err)
			continue
		}
		if full == nil || full.Country == "" {
			continue
		}
		ar.Country = full.Country
	}
}

func normalizeForMatch(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "&", " ")
	// "and" only as a whole word: replacing the substring inside "Command"
	// or "England" would silently mangle titles into false-equality. The
	// captured boundary characters are kept (replaced by a space). ReplaceAll
	// is non-overlapping and consumes the trailing boundary, so two "and"s
	// separated by one separator ("x and and y") would leave the second one;
	// loop until stable (each pass strictly removes an "and", so it ends).
	for {
		next := andWordRe.ReplaceAllString(s, "$1 $2")
		if next == s {
			break
		}
		s = next
	}
	for _, r := range []string{",", ".", "!", "?", "-", "\u2013"} {
		s = strings.ReplaceAll(s, r, " ")
	}
	// Collapse every whitespace run left by the replacements.
	return strings.Join(strings.Fields(s), " ")
}

// NormalizeForMatch is the exported form of normalizeForMatch for the
// scanner's duplicate-merge pass.
func NormalizeForMatch(s string) string { return normalizeForMatch(s) }

// SplitArtists is the exported form of splitRawArtists for the scanner's
// duplicate-merge pass.
func SplitArtists(s string) []string { return splitRawArtists(s) }

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

func (r *Resolver) searchRecordings(ctx context.Context, title string, artists []string, album string) []MBRecording {
	recs, err := r.mb.SearchRecordings(ctx, title, artists, album)
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
	if err := r.mb.get(ctx, "/recording/"+mbid, q, &rec); err != nil {
		return nil, err
	}

	result := &EnrichmentResult{
		TrackExternalID: rec.ID,
		Source:          SourceMusicBrainz,
		Title:           TrimParenSuffix(rec.Title),
	}

	if len(rec.Artists) > 0 {
		for _, ref := range rec.Artists {
			ar := ArtistResult{Name: TrimParenSuffix(ref.Name)}
			if ref.Artist != nil {
				ar.ExternalID = ref.Artist.ID
				ar.Country = ref.Artist.Country
			}
			if ref.Name != "" {
				result.Artists = append(result.Artists, ar)
			}
		}
		if rec.Artists[0].Artist != nil {
			result.ArtistExternalID = rec.Artists[0].Artist.ID
			result.ArtistCountry = rec.Artists[0].Artist.Country
		}
		result.Artist = rec.Artists[0].Name
	}

	for _, rel := range rec.Releases {
		if rel.Status != "" && !strings.EqualFold(rel.Status, "official") {
			continue
		}
		result.AlbumExternalID = rel.ID
		result.Album = TrimParenSuffix(rel.Title)
		if len(rel.Date) >= 4 {
			fmt.Sscanf(rel.Date[:4], "%d", &result.Year)
		}

		full, err := r.mb.LookupRelease(ctx, rel.ID)
		if err == nil {
			if g := GenreFromTags(full.Tags); g != "" {
				result.Genre = g
			}
			result.AlbumCountry = full.Country
		}

		break
	}

	r.enrichMainArtist(ctx, result)

	r.fillArtistCountries(ctx, result)

	return result, nil
}

func (r *Resolver) FetchCoverArt(ctx context.Context, mbid string) ([]byte, string, error) {
	return r.mb.FetchCoverArt(ctx, mbid)
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
