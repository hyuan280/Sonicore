package repository

import (
	"context"
	"database/sql"
)

// TaskStateRepo persists the user-controlled enabled/disabled state of the
// central task scheduler. A task with no row is enabled by default.
type TaskStateRepo struct {
	db *sql.DB
}

func NewTaskStateRepo(db *sql.DB) *TaskStateRepo {
	return &TaskStateRepo{db: db}
}

// GetEnabled reports whether a task is enabled. An absent row means enabled.
func (r *TaskStateRepo) GetEnabled(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := r.db.QueryRowContext(ctx,
		"SELECT enabled FROM task_state WHERE id=$1", id).Scan(&enabled)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return enabled, err
}

func (r *TaskStateRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO task_state (id, enabled, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (id) DO UPDATE SET enabled=$2, updated_at=NOW()`,
		id, enabled)
	return err
}
