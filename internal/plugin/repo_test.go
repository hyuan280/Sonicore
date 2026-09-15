package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/stretchr/testify/require"
)

func newMockRepo(t *testing.T) (*Repo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewRepo(db), mock
}

func TestRepoUpsert(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectExec("INSERT INTO plugin_instances").
		WithArgs("demo", "demo", "1.0.0", "desc", "local", "demo-dir").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := repo.Upsert(context.Background(), &Instance{
		ID: "demo", Name: "demo", Version: "1.0.0", Description: "desc", Source: "local", Dir: "demo-dir",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoList(t *testing.T) {
	repo, mock := newMockRepo(t)
	updated := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "local", true, "ok", "", true, true, "demo-dir", false, updated))

	list, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "demo", list[0].ID)
	require.True(t, list[0].Enabled)
	require.Equal(t, "ok", list[0].Status)
	require.True(t, list[0].HasPage)
	require.True(t, list[0].Installed)
	require.Equal(t, "demo-dir", list[0].Dir)
	require.Equal(t, updated, list[0].UpdatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoListUninstalled(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(false).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "local", true, "stopped", "", false, false, "demo-dir", false, time.Now()))

	list, err := repo.ListUninstalled(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.False(t, list[0].Installed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoSetInstalledAndRuntimeState(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.SetInstalled(ctx, "demo", false))

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo-dir"))
	enabled, installed, dir, err := repo.GetRuntimeState(ctx, "demo")
	require.NoError(t, err)
	require.True(t, enabled)
	require.True(t, installed)
	require.Equal(t, "demo-dir", dir)

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("nope").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}))
	_, _, _, err = repo.GetRuntimeState(ctx, "nope")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoSetConfigValidationAndStorage(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	// Invalid JSON never reaches the DB and carries the sentinel so the
	// REST layer can map it to 400.
	err := repo.SetConfig(ctx, "demo", "not-json")
	require.ErrorIs(t, err, ErrInvalidConfigJSON)

	mock.ExpectExec("INSERT INTO plugin_config").
		WithArgs("demo", `{"a":1}`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.SetConfig(ctx, "demo", `{"a":1}`))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoGetConfig(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT config FROM plugin_config").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"config"}).AddRow([]byte(`{"a":1}`)))
	raw, err := repo.GetConfig(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, `{"a":1}`, raw)

	mock.ExpectQuery("SELECT config FROM plugin_config").
		WithArgs("nope").
		WillReturnRows(sqlmock.NewRows([]string{"config"}))
	raw, err = repo.GetConfig(ctx, "nope")
	require.NoError(t, err)
	require.Equal(t, "", raw)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoDataCRUD(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	err := repo.SetData(ctx, "demo", "k", "not-json")
	require.ErrorIs(t, err, ErrInvalidDataJSON)

	mock.ExpectExec("INSERT INTO plugin_data").
		WithArgs("demo", "k", `"v"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.SetData(ctx, "demo", "k", `"v"`))

	mock.ExpectQuery("SELECT value FROM plugin_data").
		WithArgs("demo", "k").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow([]byte(`"v"`)))
	raw, err := repo.GetData(ctx, "demo", "k")
	require.NoError(t, err)
	require.Equal(t, `"v"`, raw)

	mock.ExpectQuery("SELECT value FROM plugin_data").
		WithArgs("demo", "missing").
		WillReturnRows(sqlmock.NewRows([]string{"value"}))
	raw, err = repo.GetData(ctx, "demo", "missing")
	require.NoError(t, err)
	require.Equal(t, "", raw)

	mock.ExpectExec("DELETE FROM plugin_data").
		WithArgs("demo", "k").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.DeleteData(ctx, "demo", "k"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoLifecycleWrites(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE plugin_instances SET has_page").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.SetHasPage(ctx, "demo", true))

	mock.ExpectExec("UPDATE plugin_instances SET enabled").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.SetEnabled(ctx, "demo", false))

	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", "error", "boom").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.UpdateStatus(ctx, "demo", "error", "boom"))

	mock.ExpectExec("DELETE FROM plugin_instances").
		WithArgs("demo").
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Delete(ctx, "demo"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoListMergeFromManifest(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{
		repo: NewRepo(db),
		manifests: map[string]*pluginsdk.Manifest{
			"demo": {
				Plugin: pluginsdk.PluginInfo{
					Author: "sonicore",
					History: []pluginsdk.HistoryEntry{
						{Version: "1.0.0", Description: "first"},
					},
				},
			},
		},
	}

	mock.ExpectQuery("SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, update_available, updated_at").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "version", "description", "source", "enabled", "status", "status_msg", "has_page", "installed", "dir", "update_available", "updated_at",
		}).AddRow("demo", "demo", "1.0.0", "desc", "local", true, "ok", "", true, true, "demo-dir", false, time.Now()))

	list, err := m.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "sonicore", list[0].Author)
	require.Len(t, list[0].History, 1)
	require.Equal(t, "first", list[0].History[0].Description)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoStoreErrorPropagates(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE plugin_instances SET has_page").
		WithArgs("demo", true).
		WillReturnError(errors.New("conn refused"))
	err := repo.SetHasPage(ctx, "demo", true)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrInvalidDataJSON))
	require.NoError(t, mock.ExpectationsWereMet())
}
