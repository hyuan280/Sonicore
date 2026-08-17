package metadata

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrimParenSuffix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"奢香夫人 (展现一代彝族巾帼英雄奢香夫人的异域风情歌曲)", "奢香夫人"},
		{"自由飞翔 （album version）", "自由飞翔 "}, // trailing space kept: cut happens before " （"
		{"Plain Title", "Plain Title"},
		{"", ""},
		{"(starts with paren)", "(starts with paren)"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, TrimParenSuffix(tt.in))
		})
	}
}

func TestLooksGarbled(t *testing.T) {
	assert.False(t, looksGarbled("Clean Title"))
	assert.False(t, looksGarbled(""))
	assert.True(t, looksGarbled("Bad\xff\xfeTitle"))
	assert.True(t, looksGarbled("Replacement \uFFFD char"))
}

func TestNormalizeForMatch(t *testing.T) {
	assert.Equal(t, "the beatles", normalizeForMatch("  The  Beatles  "))
	assert.Equal(t, "a b", normalizeForMatch("A&B"), "ampersand replaced with space")
	assert.Equal(t, "x y", normalizeForMatch("x–y"))
	assert.Equal(t, "a b", normalizeForMatch("a and b"), "standalone 'and' dropped")
	assert.Equal(t, "x y", normalizeForMatch("x and and y"), "consecutive 'and's all dropped")
	assert.Equal(t, "", normalizeForMatch("and and"), "leading consecutive 'and's all dropped")
	assert.Equal(t, "command", normalizeForMatch("Command"), "'and' inside a word is kept")
	assert.Equal(t, "梦想and现实", normalizeForMatch("梦想and现实"), "mixed-script 'and' is not a standalone word")
	assert.Equal(t, "ロックandダンス", normalizeForMatch("ロックandダンス"), "mixed-script 'and' is not a standalone word")
	assert.Equal(t, "and_justice_for_all", normalizeForMatch("And_Justice_For_All"), "underscore is a word char, not a boundary")
	assert.Equal(t, "", normalizeForMatch("  "))
}

func TestSplitByDash(t *testing.T) {
	assert.Equal(t, []string{"Song", "Live"}, splitByDash("Song (Live)"))
	assert.Equal(t, []string{"a", "b", "c"}, splitByDash("a - b, c"))
	assert.Empty(t, splitByDash("---"))
}

func TestSplitArtists(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitArtists("A / B"))
	assert.Equal(t, []string{"a", "b"}, splitArtists("A, B"))
	assert.Equal(t, []string{"a", "b"}, splitArtists("A & B"))
	assert.Equal(t, []string{"乐队"}, splitArtists("乐队"))
	assert.Empty(t, splitArtists("  "))
}

func TestTitlesMatch(t *testing.T) {
	assert.True(t, titlesMatch("Song Title", "Song Title"))
	assert.True(t, titlesMatch("Song Title (Live Version)", "Song Title"))
	assert.True(t, titlesMatch("Song", "Song - Live"))
	assert.True(t, titlesMatch("ABC", "ABC 2024 Remastered"))
	assert.False(t, titlesMatch("One Song", "Completely Different"))
	assert.True(t, titlesMatch("烟火", "烟火"), "short CJK titles match by token")
}

func TestMatchPartsScore(t *testing.T) {
	rec := MBRecording{
		Title: "Song (Live)",
		Artists: []MBArtistRef{{Name: "Band One"}},
		Releases: []struct {
			ID     string         `json:"id"`
			Title  string         `json:"title"`
			Date   string         `json:"date"`
			Status string         `json:"status"`
			Artists []MBArtistRef `json:"artist-credit"`
		}{{Title: "Album Name"}},
	}

	assert.Equal(t, 3, matchPartsScore([]string{"Song", "Band One", "Album Name"}, &rec))
	assert.Equal(t, 1, matchPartsScore([]string{"Song"}, &rec))
	assert.Equal(t, 0, matchPartsScore([]string{"Unrelated"}, &rec))
}

func TestScoreMatch(t *testing.T) {
	r := &Resolver{}
	recordings := []MBRecording{
		{Title: "Something Else"},
		{Title: "Target (Live)", Artists: []MBArtistRef{{Name: "Band"}}},
	}
	// filename-derived title containing both track and artist tokens
	got := r.scoreMatch("Target - Band", recordings)
	require.NotNil(t, got)
	assert.Equal(t, "Target (Live)", got.Title)

	assert.Nil(t, r.scoreMatch("Nothing Here", recordings))
}

