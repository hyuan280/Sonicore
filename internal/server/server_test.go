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

const getManyQuery = `SELECT key, value FROM server_settings WHERE category=$1 AND key = ANY($2)`

func TestCachedSettingsGetManyStablePresence(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	// An absent key must stay out of the map, including on the second
	// (cache-served) call — presence must not flip once the entry is cached.
	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))

	assert.Empty(t, c.getMany(repo, repository.CategorySource, "musicbrainz.enabled"))
	assert.Empty(t, c.getMany(repo, repository.CategorySource, "musicbrainz.enabled"), "presence must not flip after caching")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCachedSettingsGetManyTreatsEmptyAsAbsent(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow("source.musicbrainz.enabled", "true").
			AddRow("source.empty", ""))

	got := c.getMany(repo, repository.CategorySource, "musicbrainz.enabled", "empty")
	assert.Equal(t, map[string]string{"musicbrainz.enabled": "true"}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCachedSettingsGetManyFailureOmitsKeys(t *testing.T) {
	repo, mock := newMockSettingsRepo(t)
	c := newCachedSettings()

	mock.ExpectQuery(regexp.QuoteMeta(getManyQuery)).
		WillReturnError(errors.New("db down"))

	assert.Empty(t, c.getMany(repo, repository.CategorySource, "musicbrainz.enabled"))
	require.NoError(t, mock.ExpectationsWereMet())
}
