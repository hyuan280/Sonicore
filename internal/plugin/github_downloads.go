package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

const (
	githubHost = "github.com"

	// downloadCountTTL is how long a cached download count is considered
	// fresh before a refresh re-queries the GitHub API.
	downloadCountTTL = 24 * time.Hour
)

// downloadCountEntry is one cached download count for a plugin.
type downloadCountEntry struct {
	Count int       `json:"count"`
	At    time.Time `json:"at"`
}

// downloadCache maps a plugin (keyed "{repoName}/{pluginName}") to its cached
// total download count across all released versions. A present key is a real
// count (including 0); an absent key means "unknown" (the frontend hides the
// download figure).
type downloadCache struct {
	Counts map[string]downloadCountEntry `json:"counts"`
}

// downloadKey is the cache key for one plugin: the market repo name plus the
// plugin name (install/uninstall does not change it, so installed and
// uninstalled plugins resolve to the same entry).
func downloadKey(repoName, pluginName string) string {
	return repoName + "/" + pluginName
}

// githubReleaseAsset is the minimal GitHub release-asset shape we read.
type githubReleaseAsset struct {
	Name          string `json:"name"`
	DownloadCount int    `json:"download_count"`
}

type githubRelease struct {
	Assets []githubReleaseAsset `json:"assets"`
}

// parseGitHubDownloadURL decomposes a GitHub release-asset download URL of the
// form https://github.com/{owner}/{repo}/releases/download/{tag}/{asset} into
// its parts. Non-GitHub hosts and any other path shape return ok=false.
func parseGitHubDownloadURL(raw string) (owner, repo, tag, asset string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", "", false
	}
	if u.Host != githubHost {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 6 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", "", "", false
	}
	owner = parts[0]
	repo = parts[1]
	asset = parts[len(parts)-1]
	tag = strings.Join(parts[4:len(parts)-1], "/")
	if owner == "" || repo == "" || tag == "" || asset == "" {
		return "", "", "", "", false
	}
	return owner, repo, tag, asset, true
}

// githubRepoFromPlugins returns the GitHub owner/repo for a market repo by
// parsing the first plugin's download_url that is a GitHub release URL. All
// plugins in one market repo are assumed to live in the same GitHub repo.
func githubRepoFromPlugins(plugins []RepoPlugin) (owner, repo string, ok bool) {
	for _, p := range plugins {
		if o, r, _, _, okk := parseGitHubDownloadURL(p.DownloadURL); okk {
			return o, r, true
		}
	}
	return "", "", false
}

// matchPluginAsset maps a release asset name ("{name}-{version}.tar.gz") to
// its plugin name. names must be sorted longest-first so a name that is a
// prefix of another (demo vs demo-pro) matches precisely.
func matchPluginAsset(names []string, assetName string) (string, bool) {
	if !strings.HasSuffix(assetName, ".tar.gz") {
		return "", false
	}
	for _, n := range names {
		if strings.HasPrefix(assetName, n+"-") {
			return n, true
		}
	}
	return "", false
}

// fetchRepoDownloadCounts lists every release of a GitHub repo (paginated) and
// sums each plugin's download_count across all of its versioned assets. The
// token is optional (unauthenticated requests hit GitHub's lower rate limit).
func (m *Market) fetchRepoDownloadCounts(ctx context.Context, token, owner, repo string, plugins []RepoPlugin) (map[string]int, error) {
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name)
	}
	// Longest first so "demo-pro" matches before the "demo" prefix.
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	sums := make(map[string]int)
	page := 1
	for {
		u := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100&page=%d", m.githubAPIBase, owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "sonicore-plugin-market")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := m.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("github api status %d", resp.StatusCode)
		}
		var releases []githubRelease
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&releases)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			break
		}
		for _, rel := range releases {
			for _, a := range rel.Assets {
				if name, ok := matchPluginAsset(names, a.Name); ok {
					sums[name] += a.DownloadCount
				}
			}
		}
		if len(releases) < 100 {
			break
		}
		page++
	}
	return sums, nil
}

// refreshDownloadCounts walks every enabled repo's cached catalog and refreshes
// each plugin's total download count (all released versions) when its cache
// entry is missing or stale. One "list releases" call serves the whole repo, so
// the number of API calls is proportional to the repo count, not the plugin
// count. Failures never abort the pass; a 404/403/429 leaves the previous (or
// absent) count in place.
func (m *Market) refreshDownloadCounts(ctx context.Context) {
	cache, err := m.loadDownloadCache()
	if err != nil {
		logger.Warn("[plugin-market] load download cache: %v", err)
		cache = &downloadCache{Counts: map[string]downloadCountEntry{}}
	}
	repos, err := m.repo.ListRepos(ctx)
	if err != nil {
		logger.Warn("[plugin-market] list repos for downloads: %v", err)
		return
	}
	token := m.githubToken()

	for i := range repos {
		row := &repos[i]
		if !row.Enabled {
			continue
		}
		bodyPath, _, err := m.cachePaths(row)
		if err != nil {
			continue
		}
		body, err := os.ReadFile(bodyPath)
		if err != nil {
			continue
		}
		var doc repoJSON
		if err := json.Unmarshal(body, &doc); err != nil {
			continue
		}
		owner, ghRepo, ok := githubRepoFromPlugins(doc.Plugins)
		if !ok {
			continue
		}
		// Skip the whole repo when every plugin is still fresh.
		stale := false
		for _, p := range doc.Plugins {
			if e, ok := cache.Counts[downloadKey(row.Name, p.Name)]; !ok || time.Since(e.At) >= downloadCountTTL {
				stale = true
				break
			}
		}
		if !stale {
			continue
		}
		sums, err := m.fetchRepoDownloadCounts(ctx, token, owner, ghRepo, doc.Plugins)
		if err != nil {
			logger.Warn("[plugin-market] download counts for %s: %v", row.Name, err)
			continue
		}
		now := time.Now()
		for _, p := range doc.Plugins {
			if n, ok := sums[p.Name]; ok {
				cache.Counts[downloadKey(row.Name, p.Name)] = downloadCountEntry{Count: n, At: now}
			}
		}
	}

	if err := m.saveDownloadCache(cache); err != nil {
		logger.Warn("[plugin-market] save download cache: %v", err)
	}
}

// downloadCounts loads the cached counts as a flat "{repoName}/{pluginName}"→count
// map.
func (m *Market) downloadCounts() map[string]int {
	cache, err := m.loadDownloadCache()
	if err != nil {
		return nil
	}
	out := make(map[string]int, len(cache.Counts))
	for u, e := range cache.Counts {
		out[u] = e.Count
	}
	return out
}

func (m *Market) downloadCachePath() string {
	return filepath.Join(m.cacheDir, "downloads.json")
}

func (m *Market) loadDownloadCache() (*downloadCache, error) {
	raw, err := os.ReadFile(m.downloadCachePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &downloadCache{Counts: map[string]downloadCountEntry{}}, nil
		}
		return nil, err
	}
	var c downloadCache
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.Counts == nil {
		c.Counts = map[string]downloadCountEntry{}
	}
	return &c, nil
}

func (m *Market) saveDownloadCache(c *downloadCache) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return atomicWriteFile(m.downloadCachePath(), raw, 0o644)
}
