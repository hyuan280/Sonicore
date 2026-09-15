package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"

	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/ssrf"
)

// Sentinel errors let the REST layer map market failures to HTTP codes.
var (
	// ErrRepoNotFound marks an unknown repository name/URL.
	ErrRepoNotFound = errors.New("plugin repo not found")
	// ErrPluginNotFoundInRepo marks a plugin absent from its repo catalog.
	ErrPluginNotFoundInRepo = errors.New("plugin not found in repo")
	// ErrRepoDisabled marks operations on a disabled repository.
	ErrRepoDisabled = errors.New("plugin repo is disabled")
	// ErrOfficialRepoUndeletable marks removal attempts of official repos.
	ErrOfficialRepoUndeletable = errors.New("official repo cannot be removed")
	// ErrInvalidRepoJSON marks a repo.json that fails validation.
	ErrInvalidRepoJSON = errors.New("invalid repo.json")
	// ErrRepoNameConflict marks an add-repo whose declared name is already
	// used by another configured repository.
	ErrRepoNameConflict = errors.New("plugin repo name already exists")
	// ErrChecksumMismatch marks a download whose sha256 does not match.
	ErrChecksumMismatch = errors.New("download checksum mismatch")
	// ErrPluginDirConflict marks an install where the plugin directory
	// already exists (an uninstalled leftover or a manual install).
	ErrPluginDirConflict = errors.New("plugin directory already exists")
	// ErrInvalidPluginName marks an install name that is not a safe single
	// path element.
	ErrInvalidPluginName = errors.New("invalid plugin name")
	// ErrNoRepoSource marks an update for a plugin not installed from a
	// repository (source != a configured repo name).
	ErrNoRepoSource = errors.New("plugin was not installed from a repository")
	// ErrAlreadyUpToDate marks an update where the repo has no newer
	// version than the installed one.
	ErrAlreadyUpToDate = errors.New("plugin is already up to date")
	// ErrAlreadyInstalled marks an install of a plugin that is already
	// installed.
	ErrAlreadyInstalled = errors.New("plugin is already installed")
	// ErrNotInstalled marks an update of a plugin that is not installed.
	ErrNotInstalled = errors.New("plugin is not installed")
	// ErrRepoExists marks an add-repo whose URL is already configured.
	ErrRepoExists = errors.New("plugin repository already configured")
)

// repoFetchTimeout bounds one repo.json fetch.
const repoFetchTimeout = 30 * time.Second

// pluginDownloadTimeout bounds a full plugin tarball download. It is separate
// from repoFetchTimeout so a large plugin on a slow link does not fail a
// normal install.
const pluginDownloadTimeout = 10 * time.Minute

// Bounds for untrusted plugin archives: a compressed download cap, a total
// extracted-byte cap, a per-entry cap and an entry-count cap. They stop a
// malicious repo.json from exhausting disk or CPU with a high-ratio bomb.
const (
	maxPluginArchiveBytes = 512 << 20 // 512 MiB compressed download
	maxPluginExtractBytes = 1 << 30   // 1 GiB total extracted
	maxPluginFileBytes    = 512 << 20 // 512 MiB single entry
	maxPluginEntries      = 4096
)

// RepoPlugin is one plugin entry in a repo.json.
type RepoPlugin struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	UpdatedAt   string                   `json:"updated_at,omitempty"`
	DownloadURL string                   `json:"download_url"`
	SHA256      string                   `json:"sha256"`
	Author      string                   `json:"author,omitempty"`
	Description string                   `json:"description,omitempty"`
	History     []pluginsdk.HistoryEntry `json:"history,omitempty"`
}

type repoJSON struct {
	Name    string       `json:"name"`
	Plugins []RepoPlugin `json:"plugins"`
}

// repoMeta is the cache sidecar for one repository: the conditional-request
// headers and the fetch timestamp.
type repoMeta struct {
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
}

// CatalogEntry is one marketplace plugin across all repos (frontend
// PluginCatalogEntry).
type CatalogEntry struct {
	Name            string                   `json:"name"`
	Version         string                   `json:"version"`
	UpdatedAt       string                   `json:"updated_at,omitempty"`
	Author          string                   `json:"author,omitempty"`
	Description     string                   `json:"description,omitempty"`
	History         []pluginsdk.HistoryEntry `json:"history,omitempty"`
	Repo            string                   `json:"repo"`
	Official        bool                     `json:"official,omitempty"`
	Tags            []string                 `json:"tags,omitempty"`
	Downloads       *int                     `json:"downloads,omitempty"`
	Installed       bool                     `json:"installed,omitempty"`
	UpdateAvailable bool                     `json:"update_available,omitempty"`
}

// Market fetches configured plugin repositories, caches their repo.json
// files, compares versions against the installed plugins and executes
// installs/updates (download, sha256 verification, safe extraction and
// atomic directory swap).
type Market struct {
	pluginsDir     string
	cacheDir       string
	repo           *Repo
	mgr            *Manager
	client         *http.Client
	downloadClient *http.Client
	githubToken    func() string
	githubAPIBase  string
}

