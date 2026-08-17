package metadata

import (
	"context"
	"errors"
	"testing"

	"github.com/sonicore/server/internal/core/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource is a scriptable port.MetadataSource for registry tests.
type fakeSource struct {
	name           string
	enabled        bool
	priority       int
	caps           port.MetadataFields
	identify       func(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error)
	search         func(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error)
	lookup         func(ctx context.Context, id string) (*port.MetadataCandidate, error)
	identifyCalled int
	lookupCalled   int
}

func (f *fakeSource) Name() string  { return f.name }
func (f *fakeSource) Enabled() bool { return f.enabled }
func (f *fakeSource) Priority() int { return f.priority }
func (f *fakeSource) Capabilities() port.MetadataFields {
	if f.caps == 0 {
		return port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum |
			port.FieldYear | port.FieldGenre
	}
	return f.caps
}
func (f *fakeSource) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	f.identifyCalled++
	if f.identify == nil {
		return nil, nil
	}
	return f.identify(ctx, q)
}
func (f *fakeSource) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	if f.search == nil {
		return nil, nil
	}
	return f.search(ctx, q)
}
func (f *fakeSource) Lookup(ctx context.Context, id string) (*port.MetadataCandidate, error) {
	f.lookupCalled++
	if f.lookup == nil {
		return nil, nil
	}
	return f.lookup(ctx, id)
}

// fullCand builds a candidate carrying every field.
func fullCand(source, id, title string) *port.MetadataCandidate {
	return &port.MetadataCandidate{
		Source:     source,
		ExternalID: id,
		Title:      title,
		Artists:    []port.ArtistInfo{{Name: "Artist"}},
		Album:      "Album",
		CoverArtURL: "https://cover",
		Lyrics:     "lyrics",
		Score:      1.0,
	}
}

func cand(source, id string) *port.MetadataCandidate {
	return &port.MetadataCandidate{Source: source, ExternalID: id, Score: 1.0}
}

func TestRegistryFiltersDisabledAndSortsByPriority(t *testing.T) {
	low := &fakeSource{name: "low", enabled: true, priority: 5}
	high := &fakeSource{name: "high", enabled: true, priority: 20}
	off := &fakeSource{name: "off", enabled: false, priority: 1}
	dupA := &fakeSource{name: "dupa", enabled: true, priority: 10}
	dupB := &fakeSource{name: "dupb", enabled: true, priority: 10}

	r := NewRegistry(high, off, dupB, low, dupA)
	sources := r.Sources()
	require.Len(t, sources, 4)
	assert.Equal(t, []string{"low", "dupb", "dupa", "high"}, sourceNames(sources))

	assert.Empty(t, NewRegistry(nil, off).Sources())
}

func sourceNames(sources []port.MetadataSource) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.Name()
	}
	return out
}

// TestRegistryIdentifyFirstSourceCompleteStops: the first source carrying
// every target field ends the chain — later sources are never consulted.
func TestRegistryIdentifyFirstSourceCompleteStops(t *testing.T) {
	mb := &fakeSource{name: "mb", enabled: true, priority: 10}
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	mb.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("mb", "m-1", "Song"), nil
	}

	got, err := NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "mb", got.Source)
	assert.Equal(t, 1, mb.identifyCalled)
	assert.Equal(t, 0, ne.identifyCalled, "second source not consulted when fields are complete")
}

// TestRegistryIdentifyFillsMissingFields: MB lacks cover/lyrics; NetEase
// fills them without touching the identity fields.
func TestRegistryIdentifyFillsMissingFields(t *testing.T) {
	mb := &fakeSource{name: "mb", enabled: true, priority: 10}
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	mb.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("mb", "m-1", "缘生意转")
		c.CoverArtURL = ""
		c.Lyrics = ""
		return c, nil
	}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("ne", "n-1", "缘生意转"), nil
	}

	got, err := NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{Title: "缘生意转"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "mb", got.Source, "identity from first source")
	assert.Equal(t, "m-1", got.ExternalID, "identity external id not overwritten")
	assert.Equal(t, "https://cover", got.CoverArtURL, "cover filled by netease")
	assert.Equal(t, "lyrics", got.Lyrics, "lyrics filled by netease")
}

