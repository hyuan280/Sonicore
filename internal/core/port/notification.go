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
