package metadata

import (
	"context"
	"fmt"
	"log"
	"strings"
)

type EnrichmentResult struct {
	ArtistMBID   string
	AlbumMBID    string
	TrackMBID    string
	ArtistCountry string
	AlbumCountry  string
	Genre        string
	Year         int
	Biography    string
	CoverArtURL  string
	Title        string  // MB-corrected title (if ffprobe was wrong)
	Artist       string  // MB-corrected artist
	Album        string  // MB-corrected album
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
	album := meta.Album

	if title == "" {
		return nil, nil
	}

	if looksGarbled(title) || looksGarbled(artist) || looksGarbled(album) {
		log.Printf("[mb] skip garbled input: title=%q artist=%q album=%q", title, artist, album)
		return nil, nil
	}

	recordings := r.searchRecordings(title, artist, album)
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

	if recording == nil {
		log.Printf("[mb] no match found")
		return nil, nil
	}

	log.Printf("[mb] recording matched: %q (MBID=%s)", recording.Title, recording.ID)

	result := &EnrichmentResult{
		TrackMBID: recording.ID,
	}

	if meta.TitleFromFilename {
		result.Title = cleanTitle(recording.Title)
	}

	if len(recording.Artists) > 0 {
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
		if rel.Status != "" && rel.Status != "official" {
			continue
		}
		relIdx = i
		result.AlbumMBID = rel.ID
		if meta.Album == "" {
			result.Album = rel.Title
		}
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
		return &recordings[0]
	}

	for i := range recordings {
		if matchPartsScore(parts, &recordings[i]) >= 2 {
			return &recordings[i]
		}
	}
	return nil
}

func matchPartsScore(parts []string, rec *MBRecording) int {
	mbTitle := normalizeForMatch(rec.Title)
	mbArtist := ""
	if len(rec.Artists) > 0 {
		mbArtist = normalizeForMatch(rec.Artists[0].Name)
	}
	mbAlbum := ""
	if len(rec.Releases) > 0 {
		mbAlbum = normalizeForMatch(rec.Releases[0].Title)
	}

	hits := 0
	for _, p := range parts {
		n := normalizeForMatch(p)
		if n == "" {
			continue
		}
		if mbTitle != "" && (strings.Contains(mbTitle, n) || strings.Contains(n, mbTitle)) {
			hits++
			continue
		}
		if mbArtist != "" && (strings.Contains(mbArtist, n) || strings.Contains(n, mbArtist)) {
			hits++
			continue
		}
		if mbAlbum != "" && (strings.Contains(mbAlbum, n) || strings.Contains(n, mbAlbum)) {
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

func fieldsMatch(title, artist, album string, rec *MBRecording) bool {
	nTitle := normalizeForMatch(title)
	mbTitle := normalizeForMatch(rec.Title)
	if !strings.Contains(mbTitle, nTitle) && !strings.Contains(nTitle, mbTitle) {
		return false
	}

	if artist != "" && artist != "Unknown Artist" {
		nArtist := normalizeForMatch(artist)
		mbArtist := ""
		if len(rec.Artists) > 0 {
			mbArtist = normalizeForMatch(rec.Artists[0].Name)
		}
		if mbArtist == "" || (!strings.Contains(mbArtist, nArtist) && !strings.Contains(nArtist, mbArtist)) {
			return false
		}
	}

	if album != "" && album != "Unknown Album" {
		nAlbum := normalizeForMatch(album)
		found := false
		for _, rel := range rec.Releases {
			mbAlbum := normalizeForMatch(rel.Title)
			if strings.Contains(mbAlbum, nAlbum) || strings.Contains(nAlbum, mbAlbum) {
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

func (r *Resolver) searchRecordings(title, artist, album string) []MBRecording {
	recs, err := r.mb.SearchRecordings(title, artist, album)
	if err != nil {
		return nil
	}
	return recs
}

// IdentifyTrack does a full MB lookup by recording MBID for manual identification.
func (r *Resolver) IdentifyTrack(ctx context.Context, mbid string) (*EnrichmentResult, error) {
	var rec MBRecording
	q := fmt.Sprintf("/recording/%s?inc=artists+releases+tags&fmt=json", mbid)
	if err := r.mb.get(q, nil, &rec); err != nil {
		return nil, err
	}

	result := &EnrichmentResult{
		TrackMBID: rec.ID,
		Title:     rec.Title,
	}

	if len(rec.Artists) > 0 {
		if rec.Artists[0].Artist != nil {
			result.ArtistMBID = rec.Artists[0].Artist.ID
			result.ArtistCountry = rec.Artists[0].Artist.Country
		}
		result.Artist = rec.Artists[0].Name
	}

	for _, rel := range rec.Releases {
		if rel.Status != "" && rel.Status != "official" {
			continue
		}
		result.AlbumMBID = rel.ID
		result.Album = rel.Title
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

// cleanTitle removes parenthetical descriptions from MB titles.
// "奢香夫人 (展现一代彝族巾帼英雄奢香夫人的异域风情歌曲)" → "奢香夫人"
func cleanTitle(title string) string {
	if idx := strings.Index(title, " ("); idx > 0 {
		title = title[:idx]
	}
	return title
}
