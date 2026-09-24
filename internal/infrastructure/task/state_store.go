package task

import (
	"context"
	"time"

	"github.com/sonicore/server/internal/infrastructure/repository"
)

// TaskStateStore persists the enabled/disabled state of scheduled tasks in the
// dedicated task_state table. A task with no row is enabled by default.
type TaskStateStore struct {
	repo *repository.TaskStateRepo
}

func NewTaskStateStore(repo *repository.TaskStateRepo) *TaskStateStore {
	return &TaskStateStore{repo: repo}
}

func (s *TaskStateStore) GetEnabled(id string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.repo.GetEnabled(ctx, id)
}

func (s *TaskStateStore) SetEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.repo.SetEnabled(ctx, id, enabled)
}
