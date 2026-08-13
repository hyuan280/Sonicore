package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sonicore/server/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMBSource starts a fake MusicBrainz API server and builds a source
// pointing at it. No real requests are ever sent.
func newTestMBSource(t *testing.T, handler http.HandlerFunc) *mbSource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &mbSource{
		name:     "musicbrainz",
		enabled:  true,
		priority: 10,
		resolver: NewResolver(MBConfig{APIURL: srv.URL, RateLimit: 10000, AppName: "TestApp", AppVer: "1.0"}),
		mb:       NewMBClient(MBConfig{APIURL: srv.URL, RateLimit: 10000, AppName: "TestApp", AppVer: "1.0"}),
	}
}

func ctx() context.Context { return context.Background() }

func TestMBSourceEnabledAndPriority(t *testing.T) {
	cfg := MBConfig{Enabled: true}
	s := NewMBSource(cfg)
	assert.Equal(t, "musicbrainz", s.Name())
	assert.True(t, s.Enabled())
	assert.Equal(t, 10, s.Priority())

	assert.False(t, NewMBSource(MBConfig{Enabled: false}).Enabled())
}

func TestMBSourceSearchCandidatesRanksBestFirst(t *testing.T) {	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/recording", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"recordings": [
				{"id":"r-exact","title":"Lantern","length":240000,
				 "artist-credit":[{"name":"Artist A","artist":{"id":"a-1","name":"Artist A","country":"CN"}}],
				 "releases":[{"id":"rel-1","title":"Album One","date":"2002-06-10","status":"official"}]},
				{"id":"r-other","title":"Something Else","length":180000,
				 "artist-credit":[{"name":"Band B","artist":{"id":"a-2","name":"Band B"}}],
				 "releases":[{"id":"rel-2","title":"Another Album","date":"2010-01-01"}]}
			]}`)
	})

	cands, err := s.SearchCandidates(ctx(), port.MetadataQuery{Title: "Lantern", Artist: "Artist A", Album: "Album One"})
	require.NoError(t, err)
	require.Len(t, cands, 2)

	first := cands[0]
	assert.Equal(t, "r-exact", first.ExternalID)
	assert.Equal(t, "Lantern", first.Title)
	assert.Equal(t, "Album One", first.Album)
	assert.Equal(t, "rel-1", first.AlbumExternalID)
	assert.Equal(t, 2002, first.Year)
	require.Len(t, first.Artists, 1)
	assert.Equal(t, "Artist A", first.Artists[0].Name)
	assert.Equal(t, "a-1", first.Artists[0].ExternalID)
	assert.Equal(t, "CN", first.Artists[0].Country)
	// exact title 0.5 + artist 0.3 + album 0.2
	assert.Equal(t, 1.0, first.Score)

	assert.Equal(t, "r-other", cands[1].ExternalID)
	assert.Less(t, cands[1].Score, first.Score)
}

func TestMBSourceSearchCandidatesSortsByScore(t *testing.T) {
	// MB returns its native order; the source must re-rank by local
	// confidence so the best match comes first.
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"recordings": [
				{"id":"r-weak","title":"Totally Unrelated","artist-credit":[{"name":"Other Band"}]},
				{"id":"r-best","title":"Lantern","length":240000,
				 "artist-credit":[{"name":"Artist A","artist":{"id":"a-1","name":"Artist A"}}],
				 "releases":[{"id":"rel-1","title":"Album One","status":"official"}]}
			]}`)
	})

	cands, err := s.SearchCandidates(context.Background(), port.MetadataQuery{Title: "Lantern", Artist: "Artist A", Album: "Album One"})
	require.NoError(t, err)
	require.Len(t, cands, 2)
	assert.Equal(t, "r-best", cands[0].ExternalID, "higher confidence first despite MB ordering")
	assert.Equal(t, "r-weak", cands[1].ExternalID)
	assert.Greater(t, cands[0].Score, cands[1].Score)
}