// NewMarket wires the marketplace. pluginsDir is {data_dir}/plugins,
// cacheDir is {data_dir}/cache/plugins.
//
// SSRF posture: the repo URL's first hop is admin-entered and trusted, so the
// clients only pin redirects to safe hosts (ssrf.RedirectGuard) and do NOT use
// ssrf.PublicDialContext (which would reject self-hosted private repos). The
// download_url inside repo.json IS re-vetted on the download path (see
// ensureArchive).
func NewMarket(pluginsDir, cacheDir string, repo *Repo, mgr *Manager) *Market {
	return &Market{
		pluginsDir: pluginsDir,
		cacheDir:   cacheDir,
		repo:       repo,
		mgr:        mgr,
		client:     &http.Client{Timeout: repoFetchTimeout, CheckRedirect: ssrf.RedirectGuard},
		downloadClient: &http.Client{
			Timeout: pluginDownloadTimeout,
			Transport: &http.Transport{
				// Re-resolve + pin at dial time to close the DNS-rebinding
				// window between ensureArchive's SafeURL pre-check and the
				// actual dial; private hosts are allowed only when the request
				// context carries ssrf.WithAllowPrivate (self-hosted repos).
				// Proxy is disabled so the pinned dialer always connects to the
				// target host directly.
				Proxy:           nil,
				DialContext:     ssrf.DialContext(&net.Dialer{Timeout: 10 * time.Second}),
				IdleConnTimeout: 90 * time.Second,
			},
			CheckRedirect: ssrf.RedirectGuard,
		},
		githubToken:   func() string { return "" },
		githubAPIBase: "https://api.github.com",
	}
}

// SetGitHubTokenProvider wires an optional GitHub API token (used to raise
// the download-count rate limit). The provider returns the decrypted token or
// "" when unset; it is read at each refresh pass.
func (m *Market) SetGitHubTokenProvider(fn func() string) {
	if fn != nil {
		m.githubToken = fn
	}
}

// SeedOfficialRepos inserts the built-in official repositories (see
// OfficialRepos) without touching existing rows.
func (m *Market) SeedOfficialRepos(ctx context.Context) error {
	return m.repo.EnsureOfficialRepos(ctx)
}

