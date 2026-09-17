package metadata

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
)

func newImageSource() (*neteaseSource, *fakeNeteaseProvider) {
	s := NewNeteaseSource(&netease.Provider{}, true)
	f := &fakeNeteaseProvider{}
	s.provider = f
	return s, f
}

func TestCanonicalMatchArtistName(t *testing.T) {
	assert.True(t, CanonicalMatchArtistName("周杰伦", "周杰倫"), "simplified/traditional unify")
	assert.True(t, CanonicalMatchArtistName("周杰伦", "周杰伦"))
	assert.True(t, CanonicalMatchArtistName("The Beatles", "THE BEATLES"), "case-insensitive")
	assert.False(t, CanonicalMatchArtistName("周杰伦", "林俊杰"))
}

func TestNeteaseLookupArtist(t *testing.T) {
	s, f := newImageSource()
	f.artist = func(ctx context.Context, id string) (*port.ArtistDetail, error) {
		assert.Equal(t, "6452", id)
		return &port.ArtistDetail{Platform: "netease", ArtistID: "6452", Name: "周杰伦", CoverURL: "https://p1.music.126.net/artist.jpg"}, nil
	}
	d, err := s.LookupArtist(context.Background(), "6452")
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, "周杰伦", d.Name)
	assert.Equal(t, "https://p1.music.126.net/artist.jpg", d.CoverURL)

	s2, f2 := newImageSource()
	f2.artist = func(ctx context.Context, id string) (*port.ArtistDetail, error) {
		return nil, fmt.Errorf("%w: %s", netease.ErrArtistNotFound, id)
	}
	d, err = s2.LookupArtist(context.Background(), "9999")
	require.NoError(t, err)
	assert.Nil(t, d)
}

func TestResolveArtistImageMultiSourceDeterministic(t *testing.T) {
	s, f := newImageSource()
	f.artist = func(ctx context.Context, id string) (*port.ArtistDetail, error) {
		return &port.ArtistDetail{ArtistID: id, CoverURL: "https://artist.jpg"}, nil
	}
	artist := &domain.Artist{ID: "a1", Name: "周杰伦", MetadataSource: "netease", ExternalID: "6452"}
	res := ResolveArtistImage(context.Background(), []port.MetadataSource{s}, artist, nil)
	assert.Equal(t, "https://artist.jpg", res.CoverURL)
	assert.Equal(t, "netease", res.Source)
}

func TestResolveArtistImageNameSearchSingleExact(t *testing.T) {
	s, f := newImageSource()
	f.searchArtists = func(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
		return []port.ArtistDetail{{Platform: "netease", ArtistID: "6452", Name: "周杰伦", CoverURL: "https://artist.jpg"}}, 1, nil
	}
	artist := &domain.Artist{ID: "a1", Name: "周杰伦", MetadataSource: "musicbrainz", ExternalID: "mbid"}
	res := ResolveArtistImage(context.Background(), []port.MetadataSource{s}, artist, nil)
	assert.Equal(t, "https://artist.jpg", res.CoverURL)
	assert.Equal(t, "6452", res.ArtistExternalID)
	assert.Equal(t, "6452", artist.ExternalIDs["netease"], "discovered source id recorded onto the artist")
}

func TestResolveArtistImageDisambiguationByTrack(t *testing.T) {
	s, f := newImageSource()
	f.searchArtists = func(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
		return []port.ArtistDetail{
			{Platform: "netease", ArtistID: "1", Name: "河图", CoverURL: "https://wrong.jpg"},
			{Platform: "netease", ArtistID: "2", Name: "河图", CoverURL: "https://right.jpg"},
		}, 2, nil
	}
	// Identifying the library track returns full credits: 河图 maps to id "2".
	f.search = func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
		return []port.PlatformTrack{{
			Platform: "netease",
			TrackID:  "100",
			Title:    "缘生意转",
			Artists:  []port.ArtistInfo{{Name: "音频怪物", ExternalID: "X"}, {Name: "河图", ExternalID: "2"}},
		}}, 1, nil
	}
	artist := &domain.Artist{ID: "a1", Name: "河图", MetadataSource: "musicbrainz", ExternalID: "mbid"}
	res := ResolveArtistImage(context.Background(), []port.MetadataSource{s}, artist, []ArtistTrackRef{{ID: "t1", Title: "缘生意转"}})
	assert.Equal(t, "https://right.jpg", res.CoverURL)
	assert.Equal(t, "2", res.ArtistExternalID)
	assert.Equal(t, "100", res.TrackExternalID)
	assert.Equal(t, "t1", res.TrackID, "library track ID is returned, not matched by title")
	assert.Equal(t, "缘生意转", res.TrackTitle)
	assert.Equal(t, "2", artist.ExternalIDs["netease"], "disambiguated source id recorded onto the artist")
}

func TestResolveArtistImageDisambiguationNoMatch(t *testing.T) {
	s, f := newImageSource()
	f.searchArtists = func(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
		return []port.ArtistDetail{
			{Platform: "netease", ArtistID: "1", Name: "河图", CoverURL: "https://a.jpg"},
			{Platform: "netease", ArtistID: "2", Name: "河图", CoverURL: "https://b.jpg"},
		}, 2, nil
	}
	f.search = func(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
		return []port.PlatformTrack{{TrackID: "100", Title: "缘生意转", Artists: []port.ArtistInfo{{Name: "别人", ExternalID: "Z"}}}}, 1, nil
	}
	artist := &domain.Artist{ID: "a1", Name: "河图", MetadataSource: "musicbrainz", ExternalID: "mbid"}
	res := ResolveArtistImage(context.Background(), []port.MetadataSource{s}, artist, []ArtistTrackRef{{ID: "t1", Title: "缘生意转"}})
	assert.Empty(t, res.CoverURL, "inconclusive disambiguation must not pick a random candidate")
}
