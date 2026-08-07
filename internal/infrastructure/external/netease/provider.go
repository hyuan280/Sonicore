package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sonicore/server/internal/core/port"
)

// Provider implements port.PlatformProvider for NetEase Cloud Music using
// anonymous access to the private API (weapi/eapi schemes).
const platformName = "netease"

type Provider struct {
	client *Client
}

func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) Name() string { return platformName }

func (p *Provider) Label() string { return "网易云音乐" }

func (p *Provider) request(ctx context.Context, uri string, data map[string]any, mode string) (map[string]any, error) {
	resp, err := p.client.request(ctx, uri, data, mode, true)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

// ListCharts returns all NetEase toplists ("云音乐飙升榜" etc).
func (p *Provider) ListCharts(ctx context.Context) ([]port.Chart, error) {
	body, err := p.request(ctx, "/api/toplist", map[string]any{}, "eapi")
	if err != nil {
		return nil, err
	}
	list, _ := body["list"].([]any)
	charts := make([]port.Chart, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		charts = append(charts, port.Chart{
			ID:          idString(obj["id"]),
			Name:        asString(obj["name"]),
			Description: asString(obj["description"]),
			CoverURL:    asString(obj["coverImgUrl"]),
			TrackCount:  int(asFloat64(obj["trackCount"])),
			UpdateFreq:  asString(obj["updateFrequency"]),
		})
	}
	return charts, nil
}

// GetChart returns a slice of a toplist's tracks. NetEase toplist IDs are
// playlist IDs, so we resolve via playlist detail + song detail.
func (p *Provider) GetChart(ctx context.Context, chartID string, page, limit int) ([]port.PlatformTrack, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	body, err := p.request(ctx, "/api/v6/playlist/detail",
		map[string]any{"id": chartID, "n": 100000, "s": 8}, "eapi")
	if err != nil {
		return nil, 0, err
	}
	playlist, _ := body["playlist"].(map[string]any)
	trackIDs := trackIDsOf(playlist["trackIds"])
	total := len(trackIDs)

	if offset >= total {
		return []port.PlatformTrack{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	if offset == end {
		return []port.PlatformTrack{}, total, nil
	}

	tracks, err := p.songDetails(ctx, trackIDs[offset:end])
	if err != nil {
		return nil, total, err
	}
	return tracks, total, nil
}

func trackIDsOf(v any) []string {
	raw, _ := v.([]any)
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := idString(obj["id"])
		if id != "" && id != "0" {
			ids = append(ids, id)
		}
	}
	return ids
}

// songDetails fetches full track objects for a batch of IDs. Non-numeric
// IDs are skipped to avoid corrupting the JSON payload sent upstream.
func (p *Provider) songDetails(ctx context.Context, ids []string) ([]port.PlatformTrack, error) {
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if isNumericID(id) {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return []port.PlatformTrack{}, nil
	}
	c := make([]string, len(valid))
	for i, id := range valid {
		c[i] = `{"id":` + id + `}`
	}
	body, err := p.request(ctx, "/api/v3/song/detail",
		map[string]any{"c": "[" + strings.Join(c, ",") + "]"}, "weapi")
	if err != nil {
		return nil, err
	}
	songs, _ := body["songs"].([]any)
	tracks := make([]port.PlatformTrack, 0, len(songs))
	for _, item := range songs {
		if obj, ok := item.(map[string]any); ok {
			tracks = append(tracks, mapTrack(obj))
		}
	}
	return tracks, nil
}

func (p *Provider) SearchTracks(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	body, err := p.request(ctx, "/api/search/get", map[string]any{
		"s":      query,
		"type":   1,
		"limit":  limit,
		"offset": offset,
	}, "eapi")
	if err != nil {
		return nil, 0, err
	}
	result, _ := body["result"].(map[string]any)
	songs, _ := result["songs"].([]any)
	tracks := make([]port.PlatformTrack, 0, len(songs))
	for _, item := range songs {
		if obj, ok := item.(map[string]any); ok {
			tracks = append(tracks, mapTrack(obj))
		}
	}
	total := int(asFloat64(result["songCount"]))
	return tracks, total, nil
}

func (p *Provider) SearchArtists(ctx context.Context, query string, page, limit int) ([]port.ArtistDetail, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	body, err := p.request(ctx, "/api/search/get", map[string]any{
		"s":      query,
		"type":   100,
		"limit":  limit,
		"offset": offset,
	}, "eapi")
	if err != nil {
		return nil, 0, err
	}
	result, _ := body["result"].(map[string]any)
	artists, _ := result["artists"].([]any)
	out := make([]port.ArtistDetail, 0, len(artists))
	for _, item := range artists {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, mapArtistBrief(obj))
		}
	}
	total := int(asFloat64(result["artistCount"]))
	return out, total, nil
}