// Sync fetches every enabled repository, refreshes the cache and updates
// the update_available flags of installed plugins. Per-repo failures are
// recorded in plugin_repos.last_error and never abort the whole pass.
func (m *Market) Sync(ctx context.Context) error {
	repos, err := m.repo.ListRepos(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, row := range repos {
		if !row.Enabled {
			continue
		}
		if err := m.syncRepo(ctx, &row); err != nil {
			logger.Error("[plugin-market] sync repo %s: %v", row.Name, err)
			_ = m.repo.SetRepoSyncState(ctx, row.URL, err.Error())
			errs = append(errs, fmt.Errorf("sync repo %s: %w", row.Name, err))
			continue
		}
		_ = m.repo.SetRepoSyncState(ctx, row.URL, "")
	}
	if err := m.checkUpdates(ctx, repos); err != nil {
		errs = append(errs, err)
	}
	m.refreshDownloadCounts(ctx)
	return errors.Join(errs...)
}

// syncRepo fetches one repo with conditional headers and refreshes the
// cache. A 304 reuses the cached body; a fetch failure keeps the stale
// cache (the marketplace still serves it) and returns the error. row.URL is
// admin-entered and its first hop is not IP-gated (self-hosted private repos
// are supported); redirects are vetted by the client's CheckRedirect.
func (m *Market) syncRepo(ctx context.Context, row *RepoRow) error {
	if row.Name == "" {
		return fmt.Errorf("repo %s has no name", row.URL)
	}
	bodyPath, metaPath, err := m.cachePaths(row)
	if err != nil {
		return err
	}
	meta, err := m.loadMeta(metaPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, row.URL, nil)
	if err != nil {
		return err
	}
	if meta != nil {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
		if meta.LastModified != "" {
			req.Header.Set("If-Modified-Since", meta.LastModified)
		}
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch repo: %w", err)
	}
	defer resp.Body.Close()

	// 304: cache is current, just refresh the timestamp. The body may be
	// missing entirely (never fetched), so only skip writing when a
	// previous body exists.
	if resp.StatusCode == http.StatusNotModified {
		if meta == nil {
			return fmt.Errorf("not modified but no cached meta")
		}
		if _, statErr := os.Stat(bodyPath); statErr != nil {
			return fmt.Errorf("not modified but no cached body: %w", statErr)
		}
		meta.FetchedAt = time.Now()
		return m.saveMeta(metaPath, meta)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("repo returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read repo body: %w", err)
	}
	if err := validateRepoJSON(body); err != nil {
		return err
	}
	if err := atomicWriteFile(bodyPath, body, 0o644); err != nil {
		return err
	}
	meta = &repoMeta{
		URL:          row.URL,
		FetchedAt:    time.Now(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	return m.saveMeta(metaPath, meta)
}

// checkUpdates compares every installed market plugin against its source
// repo's cached catalog and persists update_available.
func (m *Market) checkUpdates(ctx context.Context, repos []RepoRow) error {
	installed, err := m.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("list installed plugins: %w", err)
	}
	byName := make(map[string]*RepoRow, len(repos))
	for i := range repos {
		byName[repos[i].Name] = &repos[i]
	}
	for _, inst := range installed {
		if inst.Source == "" || inst.Source == "local" || inst.Source == "manual" {
			continue
		}
		row, ok := byName[inst.Source]
		if !ok || !row.Enabled {
			// The source repo was removed or disabled: clear a stale
			// update_available flag so the UI does not advertise an update
			// that can no longer be applied.
			if err := m.repo.SetUpdateAvailable(ctx, inst.ID, false); err != nil {
				return err
			}
			continue
		}
		catalog, err := m.loadRepoCatalog(ctx, row)
		if err != nil {
			logger.Warn("[plugin-market] load catalog for %s: %v", row.Name, err)
			continue
		}
		entry := findPlugin(catalog, inst.ID)
		available := false
		if entry != nil {
			available = compareVersions(entry.Version, inst.Version) > 0
		}
		if err := m.repo.SetUpdateAvailable(ctx, inst.ID, available); err != nil {
			return err
		}
	}
	return nil
}

// Catalog merges every enabled repo's cached catalog into marketplace
// entries, marking installed plugins and their update state. Stale cache is
// served when a repo could not be fetched recently — never live-fetch here.
func (m *Market) Catalog(ctx context.Context) ([]CatalogEntry, error) {
	repos, err := m.repo.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := m.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	installedByName := make(map[string]Instance, len(installed))
	for _, i := range installed {
		installedByName[i.ID] = i
	}
	counts := m.downloadCounts()
	entries := make([]CatalogEntry, 0)
	for i := range repos {
		if !repos[i].Enabled {
			continue
		}
		catalog, err := m.loadRepoCatalogCached(ctx, &repos[i])
		if err != nil {
			logger.Warn("[plugin-market] catalog for %s unavailable: %v", repos[i].Name, err)
			continue
		}
		for _, p := range catalog.Plugins {
			e := CatalogEntry{
				Name:        p.Name,
				Version:     p.Version,
				UpdatedAt:   p.UpdatedAt,
				Author:      p.Author,
				Description: p.Description,
				History:     p.History,
				Repo:        repos[i].Name,
				Official:    repos[i].Official,
			}
			if n, ok := counts[downloadKey(repos[i].Name, p.Name)]; ok {
				e.Downloads = &n
			}
			if inst, ok := installedByName[p.Name]; ok && inst.Source == repos[i].Name {
				e.Installed = true
				e.UpdateAvailable = inst.UpdateAvailable
			}
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Detail returns one plugin across all repos; official repos win ties.
func (m *Market) Detail(ctx context.Context, name string) (*CatalogEntry, error) {
	entries, err := m.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	var best *CatalogEntry
	for i := range entries {
		if entries[i].Name != name {
			continue
		}
		if best == nil || (!best.Official && entries[i].Official) {
			best = &entries[i]
		}
	}
	return best, nil
}

// EnrichDownloads fills each instance's Downloads field from the shared
// download-count cache (via the market catalog). Install/uninstall state does
// not affect the count, so installed and uninstalled plugins report the same
// figure. Instances whose source is not a configured repo (local/manual) keep
// a nil Downloads (the field is omitted from the JSON).
func (m *Market) EnrichDownloads(ctx context.Context, instances []Instance) error {
	entries, err := m.Catalog(ctx)
	if err != nil {
		return err
	}
	byRepo := make(map[string]map[string]*int)
	for i := range entries {
		e := &entries[i]
		if e.Downloads == nil {
			continue
		}
		if _, ok := byRepo[e.Repo]; !ok {
			byRepo[e.Repo] = make(map[string]*int)
		}
		if _, exists := byRepo[e.Repo][e.Name]; !exists {
			v := *e.Downloads
			byRepo[e.Repo][e.Name] = &v
		}
	}
	for i := range instances {
		inst := &instances[i]
		if repoMap, ok := byRepo[inst.Source]; ok {
			if n, ok := repoMap[inst.ID]; ok {
				inst.Downloads = n
			}
		}
	}
	return nil
}

// AddRepo fetches and validates the repo.json at url, then stores the repo
// under the name declared inside it. The repo URL's first hop is not IP-gated
// here: it is admin-entered and may legitimately point at a self-hosted
// repository on a private network; redirects are still vetted by the client's
// CheckRedirect.
func (m *Market) AddRepo(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch repo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("repo returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read repo body: %w", err)
	}
	var doc repoJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRepoJSON, err)
	}
	if err := validateRepoJSON(body); err != nil {
		return err
	}
	// The repo name is the cache key and the plugin source identity: reject a
	// second repo that declares an already-used name (the plugin_repos table
	// only enforces url uniqueness).
	existing, err := m.repo.ListRepos(ctx)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.Name == doc.Name {
			return fmt.Errorf("%w: %q", ErrRepoNameConflict, doc.Name)
		}
	}
	return m.repo.AddRepo(ctx, url, doc.Name)
}

// RemoveRepo deletes a repository. Official repos refuse deletion.
func (m *Market) RemoveRepo(ctx context.Context, url string) error {
	row, err := m.repo.GetRepo(ctx, url)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrRepoNotFound
	}
	if row.Official {
		return ErrOfficialRepoUndeletable
	}
	return m.repo.RemoveRepo(ctx, url)
}

