package port

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
)

type Notifier interface {
	ChannelType() domain.ChannelType
	Name() string
	Enabled() bool
	Send(ctx context.Context, msg *domain.NotificationMessage) error
}

type MessageReceiver interface {
	ChannelType() domain.ChannelType
	Start(ctx context.Context, handler ReceivedMessageHandler) error
	Stop(ctx context.Context) error
}

type ReceivedMessageHandler func(ctx context.Context, msg *domain.NotificationMessage) error

type NotificationPrefRepository interface {
	GetCategoryPrefs(ctx context.Context) (map[domain.NotificationCategory]domain.CategoryPreference, error)
	UpdateCategoryPrefs(ctx context.Context, prefs map[domain.NotificationCategory]domain.CategoryPreference) error
	GetUserPrefs(ctx context.Context, userID string) (map[domain.NotificationCategory]domain.UserNotificationPref, error)
	GetUserPrefsByCategory(ctx context.Context, category domain.NotificationCategory) (map[string]domain.UserNotificationPref, error)
	UpsertUserPrefs(ctx context.Context, userID string, prefs []domain.UserNotificationPref) error
}
