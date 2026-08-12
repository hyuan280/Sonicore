package metadata

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMBClient starts a fake MusicBrainz API server and points the client at it.
// No real requests are ever sent.
func newTestMBClient(t *testing.T, handler http.HandlerFunc) *MBClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewMBClient(MBConfig{
		APIURL:    srv.URL,
		RateLimit: 10000, // near-zero sleep between requests
		AppName:   "TestApp",
		AppVer:    "1.0",
	})
}

func TestNewMBClientDefaults(t *testing.T) {
	c := NewMBClient(MBConfig{})
	assert.Equal(t, "https://musicbrainz.org/ws/2", c.base)
	assert.Equal(t, 1, c.rateLimitPerSec)
	assert.Equal(t, "Sonicore", c.appName)
	assert.Equal(t, "0.1.0", c.appVer)
}

func TestMBClientGetSendsFormatAndQuery(t *testing.T) {
	var gotPath, gotQuery string
	var gotUA string
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"artists":[]}`)
	})

	_, err := c.SearchArtists("The Beatles")
	require.NoError(t, err)

	assert.Equal(t, "/artist", gotPath)
	q, err := url.ParseQuery(gotQuery)
	require.NoError(t, err)
	assert.Equal(t, "json", q.Get("fmt"))
	assert.Equal(t, "artist:The Beatles", q.Get("query"))
	assert.Equal(t, "10", q.Get("limit"))
	assert.Equal(t, "TestApp/1.0 ( sonicore@localhost )", gotUA)
}

func TestSearchRecordings(t *testing.T) {
	var gotQuery string
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/recording", r.URL.Path)
		gotQuery = r.URL.Query().Get("query")
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		assert.Equal(t, "artists+releases+tags", r.URL.Query().Get("inc"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"recordings": [
				{"id":"r-1","title":"Song","length":240000,
				 "artist-credit":[{"name":"Band One","artist":{"id":"a-1","name":"Band One"}}],
				 "releases":[{"id":"rel-1","title":"Album"}],
				 "tags":[{"name":"rock","count":3}]}
			]}`)
	})

	recordings, err := c.SearchRecordings("Song", []string{"Band One", "Unknown Artist", ""}, "Album")
	require.NoError(t, err)
	require.Len(t, recordings, 1)

	rec := recordings[0]
	assert.Equal(t, "r-1", rec.ID)
	assert.Equal(t, "Song", rec.Title)
	assert.Equal(t, 240000, rec.Length)
	require.Len(t, rec.Artists, 1)
	assert.Equal(t, "Band One", rec.Artists[0].Name)
	require.Len(t, rec.Releases, 1)
	assert.Equal(t, "Album", rec.Releases[0].Title)
	require.Len(t, rec.Tags, 1)
	assert.Equal(t, "rock", rec.Tags[0].Name)

	// query building: skips empty / Unknown Artist / Unknown Album
	expected := "recording:Song AND artist:Band One AND release:Album"
	assert.Equal(t, expected, gotQuery)
}

func TestSearchRecordingsSkippedArtists(t *testing.T) {
	var gotQuery string
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		fmt.Fprint(w, `{"recordings":[]}`)
	})

	_, err := c.SearchRecordings("Song", []string{"", "Unknown Artist"}, "Unknown Album")
	require.NoError(t, err)
	assert.Equal(t, "recording:Song", gotQuery)
}

func TestSearchArtist(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[
			{"id":"a-1","name":"Radiohead","sort-name":"Radiohead","type":"Group","country":"GB"},
			{"id":"a-2","name":"Radiohead Tribute","sort-name":"Radiohead Tribute"}
		]}`)
	})

	artist, err := c.SearchArtist("Radiohead")
	require.NoError(t, err)
	assert.Equal(t, "a-1", artist.ID)
	assert.Equal(t, "Radiohead", artist.Name)
	assert.Equal(t, "Group", artist.Type)
	assert.Equal(t, "GB", artist.Country)
}

func TestSearchArtistNoResults(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists":[]}`)
	})

	_, err := c.SearchArtist("Nonexistent Band 000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no artist found")
}

func TestSearchArtistsReturnsList(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		fmt.Fprint(w, `{"artists":[{"id":"a-1","name":"X"},{"id":"a-2","name":"Y"}]}`)
	})

	artists, err := c.SearchArtists("X")
	require.NoError(t, err)
	require.Len(t, artists, 2)
	assert.Equal(t, "a-2", artists[1].ID)
}

func TestSearchReleases(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/release", r.URL.Path)
		assert.Equal(t, "artists", r.URL.Query().Get("inc"))
		fmt.Fprint(w, `{"releases":[
			{"id":"rel-1","title":"Album","date":"1999-05-10","status":"Official",
			 "artist-credit":[{"name":"Band"}],
			 "media":[{"format":"CD","track-count":10}]}
		]}`)
	})

	releases, err := c.SearchReleases("Album")
	require.NoError(t, err)
	require.Len(t, releases, 1)

	rel := releases[0]
	assert.Equal(t, "rel-1", rel.ID)
	assert.Equal(t, "Official", rel.Status)
	require.Len(t, rel.Artists, 1)
	assert.Equal(t, "Band", rel.Artists[0].Name)
	require.Len(t, rel.Media, 1)
	assert.Equal(t, "CD", rel.Media[0].Format)
	assert.Equal(t, 10, rel.Media[0].TrackCount)
}

func TestSearchReleaseWithArtist(t *testing.T) {
	var gotQuery string
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		fmt.Fprint(w, `{"releases":[{"id":"rel-1","title":"Album"}]}`)
	})

	rel, err := c.SearchRelease("Album", "Band")
	require.NoError(t, err)
	assert.Equal(t, "rel-1", rel.ID)
	assert.Equal(t, "release:Album AND artist:Band", gotQuery)
}