// Repos lists configured repositories.
func (m *Market) Repos(ctx context.Context) ([]RepoRow, error) {
	return m.repo.ListRepos(ctx)
}

// Install downloads a plugin from its repo, verifies the checksum,
// extracts it into the plugins dir and marks it installed (disabled — the
// admin enables it afterwards).
func (m *Market) Install(ctx context.Context, name, repoName string) error {
	// Serialize against enable/disable and update of the same plugin, and
	// against a concurrent Install of the same name from another repo.
	lk := m.mgr.pluginLock(name)
	lk.Lock()
	defer lk.Unlock()

	// name becomes a directory under pluginsDir: refuse anything that could
	// escape it before touching the filesystem.
	safe := sanitizeName(name)
	if safe == "" || safe != name {
		return fmt.Errorf("%w: %q", ErrInvalidPluginName, name)
	}
	row, err := m.repoByName(ctx, repoName)
	if err != nil {
		return err
	}
	catalog, err := m.loadRepoCatalog(ctx, row)
	if err != nil {
		return err
	}
	entry := findPlugin(catalog, name)
	if entry == nil {
		return ErrPluginNotFoundInRepo
	}
	_, installed, _, err := m.repo.GetRuntimeState(ctx, name)
	if err == nil && installed {
		return fmt.Errorf("%w: %s", ErrAlreadyInstalled, name)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load runtime state: %w", err)
	}
	// If a row already exists (installed=false) it must belong to this same
	// repo; otherwise it is a manual/local plugin or another repo's leftover,
	// and reinstalling from this repo would leave a mismatched source.
	if err == nil {
		source, srcErr := m.repo.GetSource(ctx, name)
		if srcErr != nil {
			return fmt.Errorf("load plugin source: %w", srcErr)
		}
		if source != repoName {
			return fmt.Errorf("%w: %s", ErrPluginDirConflict, name)
		}
	}
	target := filepath.Join(m.pluginsDir, safe)
	if _, statErr := os.Stat(target); statErr == nil {
		// The directory exists: it is our own soft-uninstalled leftover when a
		// row exists (remove it and re-download), or an orphan dir when not.
		if err != nil { // sql.ErrNoRows: directory with no plugin row
			return fmt.Errorf("%w: %s", ErrPluginDirConflict, target)
		}
		if rmErr := os.RemoveAll(target); rmErr != nil {
			return fmt.Errorf("remove leftover plugin dir: %w", rmErr)
		}
	}

	root, tmp, err := m.fetchAndExtract(ctx, entry, name, row.URL)
	if err != nil {
		return err
	}
	if err := os.Rename(root, target); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("move plugin dir: %w", err)
	}
	_ = os.RemoveAll(tmp)
	if err := m.repo.Upsert(ctx, &Instance{
		ID:          name,
		Name:        name,
		Version:     entry.Version,
		Description: entry.Description,
		Source:      repoName,
		Dir:         name,
	}); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	if err := m.repo.SetUpdateAvailable(ctx, name, false); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	return m.mgr.Install(ctx, name)
}

// UpdatePlugin downloads the newest version from the plugin's source repo
// and swaps the directory atomically, restarting the plugin when it was
// enabled.
func (m *Market) UpdatePlugin(ctx context.Context, id string) error {
	// Serialize the whole update against enable/disable (and concurrent
	// updates) of the same plugin, so the directory swap can never race a
	// restart of the old binary.
	lk := m.mgr.pluginLock(id)
	lk.Lock()
	defer lk.Unlock()

	enabled, installed, dirName, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	if dirName == "" {
		dirName = id
	}
	insts, err := m.repo.List(ctx)
	if err != nil {
		return err
	}
	var inst *Instance
	for i := range insts {
		if insts[i].ID == id {
			inst = &insts[i]
			break
		}
	}
	if inst == nil || inst.Source == "" || inst.Source == "local" || inst.Source == "manual" {
		return ErrNoRepoSource
	}
	row, err := m.repoByName(ctx, inst.Source)
	if err != nil {
		return err
	}
	catalog, err := m.loadRepoCatalog(ctx, row)
	if err != nil {
		return err
	}
	entry := findPlugin(catalog, id)
	if entry == nil {
		return ErrPluginNotFoundInRepo
	}
	if compareVersions(entry.Version, inst.Version) <= 0 {
		return ErrAlreadyUpToDate
	}

	// Stop the running process before swapping its directory.
	m.mgr.mu.Lock()
	rp, running := m.mgr.running[id]
	if running {
		delete(m.mgr.running, id)
	}
	m.mgr.mu.Unlock()
	if running {
		m.mgr.stopPlugin(id, rp)
	}

	root, tmp, err := m.fetchAndExtract(ctx, entry, id, row.URL)
	if err != nil {
		m.restartPlugin(ctx, id, enabled)
		return err
	}
	if err := m.swapPluginDir(root, dirName); err != nil {
		_ = os.RemoveAll(tmp)
		m.restartPlugin(ctx, id, enabled)
		return err
	}
	_ = os.RemoveAll(tmp)
	if err := m.repo.SetVersion(ctx, id, entry.Version); err != nil {
		_ = m.ensureRunning(ctx, id, enabled)
		return err
	}
	if err := m.repo.SetUpdateAvailable(ctx, id, false); err != nil {
		_ = m.ensureRunning(ctx, id, enabled)
		return err
	}
	return m.ensureRunning(ctx, id, enabled)
}