func TestMBSourceScoreIgnoresNonOfficialReleases(t *testing.T) {
	// A bootleg release that matches the query album must not inflate the
	// score: the produced candidate only surfaces official releases.
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"recordings": [
				{"id":"r-1","title":"Lantern","artist-credit":[{"name":"Artist A"}],
				 "releases":[{"id":"rel-boot","title":"Album One","status":"bootleg"},
				             {"id":"rel-other","title":"Another Album","status":"official"}]}
			]}`)
	})

	cands, err := s.SearchCandidates(context.Background(), port.MetadataQuery{Title: "Lantern", Artist: "Artist A", Album: "Album One"})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	// title 0.5 + artist 0.3, album bonus denied for the bootleg release
	assert.Equal(t, 0.8, cands[0].Score)
	assert.Equal(t, "Another Album", cands[0].Album, "candidate surfaces the official release")
}

func TestMBSourceSearchCandidatesPartialScores(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"recordings": [
				{"id":"r-1","title":"Free to Fly","artist-credit":[{"name":"Zhuge","artist":{"id":"a-1"}}],
				 "releases":[{"id":"rel-1","title":"Sky"}]}
			]}`)
	})

	cands, err := s.SearchCandidates(ctx(), port.MetadataQuery{Title: "Free to Fly", Artist: "Zhuge"})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	// title exact 0.5 + artist 0.3, album omitted from query
	assert.Equal(t, 0.8, cands[0].Score)
}

func TestMBSourceSearchCandidatesMultiArtist(t *testing.T) {
	var gotQuery string
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		fmt.Fprint(w, `{"recordings":[]}`)
	})

	_, err := s.SearchCandidates(ctx(), port.MetadataQuery{Title: "Song", Artist: "A, B / C"})
	require.NoError(t, err)
	assert.Equal(t, "recording:Song AND artist:A AND artist:B AND artist:C", gotQuery)
}

func TestMBSourceIdentify(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/recording" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"recordings":[
				{"id":"r-1","title":"Lantern",
				 "artist-credit":[{"name":"Artist A","artist":{"id":"a-1","country":"CN"}}],
				 "releases":[{"id":"rel-1","title":"Album One","date":"2002-06-10","status":"official"}]}
			]}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	cand, err := s.Identify(ctx(), port.MetadataQuery{Title: "Lantern", Artist: "Artist A", Album: "Album One"})
	require.NoError(t, err)
	require.NotNil(t, cand)
	assert.Equal(t, "musicbrainz", cand.Source)
	assert.Equal(t, "r-1", cand.ExternalID)
	assert.Equal(t, "Lantern", cand.Title)
	assert.Equal(t, "Album One", cand.Album)
	assert.Equal(t, 2002, cand.Year)
	assert.Equal(t, 1.0, cand.Score)
}

func TestMBSourceIdentifyNoMatch(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"recordings":[]}`)
	})

	cand, err := s.Identify(ctx(), port.MetadataQuery{Title: "Unknown Song"})
	require.NoError(t, err)
	assert.Nil(t, cand)
}

func TestMBSourceLookup(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/recording/r-1":
			fmt.Fprint(w, `{
				"id":"r-1","title":"Lantern",
				"artist-credit":[{"name":"Artist A","artist":{"id":"a-1","name":"Artist A","country":"CN"}}],
				"releases":[{"id":"rel-1","title":"Album One","date":"2002-06-10","status":"official"}]
			}`)
		case "/release/rel-1":
			fmt.Fprint(w, `{"id":"rel-1","title":"Album One","date":"2002-06-10","status":"official","tags":[]}`)
		case "/artist/a-1":
			fmt.Fprint(w, `{"id":"a-1","name":"Artist A","country":"CN","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	cand, err := s.Lookup(ctx(), "r-1")
	require.NoError(t, err)
	require.NotNil(t, cand)
	assert.Equal(t, "r-1", cand.ExternalID)
	assert.Equal(t, "Album One", cand.Album)
	require.Len(t, cand.Artists, 1)
	assert.Equal(t, "CN", cand.Artists[0].Country)
}

func TestMBSourceLookupNotFoundIsNil(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	})

	cand, err := s.Lookup(context.Background(), "ghost-mbid")
	require.NoError(t, err)
	assert.Nil(t, cand, "unresolvable ID is a miss, not an error")
}

func TestMBSourceSearchCandidatesError(t *testing.T) {
	s := newTestMBSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	})

	_, err := s.SearchCandidates(ctx(), port.MetadataQuery{Title: "Song"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "musicbrainz HTTP 500")
}