// TestRegistryIdentifyYearGenreNotCompletionGoals: a source whose
// capabilities only cover Year/Genre is never consulted, because those are
// not completion targets.
func TestRegistryIdentifyYearGenreNotCompletionGoals(t *testing.T) {
	first := &fakeSource{name: "first", enabled: true, priority: 10}
	second := &fakeSource{name: "second", enabled: true, priority: 20,
		caps: port.FieldYear | port.FieldGenre}
	first.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("first", "f-1", "Song")
		c.CoverArtURL = ""
		c.Lyrics = ""
		return c, nil
	}

	got, err := NewRegistry(first, second).Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0, second.identifyCalled, "Year/Genre absence does not trigger the next source")
	assert.Equal(t, 0, got.Year, "year stays unfilled")
}

// TestRegistryIdentifyLookupFillsLyrics: the source declares lyrics
// capability but the search candidate lacks it — one Lookup fetches it.
func TestRegistryIdentifyLookupFillsLyrics(t *testing.T) {
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("ne", "n-1", "Song")
		c.Lyrics = ""
		return c, nil
	}
	ne.lookup = func(context.Context, string) (*port.MetadataCandidate, error) {
		c := fullCand("ne", "n-1", "Song")
		return c, nil
	}

	got, err := NewRegistry(ne).Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, ne.lookupCalled, "lookup called once to fetch lyrics")
	assert.Equal(t, "lyrics", got.Lyrics)
}

// TestRegistryIdentifyNoLookupWhenSearchHasLyrics: when the first search
// already returns lyrics, no extra Lookup happens.
func TestRegistryIdentifyNoLookupWhenSearchHasLyrics(t *testing.T) {
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("ne", "n-1", "Song"), nil
	}

	got, err := NewRegistry(ne).Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0, ne.lookupCalled, "no lookup when lyrics already present")
}

// TestRegistryIdentifyIncompatibleSkipped: a candidate for a different song
// is skipped entirely, nothing is merged.
func TestRegistryIdentifyIncompatibleSkipped(t *testing.T) {
	mb := &fakeSource{name: "mb", enabled: true, priority: 10}
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	mb.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("mb", "m-1", "缘生意转")
		c.CoverArtURL = ""
		c.Lyrics = ""
		return c, nil
	}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("ne", "n-2", "完全不同的歌")
		c.Artists = []port.ArtistInfo{{Name: "别的艺人"}}
		return c, nil
	}

	got, err := NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{Title: "缘生意转"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "", got.CoverArtURL, "incompatible source contributes nothing")
	assert.Equal(t, "", got.Lyrics)
	assert.Equal(t, "m-1", got.ExternalID)
}

// TestRegistryIdentifyNoCapabilityIntersectionSkipped: the second source is
// only consulted when its capabilities intersect the missing fields.
func TestRegistryIdentifyNoCapabilityIntersectionSkipped(t *testing.T) {
	mb := &fakeSource{name: "mb", enabled: true, priority: 10}
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle} // no cover/lyrics capability
	mb.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("mb", "m-1", "Song")
		c.CoverArtURL = ""
		c.Lyrics = ""
		return c, nil
	}

	got, err := NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0, ne.identifyCalled, "source without matching capabilities not consulted")
}

