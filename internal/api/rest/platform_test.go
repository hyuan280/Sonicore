package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/core/port"
	"github.com/stretchr/testify/assert"
)

// stubProvider is a scriptable PlatformProvider for handler tests.
type stubProvider struct {
	name        string
	chartsErr   bool
	trackErr    bool
	trackDetail *port.TrackDetail
}

func (p *stubProvider) Name() string  { return p.name }
func (p *stubProvider) Label() string { return "Stub " + p.name }

func (p *stubProvider) ListCharts(ctx context.Context) ([]port.Chart, error) {
	if p.chartsErr {
		return nil, errors.New("upstream exploded")
	}
	return []port.Chart{{ID: "1", Name: "Top", TrackCount: 100}}, nil
}

func (p *stubProvider) GetChart(ctx context.Context, chartID string, page, limit int) ([]port.PlatformTrack, int, error) {
	return []port.PlatformTrack{{TrackID: "1", Title: "T"}}, 42, nil
}

func (p *stubProvider) SearchTracks(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
	return []port.PlatformTrack{{TrackID: "1", Title: query}}, 7, nil
}

func (p *stubProvider) SearchArtists(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
	return []port.ArtistDetail{{ArtistID: "1", Name: query}}, 3, nil
}

func (p *stubProvider) GetTrack(ctx context.Context, trackID string) (*port.TrackDetail, error) {
	if p.trackErr {
		return nil, errors.New("track gone")
	}
	return &port.TrackDetail{TrackID: trackID, Title: "Detail", Platform: p.name}, nil
}

func (p *stubProvider) GetArtist(ctx context.Context, artistID string) (*port.ArtistDetail, error) {
	return &port.ArtistDetail{ArtistID: artistID, Name: "Band"}, nil
}

func (p *stubProvider) GetArtistTracks(ctx context.Context, artistID string, page, limit int) ([]port.PlatformTrack, int, error) {
	return []port.PlatformTrack{{TrackID: "9"}}, 2, nil
}

func newPlatformHandler() *PlatformHandler {
	return NewPlatformHandler(map[string]port.PlatformProvider{
		"netease": &stubProvider{name: "netease"},
		"stub2":   &stubProvider{name: "stub2"},
	})
}

func platformRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	return req
}

func TestValidID(t *testing.T) {
	assert.True(t, validID("123"))
	assert.True(t, validID("0"))
	assert.False(t, validID(""))
	assert.False(t, validID("12a"))
	assert.False(t, validID("abc"))
	assert.False(t, validID("12-34"))
}

func TestPageParams(t *testing.T) {
	r := platformRequest(http.MethodGet, "/?page=2&limit=10")
	page, limit := pageParams(r)
	assert.Equal(t, 2, page)
	assert.Equal(t, 10, limit)

	r = platformRequest(http.MethodGet, "/?page=0&limit=0")
	page, limit = pageParams(r)
	assert.Equal(t, 1, page, "page < 1 clamps to 1")
	assert.Equal(t, 30, limit, "limit < 1 clamps to 30")

	r = platformRequest(http.MethodGet, "/?page=99999&limit=999")
	page, limit = pageParams(r)
	assert.Equal(t, 1, page, "page > 10000 clamps")
	assert.Equal(t, 30, limit, "limit > 100 clamps")

	r = platformRequest(http.MethodGet, "/?page=abc&limit=xyz")
	page, limit = pageParams(r)
	assert.Equal(t, 1, page)
	assert.Equal(t, 30, limit)
}

func TestPlatformListSorted(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	h.List(rec, platformRequest(http.MethodGet, "/api/platforms"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"netease"`)
	assert.Contains(t, rec.Body.String(), `"name":"stub2"`)
	assert.True(t, indexOf(rec.Body.String(), "netease") < indexOf(rec.Body.String(), "stub2"), "sorted by name")
}

func TestPlatformListCharts(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/charts"), map[string]string{"name": "netease"})
	h.ListCharts(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Top"`)
}

func TestPlatformUnknownProvider(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/nope/charts"), map[string]string{"name": "nope"})
	h.ListCharts(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":700`)
}

func TestPlatformUpstreamError(t *testing.T) {
	h := newPlatformHandler()
	h.providers["broken"] = &stubProvider{name: "broken", chartsErr: true}

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/broken/charts"), map[string]string{"name": "broken"})
	h.ListCharts(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":703`)
	assert.NotContains(t, rec.Body.String(), "upstream exploded", "provider internals never leak")
}

func TestPlatformGetChartInvalidID(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/charts/abc"),
		map[string]string{"name": "netease", "id": "abc"})
	h.GetChart(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":701`)
}

func TestPlatformGetChartSuccess(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/charts/3778678?page=1&limit=10"),
		map[string]string{"name": "netease", "id": "3778678"})
	h.GetChart(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"total":42`)
	assert.Contains(t, rec.Body.String(), `"page":1`)
}

func TestPlatformSearchMissingQuery(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/search"), map[string]string{"name": "netease"})
	h.Search(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPlatformSearchTracks(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/search?q=hello&type=track"),
		map[string]string{"name": "netease"})
	h.Search(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"hello"`)
	assert.Contains(t, rec.Body.String(), `"total":7`)
}

func TestPlatformSearchArtists(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/search?q=band&type=artist"),
		map[string]string{"name": "netease"})
	h.Search(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"band"`)
}

func TestPlatformSearchUnsupportedType(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/search?q=x&type=album"),
		map[string]string{"name": "netease"})
	h.Search(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":702`)
}

func TestPlatformGetTrack(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/tracks/123"),
		map[string]string{"name": "netease", "id": "123"})
	h.GetTrack(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Detail"`)
}

func TestPlatformGetTrackUpstreamError(t *testing.T) {
	h := newPlatformHandler()
	h.providers["broken"] = &stubProvider{name: "broken", trackErr: true}

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/broken/tracks/1"),
		map[string]string{"name": "broken", "id": "1"})
	h.GetTrack(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":703`)
}

func TestPlatformGetArtistTracks(t *testing.T) {
	h := newPlatformHandler()

	rec := httptest.NewRecorder()
	req := mux.SetURLVars(platformRequest(http.MethodGet, "/api/platforms/netease/artists/5/tracks"),
		map[string]string{"name": "netease", "id": "5"})
	h.GetArtistTracks(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"total":2`)
}

func indexOf(s, sub string) int {
	return strings.Index(s, sub)
}