func (p *Provider) GetTrack(ctx context.Context, trackID string) (*port.TrackDetail, error) {
	tracks, err := p.songDetails(ctx, []string{trackID})
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("track not found: %s", trackID)
	}

	body, err := p.request(ctx, "/api/song/lyric", map[string]any{
		"id": trackID, "tv": -1, "lv": -1, "rv": -1, "kv": -1, "_nmclfl": 1,
	}, "eapi")
	if err != nil {
		return nil, err
	}

	detail := &port.TrackDetail{
		Platform: p.Name(),
		TrackID:  tracks[0].TrackID,
		Title:    tracks[0].Title,
		Artist:   tracks[0].Artist,
		ArtistID: tracks[0].ArtistID,
		Album:    tracks[0].Album,
		AlbumID:  tracks[0].AlbumID,
		Duration: tracks[0].Duration,
		CoverURL: tracks[0].CoverURL,
	}

	// Enrich with publish time / album year when available.
	if obj, ok := body["lrc"].(map[string]any); ok {
		detail.Lyrics = asString(obj["lyric"])
	}
	if obj, ok := body["tlyric"].(map[string]any); ok {
		detail.LyricsTrans = asString(obj["lyric"])
	}

	return detail, nil
}

func (p *Provider) GetArtist(ctx context.Context, artistID string) (*port.ArtistDetail, error) {
	body, err := p.request(ctx, "/api/artist/head/info/get",
		map[string]any{"id": artistID}, "eapi")
	if err != nil {
		return nil, err
	}
	data, _ := body["data"].(map[string]any)
	artist, _ := data["artist"].(map[string]any)
	if len(artist) == 0 {
		return nil, fmt.Errorf("artist not found: %s", artistID)
	}
	return &port.ArtistDetail{
		Platform:   p.Name(),
		ArtistID:   artistID,
		Name:       asString(artist["name"]),
		CoverURL:   asString(artist["cover"]),
		AlbumCount: int(asFloat64(artist["albumSize"])),
		TrackCount: int(asFloat64(artist["musicSize"])),
		BriefDesc:  asString(artist["briefDesc"]),
	}, nil
}

func (p *Provider) GetArtistTracks(ctx context.Context, artistID string, page, limit int) ([]port.PlatformTrack, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	body, err := p.request(ctx, "/api/v1/artist/songs", map[string]any{
		"id":           artistID,
		"private_cloud": "true",
		"work_type":    1,
		"order":        "hot",
		"offset":       offset,
		"limit":        limit,
	}, "eapi")
	if err != nil {
		return nil, 0, err
	}
	songs, _ := body["songs"].([]any)
	tracks := make([]port.PlatformTrack, 0, len(songs))
	for _, item := range songs {
		if obj, ok := item.(map[string]any); ok {
			tracks = append(tracks, mapTrack(obj))
		}
	}
	total := int(asFloat64(body["total"]))
	return tracks, total, nil
}

// mapTrack converts a NetEase song object to a PlatformTrack. Search
// responses use "artists"/"album", detail responses use "ar"/"al".
func mapTrack(s map[string]any) port.PlatformTrack {
	t := port.PlatformTrack{
		Platform: platformName,
		TrackID:  idString(s["id"]),
		Title:    asString(s["name"]),
	}
	if d := s["dt"]; d != nil {
		t.Duration = asFloat64(d) / 1000
	} else {
		t.Duration = asFloat64(s["duration"]) / 1000
	}

	ars, _ := s["ar"].([]any)
	if len(ars) == 0 {
		ars, _ = s["artists"].([]any)
	}
	if len(ars) > 0 {
		if ar, ok := ars[0].(map[string]any); ok {
			t.Artist = asString(ar["name"])
			t.ArtistID = idString(ar["id"])
			if t.CoverURL == "" {
				t.CoverURL = asString(ar["img1v1Url"])
			}
		}
	}

	al, ok := s["al"].(map[string]any)
	if !ok {
		al, _ = s["album"].(map[string]any)
	}
	if len(al) > 0 {
		t.Album = asString(al["name"])
		t.AlbumID = idString(al["id"])
		if pic := asString(al["picUrl"]); pic != "" {
			t.CoverURL = pic
		}
	}
	return t
}

func mapArtistBrief(a map[string]any) port.ArtistDetail {
	out := port.ArtistDetail{
		Platform: platformName,
		ArtistID: idString(a["id"]),
		Name:     asString(a["name"]),
	}
	if pic, ok := a["picUrl"].(string); ok {
		out.CoverURL = pic
	}
	if ints, ok := a["img1v1Url"].(string); ok && out.CoverURL == "" {
		out.CoverURL = ints
	}
	return out
}

// idString formats an API id exactly (they exceed float64 precision when
// printed with %v).
func idString(v any) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case string:
		return t
	}
	return ""
}

// isNumericID reports whether s is a positive integer, as expected for
// NetEase resource IDs.
func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
