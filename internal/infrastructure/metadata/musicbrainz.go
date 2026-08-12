package metadata

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type MBClient struct {
	http           *http.Client
	base           string
	appName        string
	appVer         string
	rateLimitPerSec int // requests per second
	lastReq        time.Time
	mu             sync.Mutex
}

type MBConfig struct {
	Enabled   bool
	APIURL    string
	RateLimit int
	AppName   string
	AppVer    string
}

func NewMBClient(cfg MBConfig) *MBClient {
	if cfg.APIURL == "" {
		cfg.APIURL = "https://musicbrainz.org/ws/2"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 1
	}
	if cfg.AppName == "" {
		cfg.AppName = "Sonicore"
	}
	if cfg.AppVer == "" {
		cfg.AppVer = "0.1.0"
	}
	return &MBClient{
		http:           &http.Client{Timeout: 10 * time.Second},
		base:           cfg.APIURL,
		appName:        cfg.AppName,
		appVer:         cfg.AppVer,
		rateLimitPerSec: cfg.RateLimit,
	}
}

func (c *MBClient) rateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rateLimitPerSec <= 0 {
		return
	}
	interval := time.Second / time.Duration(c.rateLimitPerSec)
	elapsed := time.Since(c.lastReq)
	if elapsed < interval {
		time.Sleep(interval - elapsed)
	}
	c.lastReq = time.Now()
}

func (c *MBClient) get(path string, params url.Values, out interface{}) error {
	c.rateLimit()

	u := c.base + path
	if params == nil {
		params = url.Values{}
	}
	params.Set("fmt", "json")
	u += "?" + params.Encode()

	log.Printf("[mb] GET %s", u)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s ( sonicore@localhost )", c.appName, c.appVer))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("musicbrainz request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("musicbrainz HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	log.Printf("[mb] 200 %s", path)
	return json.NewDecoder(resp.Body).Decode(out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type MBArtistSearch struct {
	Artists []MBArtistBrief `json:"artists"`
}

type MBArtistBrief struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SortName string `json:"sort-name"`
	Type   string `json:"type"`
	Country string `json:"country"`
}

type MBReleaseSearch struct {
	Releases []MBRelease `json:"releases"`
}

type MBRelease struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Date     string        `json:"date"`
	Status   string        `json:"status"`
	Artists  []MBArtistRef `json:"artist-credit"`
	Media    []MBMedia     `json:"media"`
}

type MBArtistRef struct {
	Name  string `json:"name"`
	Artist *MBArtistBrief `json:"artist,omitempty"`
}

type MBMedia struct {
	Title      string      `json:"title"`
	Format     string      `json:"format"`
	TrackCount int         `json:"track-count"`
	Tracks     []MBTrack   `json:"tracks,omitempty"`
}

type MBTrack struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Title  string `json:"title"`
	Length int    `json:"length"`
}

type MBRecordingSearch struct {
	Recordings []MBRecording `json:"recordings"`
}

type MBRecording struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Length   int           `json:"length"`
	Artists  []MBArtistRef `json:"artist-credit"`
	Releases []struct {
		ID     string        `json:"id"`
		Title  string        `json:"title"`
		Date   string        `json:"date"`
		Status string        `json:"status"`
		Artists []MBArtistRef `json:"artist-credit"`
	} `json:"releases"`
	Tags []MBTag `json:"tags,omitempty"`
}

func (c *MBClient) SearchRecordings(title string, artists []string, album string) ([]MBRecording, error) {
	var result MBRecordingSearch
	q := url.Values{}
	query := "recording:" + title
	for _, a := range artists {
		if a != "" && a != "Unknown Artist" {
			query += " AND artist:" + a
		}
	}
	if album != "" && album != "Unknown Album" {
		query += " AND release:" + album
	}
	q.Set("query", query)
	q.Set("limit", "10")
	q.Set("inc", "artists+releases+tags")

	if err := c.get("/recording", q, &result); err != nil {
		return nil, err
	}

	return result.Recordings, nil
}

