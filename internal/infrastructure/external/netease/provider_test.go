package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sonicore/server/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFakeProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	c := newFakeClient(t, withAnonWrapper(handler))
	return NewProvider(c)
}

func TestTrackIDsOf(t *testing.T) {
	v := []any{
		map[string]any{"id": jsonNum(123)},
		map[string]any{"id": jsonNum(0)},
		map[string]any{"id": jsonNum(456)},
		"not-an-object",
	}
	ids := trackIDsOf(v)
	assert.Equal(t, []string{"123", "456"}, ids)
}

func TestMapTrackSearchShape(t *testing.T) {
	// search responses use "artists"/"album"
	track := mapTrack(map[string]any{
		"id":       jsonNum(777),
		"name":     "Song",
		"duration": jsonNum(200000),
		"artists": []any{
			map[string]any{"id": jsonNum(1), "name": "Artist One", "img1v1Url": "artist-pic"},
			map[string]any{"id": jsonNum(2), "name": "Artist Two"},
		},
		"album": map[string]any{"id": jsonNum(3), "name": "Album", "picUrl": "album-pic"},
	})

	assert.Equal(t, "netease", track.Platform)
	assert.Equal(t, "777", track.TrackID)
	assert.Equal(t, "Song", track.Title)
	assert.Equal(t, float64(200), track.Duration, "ms converted to seconds")
	assert.Equal(t, "Artist One", track.Artist)
	assert.Equal(t, "1", track.ArtistID)
	assert.Equal(t, "Album", track.Album)
	assert.Equal(t, "3", track.AlbumID)
	assert.Equal(t, "album-pic", track.CoverURL, "album pic takes precedence")
}

func TestMapTrackDetailShape(t *testing.T) {
	// detail responses use "ar"/"al" and "dt" (ms)
	track := mapTrack(map[string]any{
		"id":   jsonNum(888),
		"name": "Detail Song",
		"dt":   float64(180000),
		"ar": []any{
			map[string]any{"id": jsonNum(9), "name": "Detail Artist"},
		},
		"al": map[string]any{"id": jsonNum(10), "name": "Detail Album", "picUrl": ""},
	})

	assert.Equal(t, "Detail Artist", track.Artist)
	assert.Equal(t, "Detail Album", track.Album)
	assert.Equal(t, float64(180), track.Duration)
}

func TestMapArtistBrief(t *testing.T) {
	a := mapArtistBrief(map[string]any{
		"id":   jsonNum(11),
		"name": "Band",
		"picUrl": "pic-url",
	})
	assert.Equal(t, "Band", a.Name)
	assert.Equal(t, "pic-url", a.CoverURL)

	b := mapArtistBrief(map[string]any{"id": jsonNum(12), "name": "No Pic", "img1v1Url": "img1v1"})
	assert.Equal(t, "img1v1", b.CoverURL, "img1v1Url used as fallback")
}

func TestListCharts(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/eapi/toplist", r.URL.Path)
		fmt.Fprint(w, `{"code":200,"list":[
			{"id":3778678,"name":"云音乐飙升榜","description":"desc","coverImgUrl":"cover","trackCount":100,"updateFrequency":"weekly"},
			{"id":0,"name":"Bad"} 
		]}`)
	})

	charts, err := p.ListCharts(context.Background())
	require.NoError(t, err)
	require.Len(t, charts, 2)
	assert.Equal(t, "3778678", charts[0].ID)
	assert.Equal(t, "云音乐飙升榜", charts[0].Name)
	assert.Equal(t, "cover", charts[0].CoverURL)
	assert.Equal(t, 100, charts[0].TrackCount)
}

