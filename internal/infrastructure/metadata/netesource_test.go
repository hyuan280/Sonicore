package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
)

type fakeNeteaseProvider struct {
	search func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error)
	enrich func(ctx context.Context, tracks []port.PlatformTrack) ([]port.PlatformTrack, error)
	track  func(ctx context.Context, id string) (*port.TrackDetail, error)
}

func (f *fakeNeteaseProvider) SearchTracks(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
	if f.search == nil {
		return nil, 0, errors.New("unexpected search")
	}
	return f.search(ctx, query, page, limit)
}

func (f *fakeNeteaseProvider) EnrichTracks(ctx context.Context, tracks []port.PlatformTrack) ([]port.PlatformTrack, error) {
	if f.enrich == nil {
		return tracks, nil
	}
	return f.enrich(ctx, tracks)
}

func (f *fakeNeteaseProvider) GetTrack(ctx context.Context, trackID string) (*port.TrackDetail, error) {
	if f.track == nil {
		return nil, errors.New("unexpected lookup")
	}
	return f.track(ctx, trackID)
}

func (f *fakeNeteaseProvider) SearchArtists(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
	return nil, 0, errors.New("unexpected SearchArtists")
}

func (f *fakeNeteaseProvider) SearchAlbums(ctx context.Context, query string, page, limit int) ([]map[string]any, int, error) {
	return nil, 0, errors.New("unexpected SearchAlbums")
}

func (f *fakeNeteaseProvider) GetAlbum(ctx context.Context, albumID string) (*netease.AlbumDetail, error) {
	return nil, errors.New("unexpected GetAlbum")
}

func neTrack(id, title, artist, album string) port.PlatformTrack {
	t := port.PlatformTrack{
		Platform: "netease",
		TrackID:  id,
		Title:    title,
		Artist:   artist,
		Album:    album,
	}
	for _, n := range splitRawArtists(artist) {
		t.Artists = append(t.Artists, port.ArtistInfo{Name: n})
	}
	return t
}

func TestNeteaseSourceDisabled(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, false)
	assert.False(t, s.Enabled())
	assert.Equal(t, "netease", s.Name())
	assert.Equal(t, 20, s.Priority())
}

func TestNeteaseSourceIdentifyConfident(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		search: func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
			assert.Equal(t, "晴天 周杰伦", query, "query joins title and artist")
			return []port.PlatformTrack{
				neTrack("1001", "晴天 (Live)", "周杰伦", "叶惠美"),
				neTrack("1002", "其他的歌", "别的艺人", "别的专辑"),
			}, 2, nil
		},
	}
	c, err := s.Identify(context.Background(), port.MetadataQuery{Title: "晴天", Artist: "周杰伦"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "1001", c.ExternalID)
	assert.Equal(t, "晴天", c.Title, "paren suffix trimmed")
	assert.Equal(t, "netease", c.Source)
	require.Len(t, c.Artists, 1)
	assert.Equal(t, "周杰伦", c.Artists[0].Name)
	assert.True(t, c.Score >= identifyThreshold)
}

func TestNeteaseSourceIdentifyNoMatch(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		search: func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
			// containment-only title, wrong artist, wrong album: 0.3
			return []port.PlatformTrack{neTrack("2001", "晴天霹雳", "别的艺人", "别的专辑")}, 1, nil
		},
	}
	c, err := s.Identify(context.Background(), port.MetadataQuery{Title: "晴天", Artist: "周杰伦"})
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestNeteaseSourceIdentifyError(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		search: func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
			return nil, 0, errors.New("upstream down")
		},
	}
	_, err := s.Identify(context.Background(), port.MetadataQuery{Title: "晴天"})
	require.Error(t, err)
}

func TestNeteaseSourceSearchCandidatesRanked(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		search: func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
			return []port.PlatformTrack{
				neTrack("3001", "云与海", "李健", "拾光"),
				neTrack("3002", "云与海 (Live)", "李健", "演唱会"),
				neTrack("3003", "云上的海", "路人", "X"),
			}, 3, nil
		},
	}
	cs, err := s.SearchCandidates(context.Background(), port.MetadataQuery{Title: "云与海", Artist: "李健", Album: "拾光"})
	require.NoError(t, err)
	require.Len(t, cs, 3)
	assert.Equal(t, "3001", cs[0].ExternalID, "exact title + artist + album first")
	for i := 1; i < len(cs); i++ {
		assert.GreaterOrEqual(t, cs[i-1].Score, cs[i].Score, "descending confidence")
	}
}

func TestNeteaseSourceLookupFull(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		track: func(ctx context.Context, id string) (*port.TrackDetail, error) {
			assert.Equal(t, "4001", id)
			return &port.TrackDetail{
				Platform:    "netease",
				TrackID:     "4001",
				Title:       "传奇 (Album Ver.)",
				Artist:      "李健",
				ArtistID:    "77",
				Album:       "似水流年",
				AlbumID:     "88",
				CoverURL:    "https://p1.music.126.net/cover.jpg",
				Lyrics:      "[00:00.00]只是因为在人群中多看了你一眼",
				PublishTime: "1036454400000",
			}, nil
		},
	}
	c, err := s.Lookup(context.Background(), "4001")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "netease", c.Source)
	assert.Equal(t, "传奇", c.Title)
	assert.Equal(t, "似水流年", c.Album)
	assert.Equal(t, "88", c.AlbumExternalID)
	assert.Equal(t, 0, c.Year, "year deliberately not decoded (provider never fills publish time; FieldYear undeclared)")
	assert.Equal(t, "https://p1.music.126.net/cover.jpg", c.CoverArtURL)
	assert.Contains(t, c.Lyrics, "多看了你一眼")
	assert.Equal(t, 1.0, c.Score)
}

