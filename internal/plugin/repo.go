// Package plugin implements the host side of the Sonicore plugin system:
// discovery of plugin directories under the data dir, go-plugin process
// management, the host services plugins call back into (config/log/status)
// and adapters that expose plugins through the existing port interfaces
// (currently port.Notifier).
package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

// Sentinel errors let callers distinguish client-side validation failures
// (map to 400/InvalidArgument) from storage failures (500/Internal).
var (
	// ErrInvalidConfigJSON marks a config payload that is not valid JSON.
	ErrInvalidConfigJSON = errors.New("invalid config JSON")
	// ErrInvalidDataJSON marks a plugin-data value that is not valid JSON.
	ErrInvalidDataJSON = errors.New("invalid data JSON")
)

// Status values stored in plugin_instances.status.
const (
	StatusStopped  = "stopped"
	StatusOK       = "ok"
	StatusError    = "error"
	StatusDisabled = "disabled"
)

// Instance is a plugin row from plugin_instances. The JSON tags match the
// frontend PluginInstance type. Author and History are not persisted here:
// they are merged from the manifest by Manager.List/ListUninstalled. Dir is
// host-internal (never sent to the frontend).
type Instance struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Version     string                   `json:"version"`
	Source      string                   `json:"source"`
	Author      string                   `json:"author,omitempty"`
	Enabled     bool                     `json:"enabled"`
	Status      string                   `json:"status"`
	StatusMsg   string                   `json:"status_msg,omitempty"`
	HasPage     bool                     `json:"has_page"`
	Installed   bool                     `json:"installed,omitempty"`
	History     []pluginsdk.HistoryEntry `json:"history,omitempty"`
	Dir         string                   `json:"-"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// Repo persists plugin instances and their configuration.
type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert inserts the instance on first discovery and refreshes the static
// manifest fields afterwards. Lifecycle state (installed/enabled/status) is
// preserved across restarts. New discovery rows are inserted as NOT
// installed (installed=false): the admin installs plugins explicitly.
func (r *Repo) Upsert(ctx context.Context, inst *Instance) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO plugin_instances (id, name, version, description, source, dir, installed, installed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			version = EXCLUDED.version,
			description = EXCLUDED.description,
			source = EXCLUDED.source,
			dir = EXCLUDED.dir,
			updated_at = NOW()`,
		inst.ID, inst.Name, inst.Version, inst.Description, inst.Source, inst.Dir)
	if err != nil {
		return fmt.Errorf("upsert plugin instance: %w", err)
	}
	return nil
}

// List returns the installed plugin rows (soft-uninstalled plugins are
// excluded; see ListUninstalled).
func (r *Repo) List(ctx context.Context) ([]Instance, error) {
	return r.list(ctx, true)
}

// ListUninstalled returns the plugins found on disk but not installed.
func (r *Repo) ListUninstalled(ctx context.Context) ([]Instance, error) {
	return r.list(ctx, false)
}

func (r *Repo) list(ctx context.Context, installed bool) ([]Instance, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, version, description, source, enabled, status, status_msg, has_page, installed, dir, updated_at
		FROM plugin_instances WHERE installed = $1 ORDER BY name`, installed)
	if err != nil {
		return nil, fmt.Errorf("list plugin instances: %w", err)
	}
	defer rows.Close()

	out := make([]Instance, 0)
	for rows.Next() {
		var i Instance
		if err := rows.Scan(&i.ID, &i.Name, &i.Version, &i.Description,
			&i.Source, &i.Enabled, &i.Status, &i.StatusMsg, &i.HasPage, &i.Installed, &i.Dir, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan plugin instance: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SetHasPage stores whether the plugin provides a data page (probed once
// per plugin load so the frontend needs no per-plugin get_page calls).
func (r *Repo) SetHasPage(ctx context.Context, id string, hasPage bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET has_page = $2, updated_at = NOW() WHERE id = $1`,
		id, hasPage)
	if err != nil {
		return fmt.Errorf("set plugin has_page: %w", err)
	}
	return nil
}