// Reconcile reconciles the plugin_instances rows against the on-disk plugin
// directories, run once at startup. A deleted local/manual plugin has its
// load record removed; a deleted market plugin is re-downloaded from its
// source repo (or its record removed when the repo/plugin is gone).
func (m *Market) Reconcile(ctx context.Context) error {
	installed, err := m.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("list installed plugins: %w", err)
	}
	for i := range installed {
		inst := &installed[i]
		dirName := inst.Dir
		if dirName == "" {
			dirName = inst.ID
		}
		if _, statErr := os.Stat(filepath.Join(m.pluginsDir, dirName)); statErr == nil {
			continue
		}
		if inst.Source == "" || inst.Source == "local" || inst.Source == "manual" {
			if delErr := m.repo.Delete(ctx, inst.ID); delErr != nil {
				logger.Warn("[plugin-market] remove stale local plugin %s: %v", inst.ID, delErr)
			} else {
				logger.Info("[plugin-market] removed stale local plugin %s (directory gone)", inst.ID)
			}
			continue
		}
		if recErr := m.recoverMarketPlugin(ctx, inst); recErr != nil {
			logger.Error("[plugin-market] recover plugin %s: %v", inst.ID, recErr)
		}
	}
	return nil
}

// recoverMarketPlugin re-downloads a deleted market plugin from its source
// repo. When the repo was removed it drops the row; a disabled repo or a
// transient failure leaves the row in place for the next pass.
func (m *Market) recoverMarketPlugin(ctx context.Context, inst *Instance) error {
	row, err := m.repoByName(ctx, inst.Source)
	if errors.Is(err, ErrRepoNotFound) {
		if delErr := m.repo.Delete(ctx, inst.ID); delErr != nil {
			return fmt.Errorf("delete unrecoverable plugin %s: %w", inst.ID, delErr)
		}
		logger.Info("[plugin-market] removed plugin %s (repo %s no longer exists)", inst.ID, inst.Source)
		return nil
	}
	if err != nil {
		return fmt.Errorf("locate repo %s: %w", inst.Source, err)
	}
	catalog, err := m.loadRepoCatalog(ctx, row)
	if err != nil {
		return fmt.Errorf("load catalog %s: %w", inst.Source, err)
	}
	entry := findPlugin(catalog, inst.ID)
	if entry == nil {
		if delErr := m.repo.Delete(ctx, inst.ID); delErr != nil {
			return fmt.Errorf("delete removed plugin %s: %w", inst.ID, delErr)
		}
		logger.Info("[plugin-market] removed plugin %s (no longer in repo %s)", inst.ID, inst.Source)
		return nil
	}
	if err := m.recoverPlugin(ctx, inst, entry, row.URL); err != nil {
		return fmt.Errorf("recover %s: %w", inst.ID, err)
	}
	logger.Info("[plugin-market] recovered plugin %s from %s", inst.ID, inst.Source)
	return nil
}

// recoverPlugin re-downloads a market plugin whose directory was deleted and
// restores it, keeping the persisted row. An enabled plugin is started again.
func (m *Market) recoverPlugin(ctx context.Context, inst *Instance, entry *RepoPlugin, repoURL string) error {
	// Serialize against enable/disable and update of the same plugin.
	lk := m.mgr.pluginLock(inst.ID)
	lk.Lock()
	defer lk.Unlock()

	dirName := inst.Dir
	if dirName == "" {
		dirName = inst.ID
	}
	target := filepath.Join(m.pluginsDir, dirName)
	if _, err := os.Stat(target); err == nil {
		// The directory reappeared (concurrent install/restore): nothing to do.
		return nil
	}
	root, tmp, err := m.fetchAndExtract(ctx, entry, inst.ID, repoURL)
	if err != nil {
		return err
	}
	if err := os.Rename(root, target); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("restore plugin dir: %w", err)
	}
	_ = os.RemoveAll(tmp)
	if err := m.repo.Upsert(ctx, &Instance{
		ID:          inst.ID,
		Name:        inst.ID,
		Version:     entry.Version,
		Description: entry.Description,
		Source:      inst.Source,
		Dir:         dirName,
	}); err != nil {
		return err
	}
	if err := m.repo.SetInstalled(ctx, inst.ID, true); err != nil {
		return err
	}
	if err := m.repo.SetUpdateAvailable(ctx, inst.ID, false); err != nil {
		return err
	}
	if inst.Enabled {
		if err := m.restartPlugin(ctx, inst.ID, true); err != nil {
			return err
		}
	} else {
		logDBError(ctx, m.repo, inst.ID, StatusDisabled, "")
	}
	return nil
}

