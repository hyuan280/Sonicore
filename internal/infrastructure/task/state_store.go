package task

import (
	"context"
	"strconv"
	"time"

	"github.com/sonicore/server/internal/infrastructure/repository"
)

// SettingsStateStore persists enabled/disabled state in server_settings
// under "task_enabled_<id>". An unset key means the default (enabled).
type SettingsStateStore struct {
	repo *repository.SettingsRepo
}

func NewSettingsStateStore(repo *repository.SettingsRepo) *SettingsStateStore {
	return &SettingsStateStore{repo: repo}
}

func (s *SettingsStateStore) key(id string) string {
	return "task_enabled_" + id
}

func (s *SettingsStateStore) GetEnabled(id string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := s.repo.Get(ctx, s.key(id))
	if err != nil {
		return false, err
	}
	if v == "" {
		return true, nil
	}
	return v == "true", nil
}

func (s *SettingsStateStore) SetEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.repo.Set(ctx, s.key(id), strconv.FormatBool(enabled))
}
