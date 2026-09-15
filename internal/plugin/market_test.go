package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func newTestMarket(t *testing.T) (*Market, string, string, *Repo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	pluginsDir := t.TempDir()
	cacheDir := t.TempDir()
	repo := NewRepo(db)
	mkt := NewMarket(pluginsDir, cacheDir, repo, &Manager{dir: pluginsDir, repo: repo})
	return mkt, pluginsDir, cacheDir, repo, mock
}

func repoRow(name, url string) RepoRow {
	return RepoRow{URL: url, Name: name, Official: true, Enabled: true}
}

func repoRowsSQL(cols []string, rows ...[]any) *sqlmock.Rows {
	s := sqlmock.NewRows(cols)
	for _, r := range rows {
		vals := make([]driver.Value, len(r))
		for i, v := range r {
			vals[i] = v
		}
		s.AddRow(vals...)
	}
	return s
}

func mockListRepos(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT url, name, official, enabled, added_at, last_sync, last_error").
		WillReturnRows(rows)
}

func writeRepoCache(t *testing.T, cacheDir, name string, doc repoJSON) {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "repos"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "repos", name+".json"), raw, 0o644))
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.10.0", "1.9.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3-beta", "1.2.3", -1}, // stable beats its prerelease
		{"1.2.3", "1.2.3-beta", 1},
		{"1.2.3-alpha", "1.2.3-beta", -1},
		{"1.2", "1.2.0", 0},
		{"1.2.3", "1.2", 1},
		{"garbage", "1.0.0", 1}, // lexicographic fallback ("g" > "1")
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if (c.want < 0 && got >= 0) || (c.want == 0 && got != 0) || (c.want > 0 && got <= 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestValidateRepoJSON(t *testing.T) {
	valid := `{"name":"demo","plugins":[{"name":"a","version":"1.0.0","download_url":"https://x/a.tar.gz","sha256":"ab"}]}`
	require.NoError(t, validateRepoJSON([]byte(valid)))
	require.ErrorIs(t, validateRepoJSON([]byte(`{}`)), ErrInvalidRepoJSON)
	require.ErrorIs(t, validateRepoJSON([]byte(`{"name":"x"}`)), ErrInvalidRepoJSON)
	require.ErrorIs(t, validateRepoJSON([]byte(`{"name":"x","plugins":[{"name":"a"}]}`)), ErrInvalidRepoJSON)
	// missing sha256
	require.ErrorIs(t, validateRepoJSON([]byte(`{"name":"x","plugins":[{"name":"a","version":"1.0.0","download_url":"https://x/a.tar.gz"}]}`)), ErrInvalidRepoJSON)
	// unsafe plugin name
	require.ErrorIs(t, validateRepoJSON([]byte(`{"name":"x","plugins":[{"name":"../evil","version":"1.0.0","download_url":"https://x/a.tar.gz","sha256":"ab"}]}`)), ErrInvalidRepoJSON)
}

func TestInstallRejectsUnsafeName(t *testing.T) {
	mkt, _, _, _, _ := newTestMarket(t)
	err := mkt.Install(context.Background(), "../../owned", "repo")
	require.Error(t, err)
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	// "../evil" entry must be rejected before anything escapes.
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "../evil.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte("evil"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	require.Error(t, extractTarGz(archive, dest))
	_, err = os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestExtractTarGzRejectsSymlinkAndAbsolute(t *testing.T) {
	dest := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
	}))
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "/abs.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte("abs"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	archive := filepath.Join(t.TempDir(), "s.tar.gz")
	require.NoError(t, os.WriteFile(archive, buf.Bytes(), 0o644))
	require.Error(t, extractTarGz(archive, dest))
}