// restartPlugin loads the manifest and starts the process again. A failure
// marks the plugin errored and rolls the enabled flag back, mirroring
// SetEnabled's enable path.
func (m *Market) restartPlugin(ctx context.Context, id string, enabled bool) error {
	if !enabled {
		return nil
	}
	_, installed, dirName, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil || !installed {
		return err
	}
	if dirName == "" {
		dirName = id
	}
	manifest, err := pluginsdk.LoadManifest(filepath.Join(m.pluginsDir, dirName, "manifest.toml"))
	if err != nil {
		_ = m.repo.SetEnabled(ctx, id, false)
		logDBError(ctx, m.repo, id, StatusError, "update failed: "+err.Error())
		return fmt.Errorf("load manifest: %w", err)
	}
	m.mgr.mu.Lock()
	m.mgr.manifests[id] = manifest
	m.mgr.mu.Unlock()
	if err := m.mgr.start(ctx, filepath.Join(m.pluginsDir, dirName), manifest); err != nil {
		_ = m.repo.SetEnabled(ctx, id, false)
		logDBError(ctx, m.repo, id, StatusError, "update failed: "+err.Error())
		return err
	}
	if err := m.mgr.refreshHasPage(ctx, id); err != nil {
		logger.Warn("[plugin-market] probe data page for %s: %v", id, err)
	}
	return nil
}

// ensureRunning restarts an enabled plugin or marks a disabled one stopped.
// Used after a directory swap so a failure on the post-swap DB writes never
// leaves an enabled plugin in a half-stopped state.
func (m *Market) ensureRunning(ctx context.Context, id string, enabled bool) error {
	if enabled {
		return m.restartPlugin(ctx, id, true)
	}
	logDBError(ctx, m.repo, id, StatusDisabled, "")
	return nil
}

// swapPluginDir atomically replaces the plugin's directory with the newly
// extracted one. The old directory is renamed away first (so the new one
// can take the final name) and removed only after the swap succeeded; any
// failure restores the old directory.
func (m *Market) swapPluginDir(root, dirName string) error {
	final := filepath.Join(m.pluginsDir, dirName)
	backup := final + ".old-" + fmt.Sprint(time.Now().UnixNano())
	if err := os.Rename(final, backup); err != nil {
		return fmt.Errorf("move old plugin dir: %w", err)
	}
	if err := os.Rename(root, final); err != nil {
		if rbErr := os.Rename(backup, final); rbErr != nil {
			logger.Error("[plugin-market] swap %s failed: move new dir: %v; restore old dir also failed: %v (old dir left at %s)", dirName, err, rbErr, backup)
			return fmt.Errorf("move new plugin dir: %w (restore old dir failed, old dir left at %s: %v)", err, backup, rbErr)
		}
		return fmt.Errorf("move new plugin dir: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		logger.Warn("[plugin-market] remove old dir %s: %v", backup, err)
	}
	return nil
}

// fetchAndExtract downloads the plugin tarball (reusing a cached download
// whose checksum still matches), verifies sha256, extracts it into a temp
// dir and returns the extracted plugin root directory plus the temp dir that
// contains it. The caller renames root into place and must remove tmpDir
// afterwards (it is the parent temp directory left behind when the archive has
// a single top-level directory).
func (m *Market) fetchAndExtract(ctx context.Context, entry *RepoPlugin, id, repoURL string) (string, string, error) {
	dlDir := filepath.Join(m.cacheDir, "downloads")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		return "", "", err
	}
	archive := filepath.Join(dlDir, sanitizeName(entry.Name)+"-"+sanitizeName(entry.Version)+".tar.gz")
	if err := m.ensureArchive(ctx, archive, entry.DownloadURL, entry.SHA256, repoURL); err != nil {
		return "", "", err
	}
	tmp, err := os.MkdirTemp(m.pluginsDir, "."+sanitizeName(id)+".extract-")
	if err != nil {
		return "", "", fmt.Errorf("create extract dir: %w", err)
	}
	if err := extractTarGz(archive, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", "", err
	}
	root := pluginRoot(tmp)
	if root == "" {
		_ = os.RemoveAll(tmp)
		return "", "", fmt.Errorf("archive contains no plugin directory")
	}
	// Validate the manifest before the caller commits the directory, so a
	// missing/illegal manifest fails here (temp dir cleaned up) instead of
	// leaving a half-installed plugin directory behind.
	if _, err := pluginsdk.LoadManifest(filepath.Join(root, "manifest.toml")); err != nil {
		_ = os.RemoveAll(tmp)
		return "", "", fmt.Errorf("archive has invalid manifest: %w", err)
	}
	return root, tmp, nil
}

