package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, user *domain.User) error {
	query := `INSERT INTO users (id, username, email, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.ExecContext(ctx, query,
		user.ID, user.Username, user.Email, user.PasswordHash,
		user.Role, user.CreatedAt, user.UpdatedAt)
	return err
}

func (r *UserRepo) Update(ctx context.Context, user *domain.User) error {
	query := `UPDATE users SET username=$2, email=$3, password_hash=$4, role=$5, updated_at=$6 WHERE id=$1`
	_, err := r.db.ExecContext(ctx, query,
		user.ID, user.Username, user.Email, user.PasswordHash,
		user.Role, user.UpdatedAt)
	return err
}

func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (r *UserRepo) ListAll(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, username, email, password_hash, role, created_at, updated_at FROM users ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash,
			&u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func scanUser(row interface{ Scan(dest ...interface{}) error }) (*domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash,
		&u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepo) FindByID(ctx context.Context, id string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = $1", id)
	return scanUser(row)
}

func (r *UserRepo) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = $1", username)
	return scanUser(row)
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE email = $1", email)
	return scanUser(row)
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
	return err
}