func TestExtractTarGzHappyPath(t *testing.T) {
	dest := t.TempDir()
	archive := filepath.Join(t.TempDir(), "demo.tar.gz")
	require.NoError(t, os.WriteFile(archive, buildTarGz(t, map[string]string{
		"demo/manifest.toml": "[plugin]\nname = \"demo\"\n",
		"demo/demo":          "binary",
	}), 0o644))
	require.NoError(t, extractTarGz(archive, dest))
	require.Equal(t, filepath.Join(dest, "demo"), pluginRoot(dest))
	raw, err := os.ReadFile(filepath.Join(dest, "demo", "manifest.toml"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "name = \"demo\"")
}

func TestSyncRepo304ReusesCache(t *testing.T) {
	mkt, _, cacheDir, _, _ := newTestMarket(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{"name":"demo-plugins","plugins":[]}`))
	}))
	defer srv.Close()

	row := repoRow("demo-plugins", srv.URL)
	require.NoError(t, mkt.syncRepo(context.Background(), &row))
	require.NoError(t, mkt.syncRepo(context.Background(), &row))
	require.Equal(t, 2, hits)

	body, err := os.ReadFile(filepath.Join(cacheDir, "repos", "demo-plugins.json"))
	require.NoError(t, err)
	require.Contains(t, string(body), "demo-plugins")
	meta, err := mkt.loadMeta(filepath.Join(cacheDir, "repos", "demo-plugins.meta.json"))
	require.NoError(t, err)
	require.Equal(t, `"v1"`, meta.ETag)
}

func TestSyncRepoInvalidJSON(t *testing.T) {
	mkt, _, _, _, _ := newTestMarket(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	row := repoRow("bad", srv.URL)
	err := mkt.syncRepo(context.Background(), &row)
	require.ErrorIs(t, err, ErrInvalidRepoJSON)
}

func TestSyncRepoFetchFailureKeepsStaleCache(t *testing.T) {
	mkt, _, cacheDir, _, _ := newTestMarket(t)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "repos"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "repos", "stale.json"),
		[]byte(`{"name":"stale","plugins":[]}`), 0o644))
	row := repoRow("stale", "http://127.0.0.1:1/repo.json")
	require.Error(t, mkt.syncRepo(context.Background(), &row))
	body, err := os.ReadFile(filepath.Join(cacheDir, "repos", "stale.json"))
	require.NoError(t, err)
	require.Contains(t, string(body), "stale")
}

func TestCheckUpdatesFlagsNewerVersion(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	updated := time.Now()
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "demo-plugins", true, "ok", "", true, true, "demo", false, updated))
	mock.ExpectExec("UPDATE plugin_instances SET update_available").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))

	writeRepoCache(t, cacheDir, "demo-plugins", repoJSON{Name: "demo-plugins", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.1.0"},
	}})

	repos := []RepoRow{{URL: "https://x/repo.json", Name: "demo-plugins", Enabled: true}}
	require.NoError(t, mkt.checkUpdates(ctx, repos))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCatalogMergesRepos(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://o/repo.json", "official-repo", true, true, now, nil, ""},
		[]any{"https://t/repo.json", "third", false, true, now, nil, ""},
	))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "official-repo", true, "ok", "", true, true, "demo", true, now))

	writeRepoCache(t, cacheDir, "official-repo", repoJSON{Name: "official-repo", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.1.0", Author: "a"},
	}})
	writeRepoCache(t, cacheDir, "third", repoJSON{Name: "third", Plugins: []RepoPlugin{
		{Name: "other", Version: "2.0.0"},
	}})

	entries, err := mkt.Catalog(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	byName := map[string]CatalogEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	require.Equal(t, "official-repo", byName["demo"].Repo)
	require.True(t, byName["demo"].Official)
	require.True(t, byName["demo"].Installed)
	require.True(t, byName["demo"].UpdateAvailable)
	require.False(t, byName["other"].Official)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInstallFromMarket(t *testing.T) {
	mkt, pluginsDir, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	archive := buildTarGz(t, map[string]string{
		"demo/manifest.toml": "[plugin]\nname = \"demo\"\nversion = \"1.0.0\"\n",
		"demo/demo":          "binary",
	})
	var downloads int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		if r.URL.Path == "/repo.json" {
			_, _ = w.Write([]byte(`{"name":"demo-plugins","plugins":[{"name":"demo","version":"1.0.0","download_url":"` + srv.URL + `/demo.tar.gz","sha256":"` + shaOf(archive) + `"}]}`))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	// repoByName → ListRepos (enabled repo)
	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{srv.URL + "/repo.json", "demo-plugins", true, true, now, nil, ""},
	))
	// not yet installed (no row)
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}))
	// upsert the row
	mock.ExpectExec("INSERT INTO plugin_instances").
		WithArgs("demo", "demo", "1.0.0", "", "demo-plugins", "demo").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE plugin_instances SET update_available").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// mgr.Install
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, false, "demo"))
	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET enabled").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", StatusDisabled, "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, mkt.Install(ctx, "demo", "demo-plugins"))
	require.Equal(t, 2, downloads)
	// plugin dir exists with the manifest
	_, err := os.Stat(filepath.Join(pluginsDir, "demo", "manifest.toml"))
	require.NoError(t, err)
	// archive cached
	_, err = os.Stat(filepath.Join(cacheDir, "downloads", "demo-1.0.0.tar.gz"))
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInstallAfterSoftUninstall(t *testing.T) {
	mkt, pluginsDir, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	// Simulate a soft-uninstalled leftover: the directory and row exist, but
	// installed=false and the source is the same repo.
	require.NoError(t, os.MkdirAll(filepath.Join(pluginsDir, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "demo", "manifest.toml"),
		[]byte("[plugin]\nname = \"demo\"\nversion = \"1.0.0\"\n"), 0o644))

	archive := buildTarGz(t, map[string]string{
		"demo/manifest.toml": "[plugin]\nname = \"demo\"\nversion = \"1.0.0\"\n",
		"demo/demo":          "binary",
	})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo.json" {
			_, _ = w.Write([]byte(`{"name":"demo-plugins","plugins":[{"name":"demo","version":"1.0.0","download_url":"` + srv.URL + `/demo.tar.gz","sha256":"` + shaOf(archive) + `"}]}`))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{srv.URL + "/repo.json", "demo-plugins", true, true, now, nil, ""},
	))
	// soft-uninstalled row (installed=false)
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(false, false, "demo"))
	// leftover confirmed as our own repo's plugin
	mock.ExpectQuery("SELECT source FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"source"}).AddRow("demo-plugins"))
	mock.ExpectExec("INSERT INTO plugin_instances").
		WithArgs("demo", "demo", "1.0.0", "", "demo-plugins", "demo").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE plugin_instances SET update_available").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// mgr.Install
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, false, "demo"))
	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET enabled").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", StatusDisabled, "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, mkt.Install(ctx, "demo", "demo-plugins"))
	_, err := os.Stat(filepath.Join(pluginsDir, "demo", "manifest.toml"))
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInstallChecksumMismatch(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	archive := buildTarGz(t, map[string]string{"demo/manifest.toml": "x"})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo.json" {
			_, _ = w.Write([]byte(`{"name":"demo-plugins","plugins":[{"name":"demo","version":"1.0.0","download_url":"` + srv.URL + `/demo.tar.gz","sha256":"deadbeef"}]}`))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{srv.URL + "/repo.json", "demo-plugins", true, true, now, nil, ""},
	))
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}))

	err := mkt.Install(ctx, "demo", "demo-plugins")
	require.ErrorIs(t, err, ErrChecksumMismatch)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdatePluginAlreadyUpToDate(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo"))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "2.0.0", "desc", "demo-plugins", true, "ok", "", true, true, "demo", false, now))
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://x/repo.json", "demo-plugins", true, true, now, nil, ""},
	))

	writeRepoCache(t, cacheDir, "demo-plugins", repoJSON{Name: "demo-plugins", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.0.0"},
	}})

	err := mkt.UpdatePlugin(ctx, "demo")
	require.ErrorIs(t, err, ErrAlreadyUpToDate)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdatePluginNoRepoSource(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo"))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "manual", true, "ok", "", true, true, "demo", false, now))

	err := mkt.UpdatePlugin(ctx, "demo")
	require.ErrorIs(t, err, ErrNoRepoSource)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveOfficialRepoRefused(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mock.ExpectQuery("SELECT url, name, official, enabled, added_at, last_sync, last_error FROM plugin_repos WHERE url").
		WithArgs("https://o/repo.json").
		WillReturnRows(sqlmock.NewRows([]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"}).
			AddRow("https://o/repo.json", "official-repo", true, true, now, nil, ""))

	err := mkt.RemoveRepo(ctx, "https://o/repo.json")
	require.ErrorIs(t, err, ErrOfficialRepoUndeletable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveRepoUnknown(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT url, name, official, enabled, added_at, last_sync, last_error FROM plugin_repos WHERE url").
		WithArgs("https://x/repo.json").
		WillReturnRows(sqlmock.NewRows([]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"}))

	err := mkt.RemoveRepo(ctx, "https://x/repo.json")
	require.ErrorIs(t, err, ErrRepoNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddRepoRejectsInvalidJSON(t *testing.T) {
	mkt, _, _, _, _ := newTestMarket(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`nope`))
	}))
	defer srv.Close()
	err := mkt.AddRepo(context.Background(), srv.URL)
	require.ErrorIs(t, err, ErrInvalidRepoJSON)
}

func TestAddRepoStoresValidatedName(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"my-repo","plugins":[{"name":"a","version":"1.0.0","download_url":"https://x/a.tar.gz","sha256":"ab"}]}`))
	}))
	defer srv.Close()

	// duplicate-name check reads the existing repos (none)
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
	))
	mock.ExpectExec("INSERT INTO plugin_repos").
		WithArgs(srv.URL, "my-repo").
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, mkt.AddRepo(context.Background(), srv.URL))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddRepoRejectsDuplicateName(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"my-repo","plugins":[{"name":"a","version":"1.0.0","download_url":"https://x/a.tar.gz","sha256":"ab"}]}`))
	}))
	defer srv.Close()

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://other/repo.json", "my-repo", false, true, now, nil, ""},
	))

	err := mkt.AddRepo(context.Background(), srv.URL)
	require.ErrorIs(t, err, ErrRepoNameConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddRepoRejectsRedirectToPrivateHost(t *testing.T) {
	mkt, _, _, _, _ := newTestMarket(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/repo.json", http.StatusFound)
	}))
	defer srv.Close()
	// The public repo URL redirects to a loopback address: the client's
	// CheckRedirect must refuse the hop instead of pivoting into the host.
	err := mkt.AddRepo(context.Background(), srv.URL)
	require.Error(t, err)
}

func TestSanitizeName(t *testing.T) {
	require.Equal(t, "demo", sanitizeName("demo"))
	require.Equal(t, "", sanitizeName("../evil"))
	require.Equal(t, "", sanitizeName("a/b"))
	require.Equal(t, "", sanitizeName("."))
	require.Equal(t, "", sanitizeName(""))
}

func TestReconcileDeletesStaleLocalPlugin(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "local", true, "ok", "", true, true, "demo", false, now))
	mock.ExpectExec("DELETE FROM plugin_instances").
		WithArgs("demo").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, mkt.Reconcile(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReconcileRemovesMarketPluginWhenRepoGone(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "gone-repo", false, "stopped", "", true, true, "demo", false, now))
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
	))
	mock.ExpectExec("DELETE FROM plugin_instances").
		WithArgs("demo").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, mkt.Reconcile(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReconcileRecoversMarketPlugin(t *testing.T) {
	mkt, pluginsDir, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	archive := buildTarGz(t, map[string]string{
		"demo/manifest.toml": "[plugin]\nname = \"demo\"\nversion = \"1.0.0\"\n",
		"demo/demo":          "binary",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	now := time.Now()
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "demo-plugins", false, "stopped", "", true, true, "demo", false, now))
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{srv.URL + "/repo.json", "demo-plugins", true, true, now, nil, ""},
	))
	writeRepoCache(t, cacheDir, "demo-plugins", repoJSON{Name: "demo-plugins", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.0.0", DownloadURL: srv.URL + "/demo.tar.gz", SHA256: shaOf(archive)},
	}})
	mock.ExpectExec("INSERT INTO plugin_instances").
		WithArgs("demo", "demo", "1.0.0", "", "demo-plugins", "demo").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET update_available").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", StatusDisabled, "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, mkt.Reconcile(ctx))
	_, err := os.Stat(filepath.Join(pluginsDir, "demo", "manifest.toml"))
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnrichDownloads(t *testing.T) {
	mkt, _, cacheDir, _, mock := newTestMarket(t)
	ctx := context.Background()

	const ghURL = "https://github.com/owner/repo/releases/download/demo-v1.0.0/demo-1.0.0.tar.gz"
	require.NoError(t, mkt.saveDownloadCache(&downloadCache{Counts: map[string]downloadCountEntry{
		downloadKey("demo-plugins", "demo"): {Count: 42, At: time.Now()},
	}}))

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"https://o/repo.json", "demo-plugins", true, true, now, nil, ""},
	))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}))
	writeRepoCache(t, cacheDir, "demo-plugins", repoJSON{Name: "demo-plugins", Plugins: []RepoPlugin{
		{Name: "demo", Version: "1.0.0", DownloadURL: ghURL},
	}})

	instances := []Instance{
		{ID: "demo", Source: "demo-plugins"},
		{ID: "other", Source: "local"},
	}
	require.NoError(t, mkt.EnrichDownloads(ctx, instances))
	require.NotNil(t, instances[0].Downloads)
	require.Equal(t, 42, *instances[0].Downloads)
	require.Nil(t, instances[1].Downloads)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestErrorsJoinSyncPartialFailure(t *testing.T) {
	mkt, _, _, _, mock := newTestMarket(t)
	ctx := context.Background()

	now := time.Now()
	mockListRepos(mock, repoRowsSQL(
		[]string{"url", "name", "official", "enabled", "added_at", "last_sync", "last_error"},
		[]any{"http://127.0.0.1:1/repo.json", "broken", false, true, now, nil, ""},
	))
	mock.ExpectExec("UPDATE plugin_repos SET last_sync").
		WithArgs("http://127.0.0.1:1/repo.json", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}))

	err := mkt.Sync(ctx)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