func TestFieldsMatch(t *testing.T) {
	rec := MBRecording{
		Title:   "Song",
		Artists: []MBArtistRef{{Name: "Band One"}, {Name: "Band Two"}},
		Releases: []struct {
			ID      string         `json:"id"`
			Title   string         `json:"title"`
			Date    string         `json:"date"`
			Status  string         `json:"status"`
			Artists []MBArtistRef `json:"artist-credit"`
		}{{Title: "Album"}},
	}

	assert.True(t, fieldsMatch("Song", "Band One", "Album", &rec))
	assert.True(t, fieldsMatch("Song", "Band One / Band Two", "Album", &rec), "multi-artist all matched")
	assert.False(t, fieldsMatch("Other", "Band One", "Album", &rec), "title mismatch")
	assert.False(t, fieldsMatch("Song", "Wrong Band", "Album", &rec), "artist mismatch")
	assert.False(t, fieldsMatch("Song", "Band One", "Different Record", &rec), "album mismatch")
	assert.True(t, fieldsMatch("Song", "Unknown Artist", "Album", &rec), "unknown artist skipped")
}

func TestBestArtistMatch(t *testing.T) {
	r := &Resolver{}
	recordings := []MBRecording{
		{Title: "Song (Live)", Artists: []MBArtistRef{{Name: "Wrong Band"}}},
		{Title: "Song (Studio)", Artists: []MBArtistRef{{Name: "Target Artist"}}},
	}
	got := r.bestArtistMatch("Song", []string{"Target Artist"}, recordings)
	require.NotNil(t, got)
	assert.Equal(t, "Song (Studio)", got.Title)
}

// ---- Enrich / IdentifyTrack integration via fake MB server ----

func newResolverWithServer(t *testing.T, handler http.HandlerFunc) (*Resolver, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewResolver(MBConfig{APIURL: srv.URL, RateLimit: 10000}), srv
}