func TestSearchReleaseNoResults(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"releases":[]}`)
	})

	_, err := c.SearchRelease("Nope", "Band")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no release found")
}

func TestLookupArtist(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/artist/mbid-1", r.URL.Path)
		fmt.Fprint(w, `{"id":"mbid-1","name":"Band","type":"Group","country":"US",
			"area":{"name":"USA"},
			"life-span":{"begin":"1990","end":"2010","ended":true},
			"tags":[{"name":"rock","count":5}]}`)
	})

	artist, err := c.LookupArtist("mbid-1")
	require.NoError(t, err)
	assert.Equal(t, "US", artist.Country)
	require.NotNil(t, artist.Area)
	assert.Equal(t, "USA", artist.Area.Name)
	require.NotNil(t, artist.LifeSpan)
	assert.True(t, artist.LifeSpan.Ended)
	assert.Equal(t, "1990", artist.LifeSpan.Begin)
}

func TestLookupRelease(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/release/mbid-9", r.URL.Path)
		assert.Equal(t, "artists+recordings+tags", r.URL.Query().Get("inc"))
		fmt.Fprint(w, `{"id":"mbid-9","title":"Album","date":"2001-01-01","country":"DE",
			"status":"Official",
			"media":[{"title":"CD1","format":"CD","track-count":2,"tracks":[
				{"id":"t-1","number":"1","title":"Song","length":180000}
			]}],
			"tags":[{"name":"jazz","count":2}]}`)
	})

	release, err := c.LookupRelease("mbid-9")
	require.NoError(t, err)
	require.Len(t, release.Media, 1)
	require.Len(t, release.Media[0].Tracks, 1)
	assert.Equal(t, "Song", release.Media[0].Tracks[0].Title)
	assert.Equal(t, 180000, release.Media[0].Tracks[0].Length)
}

func TestMBGetNon200(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad request body")
	})

	_, err := c.SearchArtist("X")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "musicbrainz HTTP 400")
	assert.Contains(t, err.Error(), "bad request body")
}

func TestMBGetTruncatesErrorBody(t *testing.T) {
	long := strings.Repeat("x", 500)
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, long)
	})

	_, err := c.SearchArtist("X")
	require.Error(t, err)
	assert.True(t, len(err.Error()) < 400, "error body should be truncated")
}

func TestMBGetInvalidJSON(t *testing.T) {
	c := newTestMBClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"artists": not-json`)
	})

	_, err := c.SearchArtist("X")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestMBGetMalformedURL(t *testing.T) {
	c := NewMBClient(MBConfig{APIURL: "http://[::1:bad", RateLimit: 10000})
	_, err := c.SearchArtist("X")
	require.Error(t, err)
}

// rewriterTransport rewrites coverartarchive.org requests to the test server.
type rewriterTransport struct {
	target string
	inner  http.RoundTripper
}

func (rt *rewriterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "coverartarchive.org" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.target
	}
	return rt.inner.RoundTrip(req)
}

// newCoverArtClient builds an MBClient whose coverartarchive.org requests are
// routed to the test server (the domain is hardcoded in FetchCoverArt).
func newCoverArtClient(t *testing.T, handler http.HandlerFunc) *MBClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewMBClient(MBConfig{RateLimit: 10000, AppName: "TestApp", AppVer: "1.0"})
	c.http = &http.Client{Transport: &rewriterTransport{
		target: strings.TrimPrefix(srv.URL, "http://"),
		inner:  http.DefaultTransport,
	}}
	return c
}

func TestFetchCoverArt(t *testing.T) {
	c := newCoverArtClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/release/mbid-1/front", r.URL.Path)
		assert.Contains(t, r.Header.Get("User-Agent"), "TestApp/1.0")
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-image"))
	})

	data, ext, err := c.FetchCoverArt("mbid-1")
	require.NoError(t, err)
	assert.Equal(t, "fake-image", string(data))
	assert.Equal(t, "png", ext)
}

func TestFetchCoverArtJpgDefault(t *testing.T) {
	c := newCoverArtClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg"))
	})

	_, ext, err := c.FetchCoverArt("mbid-2")
	require.NoError(t, err)
	assert.Equal(t, "jpg", ext)
}

func TestFetchCoverArtNotFound(t *testing.T) {
	c := newCoverArtClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, _, err := c.FetchCoverArt("mbid-3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cover art HTTP 404")
}

func TestZeroValueClientFetchCoverArtNoPanic(t *testing.T) {
	// Regression: rateLimit() used to divide by zero on a zero-value MBClient.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg"))
	}))
	defer srv.Close()

	c := &MBClient{
		http: &http.Client{Transport: &rewriterTransport{
			target: strings.TrimPrefix(srv.URL, "http://"),
			inner:  http.DefaultTransport,
		}},
	}

	_, _, err := c.FetchCoverArt("mbid-zero")
	require.NoError(t, err)
}

func TestGenreFromTags(t *testing.T) {
	tests := []struct {
		name string
		tags []MBTag
		want string
	}{
		{"empty", nil, ""},
		{"known genre matched case-insensitively", []MBTag{{Name: "Rock", Count: 5}, {Name: "pop", Count: 2}}, "Rock"},
		{"known genre found later in list", []MBTag{{Name: "experimental", Count: 9}, {Name: "jazz", Count: 3}}, "jazz"},
		{"unknown tags fall back to first", []MBTag{{Name: "indietronica", Count: 1}, {Name: "shoegaze", Count: 2}}, "indietronica"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GenreFromTags(tt.tags))
		})
	}
}

func TestMBClientClose(t *testing.T) {
	c := NewMBClient(MBConfig{})
	c.Close() // must not panic
}