// ensureArchive downloads the tarball to archive when it is missing or its
// checksum no longer matches, verifying the sha256 afterwards. The download
// goes to a unique temp file (concurrent installs never clobber each other)
// and is size-capped.
func (m *Market) ensureArchive(ctx context.Context, archive, url, wantSHA, repoURL string) error {
	// Cache hit: only stat + stream-hash when a checksum is required, so a
	// large archive is never read whole into memory.
	if st, err := os.Stat(archive); err == nil && !st.IsDir() {
		if wantSHA == "" {
			return nil
		}
		if got, gerr := sha256File(archive); gerr == nil && got == strings.ToLower(wantSHA) {
			return nil
		}
		_ = os.Remove(archive)
	}
	// First-hop SSRF guard. The download_url comes from the repo.json, which is
	// attacker-influenced once third-party repos are allowed. It must resolve to
	// a public address, OR be served from the same host as the repo URL (a
	// self-hosted deployment where repo.json and its tarballs live on one host).
	// sameHost is a hostname comparison, so it is not fooled by a repo domain
	// that resolves to a mix of public and private addresses.
	urlIsPublic := ssrf.SafeURL(url)
	if !urlIsPublic && !sameHost(repoURL, url) {
		return fmt.Errorf("download url %q is not a safe target", url)
	}
	// A private-but-same-host download may dial private addresses; public
	// downloads stay pinned to public addresses by the client's DialContext.
	reqCtx := ctx
	if !urlIsPublic {
		reqCtx = ssrf.WithAllowPrivate(ctx)
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download plugin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(archive), filepath.Base(archive)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxPluginArchiveBytes+1)); err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if st, err := os.Stat(tmpName); err == nil && st.Size() > maxPluginArchiveBytes {
		return fmt.Errorf("download exceeds %d bytes", maxPluginArchiveBytes)
	}
	if wantSHA != "" {
		got, err := sha256File(tmpName)
		if err != nil {
			return err
		}
		if got != strings.ToLower(wantSHA) {
			return fmt.Errorf("%w: want %s got %s", ErrChecksumMismatch, wantSHA, got)
		}
	}
	if err := os.Rename(tmpName, archive); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// cacheMatchesURL reports whether the cached repo body belongs to row.URL via
// the meta sidecar. It discards a stale cache left by a repo that was removed
// and re-added with the same name but a different URL. A missing/empty meta
// cannot be verified and is treated as matching (backward compatible with
// caches written without a meta sidecar).
func (m *Market) cacheMatchesURL(row *RepoRow) bool {
	_, metaPath, err := m.cachePaths(row)
	if err != nil {
		return false
	}
	meta, err := m.loadMeta(metaPath)
	if err != nil || meta == nil || meta.URL == "" {
		return true
	}
	return meta.URL == row.URL
}

// loadRepoCatalogCached reads a repo's catalog from the cache only. A missing
// or malformed cache returns an error (the caller skips the repo) and never
// triggers a live fetch — the marketplace read path (Catalog/List/Detail)
// must not block on the network or an external repository.
func (m *Market) loadRepoCatalogCached(ctx context.Context, row *RepoRow) (*repoJSON, error) {
	if !m.cacheMatchesURL(row) {
		return nil, errors.New("cached repo.json belongs to a different URL")
	}
	bodyPath, _, err := m.cachePaths(row)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, err
	}
	var doc repoJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse repo.json: %w", err)
	}
	return &doc, nil
}

// loadRepoCatalog reads a repo's catalog from the cache; when the cache is
// missing it falls back to one live fetch (so AddRepo → install works
// before the first scheduled sync). It is used on write paths (install,
// update, recover, sync) only.
func (m *Market) loadRepoCatalog(ctx context.Context, row *RepoRow) (*repoJSON, error) {
	bodyPath, _, err := m.cachePaths(row)
	if err != nil {
		return nil, err
	}
	if m.cacheMatchesURL(row) {
		if cached, rerr := os.ReadFile(bodyPath); rerr == nil {
			var doc repoJSON
			if jerr := json.Unmarshal(cached, &doc); jerr == nil {
				return &doc, nil
			}
		}
	}
	if err := m.syncRepo(ctx, row); err != nil {
		return nil, err
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, err
	}
	var doc repoJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse repo.json: %w", err)
	}
	return &doc, nil
}

func (m *Market) repoByName(ctx context.Context, name string) (*RepoRow, error) {
	repos, err := m.repo.ListRepos(ctx)
	if err != nil {
		return nil, err
	}
	for i := range repos {
		if repos[i].Name == name {
			if !repos[i].Enabled {
				return nil, ErrRepoDisabled
			}
			return &repos[i], nil
		}
	}
	return nil, ErrRepoNotFound
}

// cachePaths derives the body/meta cache paths for one repo. The repo name
// is the primary key (it is unique enough in practice and keeps files
// readable); an unsafe name falls back to a hash of the URL.
func (m *Market) cachePaths(row *RepoRow) (body, meta string, err error) {
	name := sanitizeName(row.Name)
	if name == "" {
		name = fmt.Sprintf("%x", sha256.Sum256([]byte(row.URL)))[:16]
	}
	dir := filepath.Join(m.cacheDir, "repos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, name+".json"), filepath.Join(dir, name+".meta.json"), nil
}

func (m *Market) loadMeta(path string) (*repoMeta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var meta repoMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse repo meta: %w", err)
	}
	return &meta, nil
}

func (m *Market) saveMeta(path string, meta *repoMeta) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, raw, 0o644)
}

