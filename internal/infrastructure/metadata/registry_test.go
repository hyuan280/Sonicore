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
	name     string
	enabled  bool
	priority int
	identify func(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error)
	search   func(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error)
	lookup   func(ctx context.Context, id string) (*port.MetadataCandidate, error)
}

func (f *fakeSource) Name() string                { return f.name }
func (f *fakeSource) Enabled() bool               { return f.enabled }
func (f *fakeSource) Priority() int               { return f.priority }
func (f *fakeSource) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
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
	if f.lookup == nil {
		return nil, nil
	}
	return f.lookup(ctx, id)
}

func cand(source, id string) *port.MetadataCandidate {
	return &port.MetadataCandidate{Source: source, ExternalID: id, Score: 1.0}
}

func TestRegistryFiltersDisabledAndSortsByPriority(t *testing.T) {
	low := &fakeSource{name: "low", enabled: true, priority: 5}
	high := &fakeSource{name: "high", enabled: true, priority: 20}
	off := &fakeSource{name: "off", enabled: false, priority: 1}
	// duplicate priority keeps stable order
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

func TestRegistryIdentifyFallbackChain(t *testing.T) {
	high := &fakeSource{name: "high", enabled: true, priority: 20}
	low := &fakeSource{name: "low", enabled: true, priority: 5}

	r := NewRegistry(low, high)

	// First source has no match -> falls through to next.
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, nil
	}
	high.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return cand("high", "h-1"), nil
	}
	got, err := r.Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "high", got.Source)

	// First source errors -> skipped, second still consulted.
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, errors.New("upstream down")
	}
	got, err = r.Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "high", got.Source)

	// First source wins on priority.
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return cand("low", "l-1"), nil
	}
	got, err = r.Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "low", got.Source)

	// No matches anywhere -> nil, nil.
	low.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, nil
	}
	high.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		return nil, nil
	}
	got, err = r.Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	assert.Nil(t, got)
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

	r := NewRegistry(a, b)
	got, err := r.Identify(context.Background(), port.MetadataQuery{Title: "X"})
	require.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited", "last source error is surfaced")
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

	r := NewRegistry(a, b)
	got, err := r.SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
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

	r := NewRegistry(a, b)
	got, err := r.SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
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

	// Unknown source -> nil, no error.
	got, err = r.Lookup(context.Background(), "ghost", "x")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func r_search(t *testing.T, sources ...port.MetadataSource) ([]port.MetadataCandidate, error) {
	t.Helper()
	return NewRegistry(sources...).SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
}
