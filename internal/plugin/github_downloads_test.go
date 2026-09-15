package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseGitHubDownloadURL(t *testing.T) {
	cases := []struct {
		raw                     string
		owner, repo, tag, asset string
		ok                      bool
	}{
		{
			raw:   "https://github.com/hyuan280/Sonicore-PluginSDK/releases/download/demo-v0.1.0/demo-0.1.0.tar.gz",
			owner: "hyuan280", repo: "Sonicore-PluginSDK", tag: "demo-v0.1.0", asset: "demo-0.1.0.tar.gz", ok: true,
		},
		{raw: "https://example.com/releases/download/v1/x.tar.gz", ok: false},
		{raw: "https://raw.githubusercontent.com/o/r/main/repo.json", ok: false},
		{raw: "https://github.com/o/r/releases/tag/v1", ok: false},
		{raw: "not-a-url", ok: false},
	}
	for _, c := range cases {
		owner, repo, tag, asset, ok := parseGitHubDownloadURL(c.raw)
		if ok != c.ok {
			t.Errorf("parseGitHubDownloadURL(%q) ok=%v, want %v", c.raw, ok, c.ok)
			continue
		}
		if ok && (owner != c.owner || repo != c.repo || tag != c.tag || asset != c.asset) {
			t.Errorf("parseGitHubDownloadURL(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
				c.raw, owner, repo, tag, asset, c.owner, c.repo, c.tag, c.asset)
		}
	}
}

func TestRefreshDownloadCounts(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/owner/repo/releases", r.URL.Path)
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		// demo has two released versions: 10 + 32 = 42 total downloads.
		_, _ = w.Write([]byte(`[
			{"assets":[{"name":"demo-1.0.0.tar.gz","download_count":10},{"name":"other.tar.gz","download_count":7}]},
			{"assets":[{"name":"demo-1.1.0.tar.gz","download_count":32}]}
		]`))
	}))
	defer api.Close()
	mkt.githubAPIBase = api.URL
	mkt.SetGitHubTokenProvider(func() string { return "tok" })

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://o/repo.json", "official-repo", true, true, now, nil, ""},
	))
	writeRepoCache(t, cacheDir, "official-repo", repoJSON{Name: "official-repo", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.1.0", DownloadURL: "https://github.com/owner/repo/releases/download/demo-v1.1.0/demo-1.1.0.tar.gz"},
		{Name: "selfhosted", Version: "2.0.0", DownloadURL: "https://example.com/selfhosted-2.0.0.tar.gz"},
	}})

	mkt.refreshDownloadCounts(ctx)
	require.NoError(t, mock.ExpectationsWereMet())

	counts := mkt.downloadCounts()
	require.Equal(t, 42, counts[downloadKey("official-repo", "demo")])
	_, ok := counts[downloadKey("official-repo", "selfhosted")]
	require.False(t, ok, "non-GitHub URL must not be tracked")
}

func TestCatalogDownloadsPopulatedAndOmitted(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	const ghURL = "https://github.com/owner/repo/releases/download/demo-v1.0.0/demo-1.0.0.tar.gz"
	if err := mkt.saveDownloadCache(&downloadCache{Counts: map[string]downloadCountEntry{
		downloadKey("official-repo", "demo"): {Count: 42, At: time.Now()},
	}}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://o/repo.json", "official-repo", true, true, now, nil, ""},
	))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(repoRowsSQL(
			[]string{"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at"},
		))
	writeRepoCache(t, cacheDir, "official-repo", repoJSON{Name: "official-repo", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.0.0", DownloadURL: ghURL},
		{Name: "selfhosted", Version: "2.0.0", DownloadURL: "https://example.com/selfhosted-2.0.0.tar.gz"},
	}})

	entries, err := mkt.Catalog(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	byName := map[string]CatalogEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	require.NotNil(t, byName["demo"].Downloads)
	require.Equal(t, 42, *byName["demo"].Downloads)
	require.Nil(t, byName["selfhosted"].Downloads)
	require.NoError(t, mock.ExpectationsWereMet())
}
