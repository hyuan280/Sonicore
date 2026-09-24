package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/lib/pq"
)

// Settings categories. Every server setting belongs to exactly one category,
// which is both the first segment of its key ("category.name") and a column on
// server_settings. Keeping the two in sync is enforced by this repo.
const (
	CategorySystem       = "system"
	CategorySource       = "source"
	CategoryNetwork      = "network"
	CategoryNotification = "notification"
)

// ErrSettingsCategoryRequired is returned when a settings access omits the
// category. Failing closed keeps one category from reading or writing another
// category's keys.
var ErrSettingsCategoryRequired = errors.New("settings: category is required")

type SettingsRepo struct {
	db *sql.DB
}

func NewSettingsRepo(db *sql.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

// settingsKey composes the stored key from its category and short name. The
// category prefix is generated here so callers only ever pass the short name
// ("musicbrainz.enabled") and can never desync it from the category column.
func settingsKey(category, name string) string {
	return category + "." + name
}

// Get returns the value for a (category, name). A missing key yields ("", nil).
// An empty category is rejected so a lookup can never cross categories.
func (r *SettingsRepo) Get(ctx context.Context, category, name string) (string, error) {
	if category == "" {
		return "", ErrSettingsCategoryRequired
	}
	var value string
	err := r.db.QueryRowContext(ctx,
		"SELECT value FROM server_settings WHERE category=$1 AND key=$2",
		category, settingsKey(category, name)).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GetMany reads several names within one category in a single query. The
// returned map is keyed by the short name (not the stored key); an absent name
// is simply missing from the map.
func (r *SettingsRepo) GetMany(ctx context.Context, category string, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	if category == "" {
		return out, ErrSettingsCategoryRequired
	}
	if len(names) == 0 {
		return out, nil
	}
	keys := make([]string, len(names))
	shortByKey := make(map[string]string, len(names))
	for i, name := range names {
		k := settingsKey(category, name)
		keys[i] = k
		shortByKey[k] = name
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM server_settings WHERE category=$1 AND key = ANY($2)`,
		category, pq.Array(keys))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		if name, ok := shortByKey[k]; ok {
			out[name] = v
		}
	}
	return out, rows.Err()
}

// Set upserts one setting within a category.
func (r *SettingsRepo) Set(ctx context.Context, category, name, value string) error {
	if category == "" {
		return ErrSettingsCategoryRequired
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO server_settings (key, value, category) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET value=$2, category=$3`,
		settingsKey(category, name), value, category)
	return err
}

// SetMany persists a batch of settings in one transaction so a failed write
// rolls back every key (an admin settings batch must never leave a partial
// state behind). All values belong to the same category.
func (r *SettingsRepo) SetMany(ctx context.Context, category string, values map[string]string) error {
	if category == "" {
		return ErrSettingsCategoryRequired
	}
	if len(values) == 0 {
		return nil
	}
	// Deterministic write order (map iteration is random).
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, name := range names {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO server_settings (key, value, category) VALUES ($1, $2, $3)
			 ON CONFLICT (key) DO UPDATE SET value=$2, category=$3`,
			settingsKey(category, name), values[name], category); err != nil {
			return err
		}
	}
	return tx.Commit()
}
