package metadata

import (
	"context"
	"log"
	"sort"

	"github.com/sonicore/server/internal/core/port"
)

// Registry coordinates multiple MetadataSource implementations into a single
// identification pipeline. Sources are filtered by Enabled() and tried in
// ascending Priority order, forming a fallback chain.
type Registry struct {
	sources []port.MetadataSource
}

// NewRegistry keeps only enabled sources and sorts them by ascending
// priority (stable).
func NewRegistry(sources ...port.MetadataSource) *Registry {
	enabled := make([]port.MetadataSource, 0, len(sources))
	for _, s := range sources {
		if s != nil && s.Enabled() {
			enabled = append(enabled, s)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		return enabled[i].Priority() < enabled[j].Priority()
	})
	return &Registry{sources: enabled}
}

// Sources returns the enabled sources in priority order. Callers must not
// modify the returned slice.
func (r *Registry) Sources() []port.MetadataSource {
	return r.sources
}

// Identify tries sources in priority order and returns the first confident
// match. A source that errors or yields nil is skipped; the result is nil
// when no source produced a match. When every source failed with an error
// (e.g. a total upstream outage) and none produced a result, the last
// source error is returned so callers can distinguish "no match" from
// "identification unavailable".
func (r *Registry) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	var lastErr error
	for _, s := range r.sources {
		candidate, err := s.Identify(ctx, q)
		if err != nil {
			log.Printf("[metadata] source %s identify error: %v", s.Name(), err)
			lastErr = err
			continue
		}
		if candidate != nil {
			return candidate, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

// SearchCandidates aggregates ranked candidates from every source. A failing
// source is skipped and does not abort the aggregation. When every source
// failed and no candidate was produced, the last source error is returned.
func (r *Registry) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	var out []port.MetadataCandidate
	var lastErr error
	for _, s := range r.sources {
		candidates, err := s.SearchCandidates(ctx, q)
		if err != nil {
			log.Printf("[metadata] source %s search error: %v", s.Name(), err)
			lastErr = err
			continue
		}
		out = append(out, candidates...)
	}
	if len(out) == 0 && lastErr != nil {
		return out, lastErr
	}
	return out, nil
}

// Lookup resolves an external ID through the source that owns it (matched by
// source name). Unknown sources yield a nil candidate.
func (r *Registry) Lookup(ctx context.Context, source, externalID string) (*port.MetadataCandidate, error) {
	for _, s := range r.sources {
		if s.Name() == source {
			return s.Lookup(ctx, externalID)
		}
	}
	return nil, nil
}
