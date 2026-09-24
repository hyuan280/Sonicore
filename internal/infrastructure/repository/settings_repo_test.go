package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const getSettingQuery = `SELECT value FROM server_settings WHERE category=$1 AND key=$2`
const getManySettingsQuery = `SELECT key, value FROM server_settings WHERE category=$1 AND key = ANY($2)`
const upsertSettingQuery = `INSERT INTO server_settings (key, value, category) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value=$2, category=$3`

func TestSettingsRepoRequiresCategory(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewSettingsRepo(db)

	_, err := repo.Get(context.Background(), "", "k")
	assert.ErrorIs(t, err, ErrSettingsCategoryRequired)
	_, err = repo.GetMany(context.Background(), "", []string{"k"})
	assert.ErrorIs(t, err, ErrSettingsCategoryRequired)
	assert.ErrorIs(t, repo.Set(context.Background(), "", "k", "v"), ErrSettingsCategoryRequired)
	assert.ErrorIs(t, repo.SetMany(context.Background(), "", map[string]string{"k": "v"}), ErrSettingsCategoryRequired)
}

func TestSettingsRepoGet(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(getSettingQuery)).
		WithArgs(CategorySystem, "system.log.level").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("info"))

	value, err := repo.Get(context.Background(), CategorySystem, "log.level")
	require.NoError(t, err)
	assert.Equal(t, "info", value)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingsRepoGetMissingReturnsEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(getSettingQuery)).
		WithArgs(CategorySystem, "system.missing").
		WillReturnError(sql.ErrNoRows)

	value, err := repo.Get(context.Background(), CategorySystem, "missing")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestSettingsRepoGetError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(getSettingQuery)).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.Get(context.Background(), CategorySystem, "k")
	require.Error(t, err)
}

func TestSettingsRepoGetMany(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(getManySettingsQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow("source.musicbrainz.enabled", "true").
			AddRow("source.netease.enabled", "false"))

	got, err := repo.GetMany(context.Background(), CategorySource,
		[]string{"musicbrainz.enabled", "netease.enabled", "missing"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"musicbrainz.enabled": "true", "netease.enabled": "false"}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingsRepoSet(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(upsertSettingQuery)).
		WithArgs("system.allow_registration", "true", CategorySystem).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Set(context.Background(), CategorySystem, "allow_registration", "true"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingsRepoSetMany(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(upsertSettingQuery)).
		WithArgs("source.musicbrainz.enabled", "true", CategorySource).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(upsertSettingQuery)).
		WithArgs("source.netease.enabled", "false", CategorySource).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.SetMany(context.Background(), CategorySource, map[string]string{
		"musicbrainz.enabled": "true",
		"netease.enabled":     "false",
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingsRepoSetManyEmpty(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewSettingsRepo(db)

	require.NoError(t, repo.SetMany(context.Background(), CategorySource, nil))
}
