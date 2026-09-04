package domain

import "time"

type NotificationType string

const (
	NotifScanComplete NotificationType = "scan_complete"
	NotifScanFailed   NotificationType = "scan_failed"
	NotifSystemError  NotificationType = "system_error"
)

type ChannelType string

const (
	ChannelEmail ChannelType = "email"
)

type NotificationCategory string

const (
	CategoryScan   NotificationCategory = "scan"
	CategorySystem NotificationCategory = "system"
)

var CategoryNotificationTypes = map[NotificationCategory][]NotificationType{
	CategoryScan:   {NotifScanComplete, NotifScanFailed},
	CategorySystem: {NotifSystemError},
}

var TypeCategory = map[NotificationType]NotificationCategory{}

func init() {
	for cat, types := range CategoryNotificationTypes {
		for _, t := range types {
			TypeCategory[t] = cat
		}
	}
}

type RecipientRole string

const (
	RecipientOperator RecipientRole = "operator"
	RecipientAdmin    RecipientRole = "admin"
	RecipientAll      RecipientRole = "all"
)

// CategoryPreference is the server-wide default configuration for a category.
type CategoryPreference struct {
	Roles    []string `json:"roles"`
	Channels []string `json:"channels"`
}

// UserNotificationPref is a user's override for a category.
// nil Roles/Channels means inherit from the server default.
type UserNotificationPref struct {
	Category NotificationCategory `json:"category"`
	Enabled  bool                 `json:"enabled"`
	Roles    *[]string            `json:"roles,omitempty"`
	Channels *[]string            `json:"channels,omitempty"`
}

type NotificationMessage struct {
	ID        string
	Type      NotificationType
	Channel   ChannelType
	To        []string
	Subject   string
	TextBody  string
	HTMLBody  string
	Metadata  map[string]any
	CreatedAt time.Time
}
