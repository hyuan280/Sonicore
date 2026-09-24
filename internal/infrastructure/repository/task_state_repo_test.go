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

func TestTaskStateRepoGetEnabledDefaultTrue(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTaskStateRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT enabled FROM task_state WHERE id=$1`)).
		WithArgs("scan").
		WillReturnError(sql.ErrNoRows)

	enabled, err := repo.GetEnabled(context.Background(), "scan")
	require.NoError(t, err)
	assert.True(t, enabled, "absent row means enabled")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskStateRepoGetEnabled(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTaskStateRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT enabled FROM task_state WHERE id=$1`)).
		WithArgs("scan").
		WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(false))

	enabled, err := repo.GetEnabled(context.Background(), "scan")
	require.NoError(t, err)
	assert.False(t, enabled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskStateRepoSetEnabled(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTaskStateRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO task_state (id, enabled, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (id) DO UPDATE SET enabled=$2, updated_at=NOW()`)).
		WithArgs("scan", false).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.SetEnabled(context.Background(), "scan", false))
	require.NoError(t, mock.ExpectationsWereMet())
}
