package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func testUser() *domain.User {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	return &domain.User{
		ID:           "u-001",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash",
		Role:         domain.RoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestUserRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (id, username, email, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`)).
		WithArgs(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), user))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoCreateError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (id, username, email, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`)).
		WillReturnError(sql.ErrConnDone)

	require.Error(t, repo.Create(context.Background(), user))
}

func TestUserRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()
	user.Username = "bob"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET username=$2, email=$3, password_hash=$4, role=$5, updated_at=$6 WHERE id=$1`)).
		WithArgs(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), user))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoCount(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	count, err := repo.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 42, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoListAll(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)

	user := testUser()
	other := testUser()
	other.ID = "u-002"
	other.Username = "bob"

	rows := sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt).
		AddRow(other.ID, other.Username, other.Email, other.PasswordHash, other.Role, other.CreatedAt, other.UpdatedAt)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users ORDER BY created_at ASC`).
		WillReturnRows(rows)

	users, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, user.ID, users[0].ID)
	assert.Equal(t, other.ID, users[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()

	rows := sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "u-001")
	require.NoError(t, err)
	assert.Equal(t, user, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoFindByIDNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestUserRepoFindByUsername(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()

	rows := sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("alice").
		WillReturnRows(rows)

	got, err := repo.FindByUsername(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, user, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoFindByEmail(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)
	user := testUser()

	rows := sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow(user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE email = \$1`).
		WithArgs("alice@example.com").
		WillReturnRows(rows)

	got, err := repo.FindByEmail(context.Background(), "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, user, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepoFindScanError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)

	rows := sqlmock.NewRows([]string{"id", "username"}).AddRow("u-001", "alice")
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(rows)

	_, err := repo.FindByID(context.Background(), "u-001")
	require.Error(t, err)
}

func TestUserRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserRepo(db)

	mock.ExpectExec(`DELETE FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "u-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}
