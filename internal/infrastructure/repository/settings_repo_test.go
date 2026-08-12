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

func TestSettingsRepoGet(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	rows := sqlmock.NewRows([]string{"value"}).AddRow("enabled")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
		WithArgs("registration").
		WillReturnRows(rows)

	value, err := repo.Get(context.Background(), "registration")
	require.NoError(t, err)
	assert.Equal(t, "enabled", value)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingsRepoGetMissingReturnsEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	value, err := repo.Get(context.Background(), "missing")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestSettingsRepoGetError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.Get(context.Background(), "k")
	require.Error(t, err)
}

func TestSettingsRepoSet(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewSettingsRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO server_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=$2`)).
		WithArgs("registration", "disabled").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Set(context.Background(), "registration", "disabled"))
	require.NoError(t, mock.ExpectationsWereMet())
}
