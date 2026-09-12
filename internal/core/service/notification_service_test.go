package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/core/domain"
)

func TestSetChannelEnabledUnknownChannel(t *testing.T) {
	svc := &NotificationService{
		channelCtrl: map[domain.ChannelType]func(context.Context, bool) error{},
	}
	err := svc.SetChannelEnabled(context.Background(), "nope", true)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChannelNotFound)
}

func TestSetChannelEnabledCallsController(t *testing.T) {
	called := false
	svc := &NotificationService{
		channelCtrl: map[domain.ChannelType]func(context.Context, bool) error{
			"email": func(ctx context.Context, enabled bool) error {
				called = enabled
				return nil
			},
		},
	}
	require.NoError(t, svc.SetChannelEnabled(context.Background(), "email", true))
	require.True(t, called)

	svc.channelCtrl["email"] = func(ctx context.Context, enabled bool) error {
		return errors.New("boom")
	}
	require.Error(t, svc.SetChannelEnabled(context.Background(), "email", true))
}

type fakePrefRepo struct{}

func (f *fakePrefRepo) GetCategoryPrefs(ctx context.Context) (map[domain.NotificationCategory]domain.CategoryPreference, error) {
	return map[domain.NotificationCategory]domain.CategoryPreference{}, nil
}
func (f *fakePrefRepo) UpdateCategoryPrefs(ctx context.Context, prefs map[domain.NotificationCategory]domain.CategoryPreference) error {
	return nil
}
func (f *fakePrefRepo) GetUserPrefs(ctx context.Context, userID string) (map[domain.NotificationCategory]domain.UserNotificationPref, error) {
	return nil, nil
}
func (f *fakePrefRepo) GetUserPrefsByCategory(ctx context.Context, category domain.NotificationCategory) (map[string]domain.UserNotificationPref, error) {
	return nil, nil
}
func (f *fakePrefRepo) UpsertUserPrefs(ctx context.Context, userID string, prefs []domain.UserNotificationPref) error {
	return nil
}

// TestSetChannelEnabledEmailNilSettingsRepo pins the nil tolerance: the
// constructor accepts a nil settings repo (loadEmailConfig handles it), so
// the email channel controller must not dereference it either.
func TestSetChannelEnabledEmailNilSettingsRepo(t *testing.T) {
	svc := NewNotificationService(config.NotificationConfig{}, nil, nil, nil, &fakePrefRepo{})
	require.NotPanics(t, func() {
		require.NoError(t, svc.SetChannelEnabled(context.Background(), domain.ChannelEmail, true))
		require.NoError(t, svc.SetChannelEnabled(context.Background(), domain.ChannelEmail, false))
	})
}