// validateRepoJSON enforces the minimum repo.json schema.
func validateRepoJSON(body []byte) error {
	var doc repoJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRepoJSON, err)
	}
	if strings.TrimSpace(doc.Name) == "" {
		return fmt.Errorf("%w: missing repo name", ErrInvalidRepoJSON)
	}
	// The repo name is the cache filename and the plugin source identity: it
	// must be a single safe path element (no whitespace padding, separators or
	// ".."), otherwise two names differing only by padding would collide on the
	// same cache file.
	if sanitizeName(doc.Name) != doc.Name {
		return fmt.Errorf("%w: repo name %q is not safe", ErrInvalidRepoJSON, doc.Name)
	}
	if doc.Plugins == nil {
		return fmt.Errorf("%w: missing plugins list", ErrInvalidRepoJSON)
	}
	for i := range doc.Plugins {
		p := &doc.Plugins[i]
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Version) == "" {
			return fmt.Errorf("%w: plugin %d missing name/version", ErrInvalidRepoJSON, i)
		}
		if sanitizeName(p.Name) != p.Name {
			return fmt.Errorf("%w: plugin %q has unsafe name", ErrInvalidRepoJSON, p.Name)
		}
		if strings.TrimSpace(p.DownloadURL) == "" {
			return fmt.Errorf("%w: plugin %s missing download_url", ErrInvalidRepoJSON, p.Name)
		}
		if strings.TrimSpace(p.SHA256) == "" {
			return fmt.Errorf("%w: plugin %s missing sha256", ErrInvalidRepoJSON, p.Name)
		}
	}
	return nil
}

func findPlugin(catalog *repoJSON, name string) *RepoPlugin {
	for i := range catalog.Plugins {
		if catalog.Plugins[i].Name == name {
			return &catalog.Plugins[i]
		}
	}
	return nil
}

// sanitizeName reduces a name to a safe single path element ("" when
// unsafe), so names can be joined into file paths without escaping.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return ""
	}
	return name
}

// sameHost reports whether two URLs share the same hostname (case-insensitive,
// ignoring scheme, port and path). Used to allow a plugin download to reach a
// private host only when it is the same host that served the repo.json.
func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Hostname(), ub.Hostname())
}

// extractTarGz unpacks a .tar.gz into destDir, rejecting anything that could
// escape the target (absolute paths, ".." elements, symlinks) and bounding the
// entry count, per-entry size and total extracted size so a high-ratio bomb
// cannot fill the disk.
func extractTarGz(path, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	var entries int
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		entries++
		if entries > maxPluginEntries {
			return fmt.Errorf("archive has too many entries (> %d)", maxPluginEntries)
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fmt.Errorf("tar entry %s: links are not allowed", hdr.Name)
		}
		rel, err := safeTarPath(hdr.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > maxPluginFileBytes {
				return fmt.Errorf("tar entry %s exceeds %d bytes", hdr.Name, maxPluginFileBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			n, err := io.Copy(out, tr)
			if err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			total += n
			if total > maxPluginExtractBytes {
				return fmt.Errorf("archive extracts beyond %d bytes", maxPluginExtractBytes)
			}
		}
	}
}

// safeTarPath validates one tar entry name and returns its relative form.
func safeTarPath(name string) (string, error) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return "", nil
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return "", fmt.Errorf("tar entry %q escapes the target directory", name)
	}
	clean := filepath.Clean(name)
	if clean == "." || clean == "" {
		return "", nil
	}
	return clean, nil
}

// pluginRoot returns the root directory of an extracted plugin archive:
// when the archive contains exactly one top-level directory, that is the
// root; otherwise the extraction dir itself is.
func pluginRoot(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name())
	}
	return dir
}

// compareVersions compares two semver-ish versions ("1.2.3", "v1.2",
// "1.2.3-beta"). Numeric components win; a release without a prerelease beats
// the same version with one; prerelease identifiers compare lexically.
// Unparseable versions fall back to lexicographic comparison. Returns <0, 0 or
// >0.
func compareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if oka && okb {
		for i := 0; i < 3; i++ {
			if pa.num[i] != pb.num[i] {
				if pa.num[i] < pb.num[i] {
					return -1
				}
				return 1
			}
		}
		return comparePrerelease(pa.pre, pb.pre)
	}
	return strings.Compare(a, b)
}

type parsedVersion struct {
	num [3]int
	pre string // "" = no prerelease (stable)
}

func parseVersion(v string) (parsedVersion, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return parsedVersion{}, false
	}
	var out parsedVersion
	for i, p := range parts {
		// Bound the digit count so a maliciously long numeric segment cannot
		// overflow the int during accumulation; an over-long segment is treated
		// as unparseable and falls back to lexicographic comparison.
		if p == "" || len(p) > 9 {
			return parsedVersion{}, false
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return parsedVersion{}, false
			}
			n = n*10 + int(c-'0')
		}
		out.num[i] = n
	}
	out.pre = pre
	return out, true
}

// comparePrerelease orders two prerelease identifiers: a stable release
// (empty) ranks above any prerelease; two prereleases compare lexically.
func comparePrerelease(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	default:
		return strings.Compare(a, b)
	}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// atomicWriteFile writes body to path atomically via a unique temp file, so
// concurrent writers to the same path never truncate or rename each other's
// temp file.
func atomicWriteFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