func TestNeteaseSourceLookupNotFound(t *testing.T) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		track: func(ctx context.Context, id string) (*port.TrackDetail, error) {
			return nil, fmt.Errorf("%w: %s", netease.ErrTrackNotFound, id)
		},
	}
	c, err := s.Lookup(context.Background(), "9999")
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestNeteaseSourceIdentifyEmptyTitleArtistAlbumThreshold(t *testing.T) {
	// An empty-title query whose artist and album both match exactly scores
	// 0.3 (artist) + 0.2 (album) = 0.5, exactly the identify threshold. The
	// short-circuit in Identify must not reject it (title absent but both
	// artist and album present), searchQuery must keep the album in the search
	// string, and the candidate must come back. Guards this design path from
	// silent breakage if scoring/thresholds are ever retuned.
	s := NewNeteaseSource(&netease.Provider{}, true)
	s.provider = &fakeNeteaseProvider{
		search: func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
			assert.Equal(t, "周杰伦 叶惠美", query, "empty title keeps artist + album in the search string")
			return []port.PlatformTrack{neTrack("5001", "晴天", "周杰伦", "叶惠美")}, 1, nil
		},
	}
	c, err := s.Identify(context.Background(), port.MetadataQuery{Artist: "周杰伦", Album: "叶惠美"})
	require.NoError(t, err)
	require.NotNil(t, c, "0.3 artist + 0.2 album meets the 0.5 threshold")
	assert.Equal(t, "5001", c.ExternalID)
	assert.True(t, c.Score >= identifyThreshold)
}

func TestSearchQuery(t *testing.T) {
	assert.Equal(t, "晴天 周杰伦", searchQuery(port.MetadataQuery{Title: "晴天", Artist: "周杰伦"}))
	assert.Equal(t, "晴天", searchQuery(port.MetadataQuery{Title: "晴天"}))
	assert.Equal(t, "周杰伦", searchQuery(port.MetadataQuery{Artist: "周杰伦"}))
	assert.Equal(t, "周杰伦 叶惠美", searchQuery(port.MetadataQuery{Artist: "周杰伦", Album: "叶惠美"}), "empty title keeps the album in the search string")
	assert.Equal(t, "叶惠美", searchQuery(port.MetadataQuery{Album: "叶惠美"}), "album alone still searches")
	assert.Equal(t, "", searchQuery(port.MetadataQuery{}))
}

func TestScoreNeteaseTrack(t *testing.T) {
	q := port.MetadataQuery{Title: "晴天", Artist: "周杰伦", Album: "叶惠美"}
	// exact title + artist + album (capped)
	assert.InDelta(t, 1.0, scoreNeteaseTrack(q, neTrack("1", "晴天", "周杰伦", "叶惠美")), 1e-9)
	// exact title only
	assert.InDelta(t, 0.5, scoreNeteaseTrack(q, neTrack("1", "晴天", "", "")), 1e-9)
	// paren-suffixed title normalizes to exact + artist
	assert.InDelta(t, 0.8, scoreNeteaseTrack(q, neTrack("1", "晴天 (Live)", "周杰伦", "")), 1e-9)
	// unrelated
	assert.InDelta(t, 0.0, scoreNeteaseTrack(q, neTrack("1", "别的", "别的", "别的")), 1e-9)
	// Query-side paren suffix trimmed symmetrically: "晴天 (Live)" matches a
	// candidate titled "晴天 (Live)" exactly, not by containment.
	assert.InDelta(t, 0.5, scoreNeteaseTrack(port.MetadataQuery{Title: "晴天 (Live)"}, neTrack("1", "晴天 (Live)", "", "")), 1e-9)
	// Whole-name artist compare: a separator-bearing name matches as a unit
	// (the API returns complete names; splitRawArtists would split "AC/DC").
	whole := port.PlatformTrack{Platform: "netease", TrackID: "2", Title: "x", Artist: "AC/DC"}
	whole.Artists = []port.ArtistInfo{{Name: "AC/DC"}}
	assert.InDelta(t, 0.3, scoreNeteaseTrack(port.MetadataQuery{Artist: "AC/DC"}, whole), 1e-9, "whole-name artist matches as a unit")
	// A multi-artist query must NOT collapse into a single combined name: the
	// whole compare keeps separators, so "Pink, Floyd" cannot match the
	// single artist "Pink Floyd" (normalizeForMatch would fold the comma).
	multi := port.PlatformTrack{Platform: "netease", TrackID: "4", Title: "x", Artist: "Pink Floyd"}
	multi.Artists = []port.ArtistInfo{{Name: "Pink Floyd"}}
	assert.InDelta(t, 0.0, scoreNeteaseTrack(port.MetadataQuery{Artist: "Pink, Floyd"}, multi), 1e-9, "multi-artist query does not match a single combined name")
	// Partial fallback: query "AC/DC" against a candidate carrying only "AC".
	partial := port.PlatformTrack{Platform: "netease", TrackID: "3", Title: "x", Artist: "AC"}
	partial.Artists = []port.ArtistInfo{{Name: "AC"}}
	assert.InDelta(t, 0.15, scoreNeteaseTrack(port.MetadataQuery{Artist: "AC/DC"}, partial), 1e-9, "whole compare misses, split fallback gives partial credit")
}
