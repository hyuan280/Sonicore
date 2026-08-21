package scanner

import (
	"context"
	"strings"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

func matchArtistEnrichment(name string, enrichment *metadata.EnrichmentResult) *metadata.EnrichmentResult {
	if enrichment == nil || len(enrichment.Artists) == 0 {
		return nil
	}
	n := strings.ToLower(strings.TrimSpace(name))
	for _, ar := range enrichment.Artists {
		if strings.ToLower(strings.TrimSpace(ar.Name)) == n {
			return &metadata.EnrichmentResult{
				Source:           enrichment.Source,
				ArtistExternalID: ar.ExternalID,
				ArtistCountry:    ar.Country,
				Artist:           ar.Name,
			}
		}
	}
	return nil
}

// ApplyEnrichment applies enrichment results to an existing track. When
// overwrite is true, all fields are replaced; when false, only empty/unknown
// fields are filled.
func ApplyEnrichment(ctx context.Context, track *domain.Track, meta *metadata.AudioMeta, enrichment *metadata.EnrichmentResult, overwrite bool, trackRepo *repository.TrackRepo, artistRepo *repository.ArtistRepo, albumRepo *repository.AlbumRepo, er *metadata.EntityResolver) (changed bool) {
	// A file MBID only makes sense as the primary id under the MusicBrainz
	// namespace. When the track is already keyed by another source (and this
	// scan did not re-identify it), adopting the MBID would mix namespaces
	// into (netease, MBID); the scanner records such MBIDs as a musicbrainz
	// alias instead.
	if meta.MBID != "" && (track.ExternalID == "" || overwrite) &&
		(track.MetadataSource == "" || track.MetadataSource == metadata.SourceMusicBrainz) {
		track.SetExternalID(meta.MBID)
		changed = true
	}
	if enrichment != nil {
		// Record the producing source so later scans and the version
		// grouping treat the external IDs correctly. An overwrite scan
		// abandons the existing metadata and re-fills it, so the source
		// follows the recognition result — including MusicBrainz, which
		// lets NetEase-identified tracks rejoin the MusicBrainz version
		// group once MB matches them. In missing mode only empty or the
		// legacy default source is written.
		if enrichment.Source != "" && track.MetadataSource != enrichment.Source {
			if overwrite || track.MetadataSource == "" || track.MetadataSource == metadata.SourceMusicBrainz {
				// Source switch: the current external id belongs to the old
				// namespace. Record it as an alias under the OLD source BEFORE
				// switching, so it survives the new primary id replacing the
				// primary slot (SetExternalID keys the alias table on the new
				// MetadataSource). The external_ids map is then persisted with
				// the same row update, keeping (source, external_id) consistent
				// and the old id reachable via aliases. An empty source means
				// the id is file-tag data (a MusicBrainz MBID) — preserve it
				// under musicbrainz instead of a garbage empty-key entry.
				if track.ExternalID != "" {
					if track.ExternalIDs == nil {
						track.ExternalIDs = map[string]string{}
					}
					src := track.MetadataSource
					if src == "" {
						src = metadata.SourceMusicBrainz
					}
					track.ExternalIDs[src] = track.ExternalID
				}
				oldSrc := track.MetadataSource
				if oldSrc == "" {
					oldSrc = metadata.SourceMusicBrainz
				}
				track.MetadataSource = enrichment.Source
				if enrichment.TrackExternalID != "" {
					track.SetExternalID(enrichment.TrackExternalID)
				} else if track.ExternalID != "" && oldSrc != enrichment.Source {
					// A real namespace change with no new id: clear the old
					// primary (already preserved as an alias above). When the
					// old source normalizes to the new one (legacy empty →
					// musicbrainz), the id already lives in the target
					// namespace, so clearing it would delete the primary AND
					// its just-written same-key alias together.
					track.SetExternalID("")
				}
				changed = true
			}
		}
		if enrichment.Title != "" && (track.Title == "" || meta.TitleFromFilename || overwrite) {
			track.Title = enrichment.Title
			changed = true
		}
		if enrichment.TrackExternalID != "" && (track.ExternalID == "" || overwrite) {
			track.SetExternalID(enrichment.TrackExternalID)
			changed = true
		}
		if len(enrichment.Artists) > 0 {
			trackArtists, err := trackRepo.LoadTrackArtists(ctx, track.ID)
			if err != nil {
				logger.Info("[scan] load track artists for enrichment %s: %v", track.ID, err)
			} else {
				allUnknown := true
				lookupFailed := false
				for _, ta := range trackArtists {
					artist, err := artistRepo.FindByID(ctx, ta.ArtistID)
					if err != nil {
						// Do not treat a lookup failure as "all
						// unknown": replacing the artists below would
						// DELETE the real associations (ReplaceTrackArtists
						// deletes then re-inserts) and lose data.
						logger.Info("[scan] artist lookup for enrichment %s: %v", ta.ArtistID, err)
						lookupFailed = true
						continue
					}
					if artist.Name != "" && artist.Name != "Unknown Artist" {
						allUnknown = false
					}
					match := matchArtistEnrichment(artist.Name, enrichment)
					updated := false
					if match != nil && match.ArtistExternalID != "" && (artist.ExternalID == "" || overwrite) {
						// The id replacement changes the artist's
						// namespace too: keep MetadataSource in step
						// (including MusicBrainz) or the artist would
						// end up (netease, MBID) like the track-level
						// guard prevents above. The old id is kept as an
						// alias under its old source so identity lookups
						// by the previous (source, external_id) still
						// resolve to this artist instead of creating a
						// duplicate.
						if enrichment.Source != "" && artist.MetadataSource != enrichment.Source {
							if artist.ExternalID != "" {
								if artist.ExternalIDs == nil {
									artist.ExternalIDs = map[string]string{}
								}
								src := artist.MetadataSource
								if src == "" {
									src = metadata.SourceMusicBrainz
								}
								artist.ExternalIDs[src] = artist.ExternalID
							}
							artist.MetadataSource = enrichment.Source
						}
						artist.ExternalID = match.ArtistExternalID
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
						if err := artistRepo.Update(ctx, artist); err != nil {
							logger.Error("[scan] artist enrichment update error for %s: %v", artist.ID, err)
						} else {
							changed = true
						}
					}
				}
				if allUnknown && !lookupFailed {
					var newArtists []*domain.TrackArtist
					for i, ar := range enrichment.Artists {
						if ar.Name == "" {
							continue
						}
						enrich := &metadata.EnrichmentResult{
							Source:           enrichment.Source,
							ArtistExternalID: ar.ExternalID,
							ArtistCountry:    ar.Country,
							Artist:           ar.Name,
						}
						artist, err := findOrCreateArtist(ctx, er, artistRepo, ar.Name, enrich)
						if err != nil {
							logger.Info("[scan] create artist for enrichment %s: %v", ar.Name, err)
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
						if err := trackRepo.ReplaceTrackArtists(ctx, track.ID, newArtists); err != nil {
							logger.Error("[scan] replace track artists error for %s: %v", track.ID, err)
						} else {
							changed = true
						}
					}
				}
			}
		}
		// No AlbumExternalID gate: sources like the user cache enrich the
		// album by title/year/genre only (they never carry an album external
		// id), and those corrections must still land on the album row. Each
		// field below guards its own value.
		if len(track.Albums) > 0 {
			album, err := albumRepo.FindByID(ctx, track.Albums[0].AlbumID)
			if err != nil {
				logger.Info("[scan] album lookup for enrichment %s: %v", track.Albums[0].AlbumID, err)
			} else {
				updated := false
				if enrichment.AlbumExternalID != "" && (album.ExternalID == "" || overwrite) {
					if enrichment.Source != "" && album.MetadataSource != enrichment.Source {
						// Keep the old id reachable as an alias under its old
						// source before switching (mirrors the track branch),
						// or EntityResolver.FindAlbum would miss it and create
						// a duplicate on the next scan by the old source.
						if album.ExternalID != "" {
							if album.ExternalIDs == nil {
								album.ExternalIDs = map[string]string{}
							}
							src := album.MetadataSource
							if src == "" {
								src = metadata.SourceMusicBrainz
							}
							album.ExternalIDs[src] = album.ExternalID
						}
						album.MetadataSource = enrichment.Source
					}
					album.ExternalID = enrichment.AlbumExternalID
					updated = true
				}
				if enrichment.Album != "" && (album.Title == "" || album.Title == "Unknown Album" || overwrite) && album.Title != enrichment.Album {
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
					if err := albumRepo.Update(ctx, album); err != nil {
						logger.Error("[scan] album enrichment update error for %s: %v", album.ID, err)
					} else {
						changed = true
					}
				}
			}
		}
	}
	return
}
