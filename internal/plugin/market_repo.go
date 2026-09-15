package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RepoRow is one plugin_repos row. The JSON tags match the frontend
// PluginRepo type.
type RepoRow struct {
	URL       string     `json:"url"`
	Name      string     `json:"name"`
	Official  bool       `json:"official"`
	Enabled   bool       `json:"enabled"`
	AddedAt   time.Time  `json:"added_at"`
	LastSync  *time.Time `json:"last_sync,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

// ListRepos returns every configured repository ordered by official first,
// then name.
func (r *Repo) ListRepos(ctx context.Context) ([]RepoRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT url, name, official, enabled, added_at, last_sync, last_error
		FROM plugin_repos ORDER BY official DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list plugin repos: %w", err)
	}
	defer rows.Close()

	out := make([]RepoRow, 0)
	for rows.Next() {
		var p RepoRow
		if err := rows.Scan(&p.URL, &p.Name, &p.Official, &p.Enabled, &p.AddedAt, &p.LastSync, &p.LastError); err != nil {
			return nil, fmt.Errorf("scan plugin repo: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetRepo returns one repository row by URL.
func (r *Repo) GetRepo(ctx context.Context, url string) (*RepoRow, error) {
	var p RepoRow
	err := r.db.QueryRowContext(ctx, `
		SELECT url, name, official, enabled, added_at, last_sync, last_error
		FROM plugin_repos WHERE url = $1`, url).
		Scan(&p.URL, &p.Name, &p.Official, &p.Enabled, &p.AddedAt, &p.LastSync, &p.LastError)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get plugin repo: %w", err)
	}
	return &p, nil
}

// AddRepo stores a new repository. Duplicate URLs are ignored by the INSERT
// (ON CONFLICT DO NOTHING), but that is surfaced to the caller as ErrRepoExists
// instead of a silent no-op. The caller must fetch/validate the repo.json
// first to derive the repo name.
func (r *Repo) AddRepo(ctx context.Context, url, name string) error {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO plugin_repos (url, name, official)
		VALUES ($1, $2, FALSE)
		ON CONFLICT (url) DO NOTHING`, url, name)
	if err != nil {
		return fmt.Errorf("add plugin repo: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrRepoExists
	}
	return nil
}

// EnsureOfficialRepos seeds the built-in official repositories. Existing
// rows (including a manually removed official repo the admin re-added with
// different settings) are left untouched.
func (r *Repo) EnsureOfficialRepos(ctx context.Context) error {
	for _, o := range OfficialRepos {
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO plugin_repos (url, name, official)
			VALUES ($1, $2, TRUE)
			ON CONFLICT (url) DO NOTHING`, o.URL, o.Name); err != nil {
			return fmt.Errorf("seed official repo %s: %w", o.Name, err)
		}
	}
	return nil
}

// RemoveRepo deletes a repository row. The caller must refuse removing
// official repos before calling this.
func (r *Repo) RemoveRepo(ctx context.Context, url string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plugin_repos WHERE url = $1`, url)
	if err != nil {
		return fmt.Errorf("remove plugin repo: %w", err)
	}
	return nil
}

// SetRepoSyncState records the outcome of one sync pass for the repo.
func (r *Repo) SetRepoSyncState(ctx context.Context, url, lastError string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_repos SET last_sync = NOW(), last_error = $2 WHERE url = $1`, url, lastError)
	if err != nil {
		return fmt.Errorf("set repo sync state: %w", err)
	}
	return nil
}

// SetUpdateAvailable stores the update flag computed by a repo sync.
func (r *Repo) SetUpdateAvailable(ctx context.Context, id string, available bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET update_available = $2, updated_at = NOW() WHERE id = $1`,
		id, available)
	if err != nil {
		return fmt.Errorf("set plugin update_available: %w", err)
	}
	return nil
}

// SetVersion stores the plugin's new version after an update.
func (r *Repo) SetVersion(ctx context.Context, id, version string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET version = $2, updated_at = NOW() WHERE id = $1`,
		id, version)
	if err != nil {
		return fmt.Errorf("set plugin version: %w", err)
	}
	return nil
}
