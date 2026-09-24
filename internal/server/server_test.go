package server

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/infrastructure/repository"
)

func newMockSettingsRepo(t *testing.T) (*repository.SettingsRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return repository.NewSettingsRepo(db), mock
}

const getManyQuery = `SELECT key, value FROM server_settings WHERE key = ANY($1)`

func TestCachedSettingsGetManyStablePresence(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	// An absent key must stay out of the map, including on the second
	// (cache-served) call — presence must not flip once the entry is cached.
	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))

	assert.Empty(t, c.getMany(repo, "missing"))
	assert.Empty(t, c.getMany(repo, "missing"), "presence must not flip after caching")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCachedSettingsGetManyTreatsEmptyAsAbsent(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow("enabled", "true").
			AddRow("empty", ""))

	got := c.getMany(repo, "enabled", "empty")
	assert.Equal(t, map[string]string{"enabled": "true"}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCachedSettingsGetManyFailureOmitsKeys(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnError(errors.New("db down"))

	assert.Empty(t, c.getMany(repo, "missing"))
	require.NoError(t, mock.ExpectationsWereMet())
}
