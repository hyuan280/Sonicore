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