func TestEnrichFullChain(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording":
			fmt.Fprint(w, `{"recordings":[
				{"id":"rec-1","title":"Song",
				 "artist-credit":[{"name":"Band","artist":{"id":"a-1","country":"GB"}}],
				 "releases":[
					{"id":"rel-1","title":"Album","date":"1999-05-10","status":"Official"},
					{"id":"rel-2","title":"Bootleg","date":"2001-01-01","status":"Bootleg"}
				 ]}
			]}`)
		case "/release/rel-1":
			fmt.Fprint(w, `{"id":"rel-1","country":"GB","tags":[{"name":"rock","count":3}]}`)
		case "/artist/a-1":
			fmt.Fprint(w, `{"id":"a-1","name":"Band","country":"GB","tags":[{"name":"jazz","count":2}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	meta := &AudioMeta{
		Title:   "Song",
		Artist:  "Band",
		Album:   "Album",
		Artists: []string{"Band"},
	}

	result, err := r.Enrich(t.Context(), meta)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "rec-1", result.TrackExternalID)
	assert.Equal(t, "Song", result.Title)
	assert.Equal(t, "a-1", result.ArtistExternalID)
	assert.Equal(t, "GB", result.ArtistCountry)
	assert.Equal(t, "rel-1", result.AlbumExternalID)
	assert.Equal(t, "Album", result.Album)
	assert.Equal(t, 1999, result.Year)
	assert.Equal(t, "rock", result.Genre, "genre from release tags")
	require.Len(t, result.Artists, 1)
	assert.Equal(t, "Band", result.Artists[0].Name)
	assert.Equal(t, "a-1", result.Artists[0].ExternalID)
}

func TestEnrichSkipsNonOfficialReleaseYearFallback(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording":
			fmt.Fprint(w, `{"recordings":[
				{"id":"rec-1","title":"Song",
				 "artist-credit":[{"name":"Band"}],
				 "releases":[
					{"id":"rel-boot","title":"Bootleg","date":"2001-01-01","status":"Bootleg"},
					{"id":"rel-2","title":"Album","date":"1985-06-01","status":""}
				 ]}
			]}`)
		case "/release/rel-boot":
			fmt.Fprint(w, `{"id":"rel-boot","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	result, err := r.Enrich(t.Context(), &AudioMeta{Title: "Song", Artist: "Band"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rel-2", result.AlbumExternalID, "bootleg skipped, unset-status release picked")
	assert.Equal(t, 1985, result.Year)
}

func TestEnrichSecondaryArtistsGetCountry(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording":
			// Recording artist-credit carries only ids, no country.
			fmt.Fprint(w, `{"recordings":[
				{"id":"rec-1","title":"Song",
				 "artist-credit":[
					{"name":"Band A","artist":{"id":"a-1"}},
					{"name":"Band B","artist":{"id":"a-2"}}
				 ],
				 "releases":[]}
			]}`)
		case "/artist/a-1":
			fmt.Fprint(w, `{"id":"a-1","name":"Band A","country":"GB","tags":[{"name":"rock","count":2}]}`)
		case "/artist/a-2":
			fmt.Fprint(w, `{"id":"a-2","name":"Band B","country":"US","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	result, err := r.Enrich(t.Context(), &AudioMeta{
		Title:   "Song",
		Artist:  "Band A",
		Artists: []string{"Band A", "Band B"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Artists, 2)
	assert.Equal(t, "GB", result.Artists[0].Country, "main artist country backfilled")
	assert.Equal(t, "US", result.Artists[1].Country, "secondary artist country backfilled")
	assert.Equal(t, "GB", result.ArtistCountry)
}

func TestEnrichMainArtistLookedUpOnce(t *testing.T) {
	// Without a scan-scoped cache (REST identify/lookup path) the main artist
	// must still be fetched exactly once per call — enrichMainArtist fills it,
	// and fillArtistCountries skips it. A regression that re-looked the main
	// artist would double the artist detail requests.
	var mainHits, secondaryHits int
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording":
			fmt.Fprint(w, `{"recordings":[
				{"id":"rec-1","title":"Song",
				 "artist-credit":[
					{"name":"Band A","artist":{"id":"a-1"}},
					{"name":"Band B","artist":{"id":"a-2"}}
				 ],
				 "releases":[]}
			]}`)
		case "/artist/a-1":
			mainHits++
			fmt.Fprint(w, `{"id":"a-1","name":"Band A","country":"GB","tags":[]}`)
		case "/artist/a-2":
			secondaryHits++
			fmt.Fprint(w, `{"id":"a-2","name":"Band B","country":"US","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	result, err := r.Enrich(t.Context(), &AudioMeta{
		Title:   "Song",
		Artist:  "Band A",
		Artists: []string{"Band A", "Band B"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "GB", result.ArtistCountry)
	require.Len(t, result.Artists, 2)
	assert.Equal(t, "GB", result.Artists[0].Country)
	assert.Equal(t, "US", result.Artists[1].Country)
	assert.Equal(t, 1, mainHits, "main artist looked up once")
	assert.Equal(t, 1, secondaryHits, "secondary artist looked up once")
}

func TestEnrichUsesArtistDetailCache(t *testing.T) {
	var artistHits int
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording":
			fmt.Fprint(w, `{"recordings":[
				{"id":"rec-1","title":"Song",
				 "artist-credit":[{"name":"Band","artist":{"id":"a-1"}}],
				 "releases":[]}
			]}`)
		case "/artist/a-1":
			artistHits++
			fmt.Fprint(w, `{"id":"a-1","name":"Band","country":"GB","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	meta := &AudioMeta{Title: "Song", Artist: "Band", Artists: []string{"Band"}}
	cache := NewArtistDetailCache()
	ctx := WithArtistDetailCache(t.Context(), cache)

	for i := 0; i < 2; i++ {
		result, err := r.Enrich(ctx, meta)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Artists, 1)
		assert.Equal(t, "GB", result.Artists[0].Country)
	}

	assert.Equal(t, 1, artistHits, "two tracks crediting the same artist share one lookup per scan")
}


func TestEnrichEmptyTitle(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	result, err := r.Enrich(t.Context(), &AudioMeta{})
	require.NoError(t, err)
	assert.Nil(t, result, "empty title short-circuits before any request")
}

func TestEnrichGarbledInput(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	result, err := r.Enrich(t.Context(), &AudioMeta{Title: "Bad\xffTitle", Artist: "X"})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnrichNoRecordingResults(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"recordings":[]}`)
	})
	result, err := r.Enrich(t.Context(), &AudioMeta{Title: "Song", Artist: "Band"})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnrichNoMatch(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"recordings":[{"id":"rec-1","title":"Totally Different"}]}`)
	})
	result, err := r.Enrich(t.Context(), &AudioMeta{Title: "My Song", Artist: "Band"})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnrichSearchError(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	result, err := r.Enrich(t.Context(), &AudioMeta{Title: "Song", Artist: "Band"})
	require.NoError(t, err)
	assert.Nil(t, result, "search failure degrades gracefully to nil")
}

func TestIdentifyTrack(t *testing.T) {
	r, _ := newResolverWithServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording/mbid-9":
			fmt.Fprint(w, `{"id":"mbid-9","title":"Track (Version)",
				"artist-credit":[{"name":"Artist","artist":{"id":"a-9","country":"FR"}}],
				"releases":[{"id":"rel-9","title":"LP","date":"2005-03-01","status":"Official"}]}`)
		case "/release/rel-9":
			fmt.Fprint(w, `{"id":"rel-9","tags":[{"name":"pop","count":4}]}`)
		case "/artist/a-9":
			fmt.Fprint(w, `{"id":"a-9","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	result, err := r.IdentifyTrack(t.Context(), "mbid-9")
	require.NoError(t, err)
	assert.Equal(t, "mbid-9", result.TrackExternalID)
	assert.Equal(t, "Track", result.Title, "paren suffix trimmed")
	assert.Equal(t, "Artist", result.Artist)
	assert.Equal(t, "a-9", result.ArtistExternalID)
	assert.Equal(t, "LP", result.Album)
	assert.Equal(t, 2005, result.Year)
	assert.Equal(t, "pop", result.Genre)
}

func TestResolverClose(t *testing.T) {
	r := NewResolver(MBConfig{RateLimit: 10000})
	r.Close() // must not panic
}