func (r *Repo) UpdateStatus(ctx context.Context, id, status, msg string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET status = $2, status_msg = $3, updated_at = NOW() WHERE id = $1`,
		id, status, msg)
	if err != nil {
		return fmt.Errorf("update plugin status: %w", err)
	}
	return nil
}

func (r *Repo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET enabled = $2, updated_at = NOW() WHERE id = $1`,
		id, enabled)
	if err != nil {
		return fmt.Errorf("set plugin enabled: %w", err)
	}
	return nil
}

// SetInstalled stores the installed flag (soft install/uninstall).
func (r *Repo) SetInstalled(ctx context.Context, id string, installed bool) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE plugin_instances SET installed = $2, updated_at = NOW() WHERE id = $1`,
		id, installed)
	if err != nil {
		return fmt.Errorf("set plugin installed: %w", err)
	}
	return nil
}

// GetRuntimeState returns the persisted lifecycle state plus the actual
// plugin directory name for one plugin.
func (r *Repo) GetRuntimeState(ctx context.Context, id string) (enabled, installed bool, dir string, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT enabled, installed, dir FROM plugin_instances WHERE id = $1`, id).
		Scan(&enabled, &installed, &dir)
	if err != nil {
		return false, false, "", fmt.Errorf("get plugin runtime state: %w", err)
	}
	return enabled, installed, dir, nil
}

func (r *Repo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM plugin_instances WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete plugin instance: %w", err)
	}
	return nil
}

// GetConfig returns the stored JSON configuration ("" when none).
func (r *Repo) GetConfig(ctx context.Context, id string) (string, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT config FROM plugin_config WHERE plugin_id = $1`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get plugin config: %w", err)
	}
	return string(raw), nil
}

// SetConfig stores the plugin configuration as raw JSON.
func (r *Repo) SetConfig(ctx context.Context, id string, configJSON string) error {
	if !json.Valid([]byte(configJSON)) {
		return fmt.Errorf("%w for plugin %s", ErrInvalidConfigJSON, id)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO plugin_config (plugin_id, config, updated_at)
		VALUES ($1, $2::jsonb, NOW())
		ON CONFLICT (plugin_id) DO UPDATE SET config = EXCLUDED.config, updated_at = NOW()`,
		id, configJSON)
	if err != nil {
		return fmt.Errorf("set plugin config: %w", err)
	}
	return nil
}

// GetData returns the stored value for the key as raw JSON ("" when the key
// does not exist).
func (r *Repo) GetData(ctx context.Context, id, key string) (string, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM plugin_data WHERE plugin_id = $1 AND key = $2`, id, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get plugin data: %w", err)
	}
	return string(raw), nil
}

// SetData stores a raw JSON value under the plugin's key.
func (r *Repo) SetData(ctx context.Context, id, key, valueJSON string) error {
	if !json.Valid([]byte(valueJSON)) {
		return fmt.Errorf("%w for plugin %s key %s", ErrInvalidDataJSON, id, key)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO plugin_data (plugin_id, key, value, updated_at)
		VALUES ($1, $2, $3::jsonb, NOW())
		ON CONFLICT (plugin_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		id, key, valueJSON)
	if err != nil {
		return fmt.Errorf("set plugin data: %w", err)
	}
	return nil
}

// DeleteData removes the plugin's key. Deleting a missing key is not an
// error.
func (r *Repo) DeleteData(ctx context.Context, id, key string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM plugin_data WHERE plugin_id = $1 AND key = $2`, id, key)
	if err != nil {
		return fmt.Errorf("delete plugin data: %w", err)
	}
	return nil
}

func logDBError(ctx context.Context, r statusUpdater, id, status, msg string) {
	if err := r.UpdateStatus(ctx, id, status, msg); err != nil {
		logger.Warn("[plugin] failed to persist status for %s: %v", id, err)
	}
}

// statusUpdater is the surface logDBError needs; *Repo and the host
// service's pluginRepo both satisfy it.
type statusUpdater interface {
	UpdateStatus(ctx context.Context, id, status, msg string) error
}
