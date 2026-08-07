package rest

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
)

// PlatformHandler exposes external music platform data (charts, search,
// track/artist details) through a provider registry.
type PlatformHandler struct {
	providers map[string]port.PlatformProvider
}

func NewPlatformHandler(providers map[string]port.PlatformProvider) *PlatformHandler {
	return &PlatformHandler{providers: providers}
}

func (h *PlatformHandler) provider(name string) (port.PlatformProvider, bool) {
	p, ok := h.providers[name]
	return p, ok
}

// upstreamError logs the full provider error server-side and returns a
// generic coded message so provider internals never leak to clients.
func (h *PlatformHandler) upstreamError(w http.ResponseWriter, platform, op string, err error) {
	log.Printf("[platform] %s %s failed: %v", platform, op, err)
	writeCodedError(w, http.StatusBadGateway, domain.ErrPlatUpstream)
}

func (h *PlatformHandler) List(w http.ResponseWriter, r *http.Request) {
	type platformItem struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	platforms := make([]platformItem, 0, len(h.providers))
	for name, p := range h.providers {
		platforms = append(platforms, platformItem{Name: name, Label: p.Label()})
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Name < platforms[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"platforms": platforms})
}

func (h *PlatformHandler) ListCharts(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	charts, err := p.ListCharts(ctx)
	if err != nil {
		h.upstreamError(w, name, "list charts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"charts": charts})
}

func (h *PlatformHandler) GetChart(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	chartID := mux.Vars(r)["id"]
	if !validID(chartID) {
		writeCodedError(w, http.StatusBadRequest, domain.ErrPlatInvalidID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	page, limit := pageParams(r)
	tracks, total, err := p.GetChart(ctx, chartID, page, limit)
	if err != nil {
		h.upstreamError(w, name, "get chart "+chartID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tracks": tracks, "total": total, "page": page, "limit": limit,
	})
}

func (h *PlatformHandler) Search(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	page, limit := pageParams(r)
	switch r.URL.Query().Get("type") {
	case "", "track":
		tracks, total, err := p.SearchTracks(ctx, q, page, limit)
		if err != nil {
			h.upstreamError(w, name, "search tracks "+q, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tracks": tracks, "total": total, "page": page, "limit": limit,
		})
	case "artist":
		artists, total, err := p.SearchArtists(ctx, q, page, limit)
		if err != nil {
			h.upstreamError(w, name, "search artists "+q, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"artists": artists, "total": total, "page": page, "limit": limit,
		})
	default:
		writeCodedError(w, http.StatusBadRequest, domain.ErrPlatUnsupportedType)
	}
}

func (h *PlatformHandler) GetTrack(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	trackID := mux.Vars(r)["id"]
	if !validID(trackID) {
		writeCodedError(w, http.StatusBadRequest, domain.ErrPlatInvalidID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	track, err := p.GetTrack(ctx, trackID)
	if err != nil {
		h.upstreamError(w, name, "get track "+trackID, err)
		return
	}
	writeJSON(w, http.StatusOK, track)
}

func (h *PlatformHandler) GetArtist(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	artistID := mux.Vars(r)["id"]
	if !validID(artistID) {
		writeCodedError(w, http.StatusBadRequest, domain.ErrPlatInvalidID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	artist, err := p.GetArtist(ctx, artistID)
	if err != nil {
		h.upstreamError(w, name, "get artist "+artistID, err)
		return
	}
	writeJSON(w, http.StatusOK, artist)
}

func (h *PlatformHandler) GetArtistTracks(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	p, ok := h.provider(name)
	if !ok {
		writeCodedError(w, http.StatusNotFound, domain.ErrPlatUnknownPlatform)
		return
	}
	artistID := mux.Vars(r)["id"]
	if !validID(artistID) {
		writeCodedError(w, http.StatusBadRequest, domain.ErrPlatInvalidID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	page, limit := pageParams(r)
	tracks, total, err := p.GetArtistTracks(ctx, artistID, page, limit)
	if err != nil {
		h.upstreamError(w, name, "get artist tracks "+artistID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tracks": tracks, "total": total, "page": page, "limit": limit,
	})
}

func pageParams(r *http.Request) (page, limit int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 || page > 10000 {
		page = 1
	}
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	return page, limit
}

// validID rejects non-numeric resource IDs before they reach the upstream
// JSON payloads built by providers. NOTE: the numeric-only rule matches the
// current NetEase provider's ID scheme; if a future provider uses
// alphanumeric IDs, this check must move into the provider (or the
// PlatformProvider contract must define a per-provider ID validator).
func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
