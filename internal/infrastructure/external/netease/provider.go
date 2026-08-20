package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sonicore/server/internal/core/port"
)

// Provider implements port.PlatformProvider for NetEase Cloud Music using
// anonymous access to the private API (weapi/eapi schemes).
const platformName = "netease"

// ErrTrackNotFound is the sentinel returned when a track detail lookup finds
// no matching track. Sources classify this as a clean "no match" instead of
// an upstream failure.
var ErrTrackNotFound = fmt.Errorf("track not found")

// ErrAlbumNotFound is returned when an album detail lookup finds no matching
// album. It allows callers to distinguish "not found" from network errors.
var ErrAlbumNotFound = fmt.Errorf("album not found")

// AlbumDetail holds metadata for a single album retrieved from the NetEase
// API. It is used by the metadata resolution chain to fill in album-level
// fields (artist, year) when saving a track identified via NetEase.
type AlbumDetail struct {
	ID     string
	Title  string
	Artist string
	Year   int
}

type Provider struct {
	client *Client
}

func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) Name() string { return platformName }

func (p *Provider) Label() string { return "网易云音乐" }

// SetCookieProvider forwards the runtime cookie source to the underlying
// client.
func (p *Provider) SetCookieProvider(f func() string) {
	p.client.SetCookieProvider(f)
}

// SetRateLimitProvider forwards the runtime requests-per-second source to the
// underlying client (admin setting with a config fallback).
func (p *Provider) SetRateLimitProvider(f func() int) {
	p.client.SetRateLimitProvider(f)
}

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

func (p *Provider) SearchAlbums(ctx context.Context, query string, page, limit int) ([]map[string]any, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}
	body, err := p.request(ctx, "/api/search/get", map[string]any{
		"s":      query,
		"type":   10,
		"limit":  limit,
		"offset": offset,
	}, "eapi")
	if err != nil {
		return nil, 0, err
	}
	result, _ := body["result"].(map[string]any)
	albums, _ := result["albums"].([]any)
	out := make([]map[string]any, 0, len(albums))
	for _, item := range albums {
		if obj, ok := item.(map[string]any); ok {
			artistName := ""
			if artists, ok := obj["artists"].([]any); ok && len(artists) > 0 {
				if first, ok := artists[0].(map[string]any); ok {
					artistName = asString(first["name"])
				}
			}
			out = append(out, map[string]any{
				"title":       asString(obj["name"]),
				"external_id": idString(obj["id"]),
				"artist":      artistName,
			})
		}
	}
	total := int(asFloat64(result["albumCount"]))
	return out, total, nil
}

func (p *Provider) GetTrack(ctx context.Context, trackID string) (*port.TrackDetail, error) {
	tracks, err := p.songDetails(ctx, []string{trackID})
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
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
		Artists:  tracks[0].Artists,
		Album:    tracks[0].Album,
		AlbumID:  tracks[0].AlbumID,
		Duration: tracks[0].Duration,
		CoverURL: tracks[0].CoverURL,
	}

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
		"id":            artistID,
		"private_cloud": "true",
		"work_type":     1,
		"order":         "hot",
		"offset":        offset,
		"limit":         limit,
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

// GetAlbum retrieves album metadata from the NetEase API. It returns the
// album title, the first credited artist, and the release year extracted from
// the album's publishTime.
func (p *Provider) GetAlbum(ctx context.Context, albumID string) (*AlbumDetail, error) {
	body, err := p.request(ctx, "/api/album/v3/detail",
		map[string]any{"id": albumID}, "weapi")
	if err != nil {
		return nil, err
	}
	album, _ := body["album"].(map[string]any)
	if album == nil {
		return nil, fmt.Errorf("%w: %s", ErrAlbumNotFound, albumID)
	}
	detail := &AlbumDetail{
		ID:    albumID,
		Title: asString(album["name"]),
	}
	if artists, ok := album["artists"].([]any); ok && len(artists) > 0 {
		if first, ok := artists[0].(map[string]any); ok {
			detail.Artist = asString(first["name"])
		}
	}
	if pt, ok := toFloat64(album["publishTime"]); ok && pt > 0 {
		detail.Year = time.UnixMilli(int64(pt)).UTC().Year()
	}
	return detail, nil
}

// mapTrack converts a NetEase song object to a PlatformTrack. Search
// responses use "artists"/"album", detail responses use "ar"/"al". All
// credited artists are collected (Artist is their comma-joined names,
// ArtistID the first) so multi-artist tracks survive identification and
// scoring.
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
	names := make([]string, 0, len(ars))
	for _, item := range ars {
		ar, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := asString(ar["name"])
		if name == "" {
			continue
		}
		ai := port.ArtistInfo{Name: name, ExternalID: idString(ar["id"])}
		if t.Artists == nil {
			t.Artists = []port.ArtistInfo{}
		}
		t.Artists = append(t.Artists, ai)
		names = append(names, name)
	}
	if len(t.Artists) > 0 {
		t.Artist = strings.Join(names, ",")
		t.ArtistID = t.Artists[0].ExternalID
	}

	al, ok := s["al"].(map[string]any)
	if !ok {
		al, _ = s["album"].(map[string]any)
	}
	if len(al) > 0 {
		t.Album = asString(al["name"])
		t.AlbumID = idString(al["id"])
		// Detail responses carry the real cover URL; search responses only
		// expose picId, so the cover stays empty there and is filled in by
		// EnrichTracks' batch detail lookup.
		if pic := asString(al["picUrl"]); pic != "" {
			t.CoverURL = pic
		}
	}
	return t
}

// EnrichTracks fills in the fields search responses omit — the real album
// cover URL and the full artist list — via one batch song-detail request.
// Tracks whose id is not returned by the API are left untouched.
func (p *Provider) EnrichTracks(ctx context.Context, tracks []port.PlatformTrack) ([]port.PlatformTrack, error) {
	if len(tracks) == 0 {
		return tracks, nil
	}
	ids := make([]string, 0, len(tracks))
	for _, t := range tracks {
		if t.TrackID != "" {
			ids = append(ids, t.TrackID)
		}
	}
	if len(ids) == 0 {
		return tracks, nil
	}
	details, err := p.songDetails(ctx, ids)
	if err != nil {
		return tracks, err
	}
	byID := make(map[string]port.PlatformTrack, len(details))
	for _, d := range details {
		byID[d.TrackID] = d
	}
	out := make([]port.PlatformTrack, len(tracks))
	copy(out, tracks)
	for i := range out {
		d, ok := byID[out[i].TrackID]
		if !ok {
			continue
		}
		if d.CoverURL != "" {
			out[i].CoverURL = d.CoverURL
		}
		if len(d.Artists) > 0 {
			out[i].Artists = d.Artists
			out[i].Artist = d.Artist
			out[i].ArtistID = d.ArtistID
		}
		if d.Album != "" {
			out[i].Album = d.Album
			out[i].AlbumID = d.AlbumID
		}
		if d.Duration > 0 {
			out[i].Duration = d.Duration
		}
	}
	return out, nil
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