// TestRegistryIdentifyFileFieldsPresent: fields the file already carries
// (embedded/sidecar lyrics, embedded cover) count as present, so the capable
// source is never consulted for them.
func TestRegistryIdentifyFileFieldsPresent(t *testing.T) {
	mb := &fakeSource{name: "mb", enabled: true, priority: 10}
	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	mb.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("mb", "m-1", "Song")
		c.CoverArtURL = ""
		c.Lyrics = ""
		return c, nil
	}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("ne", "n-1", "Song"), nil
	}

	// Both lyrics and cover come from the file → chain stops at MB.
	got, err := NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{
		Title: "Song", FileFields: port.FileFieldLyrics | port.FileFieldCover,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0, ne.identifyCalled, "netease not consulted when lyrics/cover come from the file")

	// Only lyrics from the file → cover still a goal, netease consulted.
	got, err = NewRegistry(mb, ne).Identify(context.Background(), port.MetadataQuery{
		Title: "Song", FileFields: port.FileFieldLyrics,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, ne.identifyCalled, "cover still triggers the completion source")
	assert.Equal(t, "https://cover", got.CoverArtURL)
}

// TestRegistryIdentifyFallbackToNextSource: when the first source has no
// match, the next one becomes the identity source.
func TestRegistryIdentifyFallbackToNextSource(t *testing.T) {
	low := &fakeSource{name: "low", enabled: true, priority: 5}
	high := &fakeSource{name: "high", enabled: true, priority: 10}
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, nil
	}
	high.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("high", "h-1", "Song"), nil
	}

	got, err := NewRegistry(low, high).Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "high", got.Source)
}

// TestRegistryIdentifyFirstSourceErrorFallsThrough: an erroring source is
// skipped, later sources still consulted.
func TestRegistryIdentifyFirstSourceErrorFallsThrough(t *testing.T) {
	low := &fakeSource{name: "low", enabled: true, priority: 5}
	high := &fakeSource{name: "high", enabled: true, priority: 10}
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, errors.New("upstream down")
	}
	high.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return fullCand("high", "h-1", "Song"), nil
	}

	got, err := NewRegistry(low, high).Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "high", got.Source)
}

func TestRegistryIdentifyAllSourcesFail(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}
	a.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, errors.New("upstream down")
	}
	b.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, errors.New("rate limited")
	}

	got, err := NewRegistry(a, b).Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited", "last source error is surfaced")
}

func TestRegistryIdentifyNoMatches(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}
	a.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) { return nil, nil }
	b.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) { return nil, nil }

	got, err := NewRegistry(a, b).Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRegistrySearchCandidatesAllSourcesFail(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}
	a.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return nil, errors.New("upstream down")
	}
	b.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return nil, errors.New("rate limited")
	}

	got, err := NewRegistry(a, b).SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
	require.Empty(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestRegistrySearchCandidatesPartialFailureKeepsResults(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}
	a.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return []port.MetadataCandidate{*cand("a", "a-1")}, nil
	}
	b.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return nil, errors.New("rate limited")
	}

	got, err := NewRegistry(a, b).SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
	require.Len(t, got, 1)
	require.NoError(t, err, "results from a healthy source suppress the error")
}

func TestRegistrySearchCandidatesAggregatesAndSkipsErrors(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}

	a.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return []port.MetadataCandidate{*cand("a", "a-1"), *cand("a", "a-2")}, nil
	}
	b.search = func(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
		return nil, errors.New("rate limited")
	}

	cands, err := r_search(t, a, b)
	require.NoError(t, err)
	require.Len(t, cands, 2)
	assert.Equal(t, "a-1", cands[0].ExternalID)
	assert.Equal(t, "a-2", cands[1].ExternalID)
}

func TestRegistryLookupRoutesBySource(t *testing.T) {
	a := &fakeSource{name: "a", enabled: true, priority: 1}
	b := &fakeSource{name: "b", enabled: true, priority: 2}
	b.lookup = func(context.Context, string) (*port.MetadataCandidate, error) {
		return cand("b", "b-9"), nil
	}

	r := NewRegistry(a, b)
	got, err := r.Lookup(context.Background(), "b", "b-9")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "b", got.Source)

	got, err = r.Lookup(context.Background(), "ghost", "x")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func r_search(t *testing.T, sources ...port.MetadataSource) ([]port.MetadataCandidate, error) {
	t.Helper()
	return NewRegistry(sources...).SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
}