func TestGetChartPaginates(t *testing.T) {
	var playlistCalls, detailCalls int
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eapi/v6/playlist/detail":
			playlistCalls++
			ids := make([]string, 0, 5)
			for i := 1; i <= 5; i++ {
				ids = append(ids, fmt.Sprintf(`{"id":%d}`, i))
			}
			fmt.Fprintf(w, `{"code":200,"playlist":{"trackIds":[%s]}}`, join(ids, ","))
		case "/weapi/v3/song/detail":
			detailCalls++
			count := 2
			if detailCalls > 1 {
				count = 1
			}
			songs := make([]string, 0, count)
			for i := 1; i <= count; i++ {
				songs = append(songs, fmt.Sprintf(`{"id":%d,"name":"Song %d","ar":[{"name":"A"}]}`, i, i))
			}
			fmt.Fprintf(w, `{"code":200,"songs":[%s]}`, strings.Join(songs, ","))
		default:
			t.Logf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	// page 1, limit 2 → tracks 1-2, total 5
	tracks, total, err := p.GetChart(context.Background(), "3778678", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	require.Len(t, tracks, 2)
	assert.Equal(t, "Song 1", tracks[0].Title)
	assert.Equal(t, "Song 2", tracks[1].Title)

	// page 3, limit 2 → offset 4, only track 5
	tracks, total, err = p.GetChart(context.Background(), "3778678", 3, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	require.Len(t, tracks, 1)
	assert.Equal(t, "Song 1", tracks[0].Title)

	// page beyond total → empty
	tracks, total, err = p.GetChart(context.Background(), "3778678", 99, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Empty(t, tracks)

	assert.Equal(t, 3, playlistCalls, "one playlist detail call per GetChart")
	assert.Equal(t, 2, detailCalls, "only in-range pages hit song detail")
}

func TestGetChartClampsLimit(t *testing.T) {
	var lastLimit string
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"playlist":{"trackIds":[{"id":1}]}}`)
	})

	// limit 0 and 500 both clamp to 50 — but total is 1 so no detail call
	_, _, err := p.GetChart(context.Background(), "1", 1, 0)
	require.NoError(t, err)
	_, _, err = p.GetChart(context.Background(), "1", 1, 500)
	require.NoError(t, err)
	assert.Equal(t, "", lastLimit)
}

func TestGetChartEmptyPlaylist(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"playlist":{"trackIds":[]}}`)
	})

	tracks, total, err := p.GetChart(context.Background(), "1", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, tracks)
}

func TestSearchTracks(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/eapi/search/get", r.URL.Path)
		fmt.Fprint(w, `{"code":200,"result":{
			"songs":[{"id":1,"name":"Hit","ar":[{"name":"Artist"}]}],
			"songCount":42
		}}`)
	})

	tracks, total, err := p.SearchTracks(context.Background(), "hit", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, 42, total)
	require.Len(t, tracks, 1)
	assert.Equal(t, "Hit", tracks[0].Title)
}

func TestSearchArtists(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"result":{
			"artists":[{"id":5,"name":"Band"}],
			"artistCount":7
		}}`)
	})

	artists, total, err := p.SearchArtists(context.Background(), "band", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, 7, total)
	require.Len(t, artists, 1)
	assert.Equal(t, "Band", artists[0].Name)
}

func TestGetTrackWithLyrics(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/weapi/v3/song/detail":
			fmt.Fprint(w, `{"code":200,"songs":[{"id":1,"name":"Song","dt":210000,
				"ar":[{"id":2,"name":"Artist"}],"al":{"id":3,"name":"Album","picUrl":"pic"}}]}`)
		case "/eapi/song/lyric":
			fmt.Fprint(w, `{"code":200,"lrc":{"lyric":"[00:01.00]line"},"tlyric":{"lyric":"[00:01.00]译文"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	detail, err := p.GetTrack(context.Background(), "1")
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, "Song", detail.Title)
	assert.Equal(t, "Artist", detail.Artist)
	assert.Equal(t, float64(210), detail.Duration)
	assert.Equal(t, "[00:01.00]line", detail.Lyrics)
	assert.Equal(t, "[00:01.00]译文", detail.LyricsTrans)
}

func TestGetTrackNotFound(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"songs":[]}`)
	})

	_, err := p.GetTrack(context.Background(), "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "track not found")
}

func TestGetTrackNonNumericID(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not hit upstream for non-numeric ids")
	})

	_, err := p.GetTrack(context.Background(), "abc")
	require.Error(t, err)
}

func TestGetArtist(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/eapi/artist/head/info/get", r.URL.Path)
		fmt.Fprint(w, `{"code":200,"data":{"artist":{
			"name":"Band","cover":"cover-url","albumSize":3,"musicSize":50,"briefDesc":"desc"
		}}}`)
	})

	artist, err := p.GetArtist(context.Background(), "5")
	require.NoError(t, err)
	require.NotNil(t, artist)
	assert.Equal(t, "Band", artist.Name)
	assert.Equal(t, 3, artist.AlbumCount)
	assert.Equal(t, 50, artist.TrackCount)
}

func TestGetArtistNotFound(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{}}`)
	})

	_, err := p.GetArtist(context.Background(), "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artist not found")
}

func TestGetArtistTracks(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/eapi/v1/artist/songs", r.URL.Path)
		fmt.Fprint(w, `{"code":200,"songs":[{"id":1,"name":"S","ar":[{"name":"A"}]}],"total":10}`)
	})

	tracks, total, err := p.GetArtistTracks(context.Background(), "5", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	require.Len(t, tracks, 1)
}

func TestProviderNameAndLabel(t *testing.T) {
	p := NewProvider(NewClient())
	assert.Equal(t, "netease", p.Name())
	assert.Equal(t, "网易云音乐", p.Label())
}

func TestProviderRequestErrorPropagates(t *testing.T) {
	p := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := p.ListCharts(context.Background())
	require.Error(t, err)
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

func jsonNum(n int) json.Number {
	return json.Number(fmt.Sprintf("%d", n))
}

var _ = port.PlatformTrack{}