func (c *MBClient) SearchArtist(name string) (*MBArtistBrief, error) {
	var result MBArtistSearch
	q := url.Values{}
	q.Set("query", fmt.Sprintf("artist:%s", name))
	q.Set("limit", "5")

	if err := c.get("/artist", q, &result); err != nil {
		return nil, err
	}

	if len(result.Artists) == 0 {
		return nil, fmt.Errorf("no artist found for: %s", name)
	}

	return &result.Artists[0], nil
}

func (c *MBClient) SearchArtists(name string) ([]MBArtistBrief, error) {
	var result MBArtistSearch
	q := url.Values{}
	q.Set("query", fmt.Sprintf("artist:%s", name))
	q.Set("limit", "10")

	if err := c.get("/artist", q, &result); err != nil {
		return nil, err
	}
	return result.Artists, nil
}

func (c *MBClient) SearchReleases(name string) ([]MBRelease, error) {
	var result MBReleaseSearch
	q := url.Values{}
	q.Set("query", fmt.Sprintf("release:%s", name))
	q.Set("limit", "10")
	q.Set("inc", "artists")

	if err := c.get("/release", q, &result); err != nil {
		return nil, err
	}
	return result.Releases, nil
}

type MBArtistFull struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SortName string `json:"sort-name"`
	Type    string `json:"type"`
	Country string `json:"country"`
	Area    *struct {
		Name string `json:"name"`
	} `json:"area"`
	LifeSpan *struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
		Ended bool   `json:"ended"`
	} `json:"life-span"`
	Tags []MBTag `json:"tags"`
}

func (c *MBClient) LookupArtist(mbid string) (*MBArtistFull, error) {
	var result MBArtistFull
	if err := c.get("/artist/"+mbid, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *MBClient) SearchRelease(title, artist string) (*MBRelease, error) {
	var result MBReleaseSearch
	q := url.Values{}
	query := fmt.Sprintf("release:%s", title)
	if artist != "" {
		query += fmt.Sprintf(" AND artist:%s", artist)
	}
	q.Set("query", query)
	q.Set("limit", "5")

	if err := c.get("/release", q, &result); err != nil {
		return nil, err
	}

	if len(result.Releases) == 0 {
		return nil, fmt.Errorf("no release found for: %s - %s", artist, title)
	}

	return &result.Releases[0], nil
}

type MBReleaseFull struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Date     string        `json:"date"`
	Country  string        `json:"country"`
	Status   string        `json:"status"`
	Artists  []MBArtistRef `json:"artist-credit"`
	Media    []MBMedia     `json:"media"`
	Tags     []MBTag `json:"tags"`
}

func (c *MBClient) LookupRelease(mbid string) (*MBReleaseFull, error) {
	var result MBReleaseFull
	q := url.Values{}
	q.Set("inc", "artists+recordings+tags")
	if err := c.get("/release/"+mbid, q, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *MBClient) FetchCoverArt(releaseMBID string) ([]byte, string, error) {
	c.rateLimit()

	url := fmt.Sprintf("https://coverartarchive.org/release/%s/front", releaseMBID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s ( sonicore@localhost )", c.appName, c.appVer))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("cover art fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("cover art HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	ext := "jpg"
	if strings.Contains(contentType, "png") {
		ext = "png"
	}

	return body, ext, nil
}

type MBTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func GenreFromTags(tags []MBTag) string {
	if len(tags) == 0 {
		return ""
	}
	genres := []string{"rock", "pop", "jazz", "classical", "electronic", "hip hop", "r&b", "blues", "folk", "metal", "punk", "reggae", "country", "soul", "funk", "indie", "alternative", "dance", "ambient", "latin", "world"}
	for _, t := range tags {
		for _, g := range genres {
			if strings.EqualFold(t.Name, g) {
				return t.Name
			}
		}
	}
	if len(tags) > 0 {
		return tags[0].Name
	}
	return ""
}

func (c *MBClient) Close() {
}
