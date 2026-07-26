package metadata

import (
	"context"
	"fmt"
	"log"
	"strings"
)

type EnrichmentResult struct {
	ArtistMBID  string
	AlbumMBID   string
	TrackMBID   string
	Genre       string
	Year        int
	Biography   string
	CoverArtURL string
}

type Resolver struct {
	mb *MBClient
}

func NewResolver() *Resolver {
	return &Resolver{
		mb: NewMBClient(),
	}
}

func (r *Resolver) Enrich(ctx context.Context, meta *AudioMeta) (*EnrichmentResult, error) {
	if meta.Artist == "" && meta.Album == "" {
		return nil, nil
	}

	result := &EnrichmentResult{}
	artistName := meta.Artist
	if artistName == "" {
		artistName = meta.AlbumArtist
	}

	// Step 1: Search artist on MusicBrainz
	if artistName != "" {
		artist, err := r.mb.SearchArtist(artistName)
		if err != nil {
			log.Printf("[mb] artist search failed for %q: %v", artistName, err)
		} else {
			result.ArtistMBID = artist.ID
			log.Printf("[mb] found artist %q → %s", artist.Name, artist.ID)

			full, err := r.mb.LookupArtist(artist.ID)
			if err == nil && len(full.Tags) > 0 {
				if g := GenreFromTags(full.Tags); g != "" {
					result.Genre = g
				}
			}
		}
	}

	// Step 2: Search album on MusicBrainz
	albumTitle := meta.Album
	if albumTitle != "" && albumTitle != "Unknown Album" {
		release, err := r.mb.SearchRelease(albumTitle, artistName)
		if err != nil {
			log.Printf("[mb] release search failed for %q - %q: %v", artistName, albumTitle, err)
		} else {
			result.AlbumMBID = release.ID
			log.Printf("[mb] found release %q → %s", release.Title, release.ID)

			full, err := r.mb.LookupRelease(release.ID)
			if err == nil {
				if result.Year == 0 && len(full.Date) >= 4 {
					fmt.Sscanf(full.Date[:4], "%d", &result.Year)
				}
				if g := GenreFromTags(full.Tags); g != "" {
					result.Genre = g
				}

				for _, m := range full.Media {
					for _, t := range m.Tracks {
						if meta.Title != "" && strings.EqualFold(t.Title, meta.Title) {
							result.TrackMBID = t.ID
							break
						}
					}
					if result.TrackMBID != "" {
						break
					}
				}
			}

			result.CoverArtURL = fmt.Sprintf("https://coverartarchive.org/release/%s/front", release.ID)
		}
	}

	// Step 3: If year not found by MB, try from meta
	if result.Year == 0 && meta.Year != 0 {
		result.Year = meta.Year
	}

	return result, nil
}

func (r *Resolver) FetchCoverArt(ctx context.Context, mbid string) ([]byte, string, error) {
	return r.mb.FetchCoverArt(mbid)
}

func (r *Resolver) Close() {
	r.mb.Close()
}



