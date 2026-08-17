package repository

import (
	"context"
	"database/sql"
	"sort"

	"github.com/lib/pq"
)

type SettingsRepo struct {
	db *sql.DB
}

func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, "SELECT value FROM server_settings WHERE key=$1", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GetMany reads several keys in one query (an absent key is simply missing
// from the map). Used by the registry build so a cold cache does not pay one
// round-trip per setting.
func (r *SettingsRepo) GetMany(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM server_settings WHERE key = ANY($1)`, pq.Array(keys))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (r *SettingsRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO server_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=$2",
		key, value)
	return err
}

// SetMany persists a batch of settings in a single transaction so a failed
// write rolls back every key (an admin settings batch must never leave a
// partial state behind).
func (r *SettingsRepo) SetMany(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	// Deterministic write order (map iteration is random).
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, k := range keys {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO server_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=$2",
			k, values[k]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
