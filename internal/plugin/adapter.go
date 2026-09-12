package plugin

import (
	"context"
	"fmt"
	"time"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"

	"github.com/sonicore/server/internal/core/domain"
)

// notifierAdapter exposes a plugin's NotifierService as a port.Notifier.
type notifierAdapter struct {
	name    string
	client  gen.NotifierServiceClient
	running func() bool
}

func (a *notifierAdapter) ChannelType() domain.ChannelType { return domain.ChannelType(a.name) }

func (a *notifierAdapter) Name() string { return a.name }

func (a *notifierAdapter) Enabled() bool { return a.running() }

func (a *notifierAdapter) Send(ctx context.Context, msg *domain.NotificationMessage) error {
	resp, err := a.client.Send(ctx, &gen.SendRequest{
		ApiVersion: pluginsdk.ABIVersion,
		Message:    toProtoMessage(msg),
	})
	if err != nil {
		return fmt.Errorf("plugin %s send: %w", a.name, err)
	}
	if resp.Error != "" {
		// Keep the plugin name for context, symmetric with the RPC error
		// path: callers (e.g. SendTest) surface this to clients and need
		// to know which plugin rejected the send.
		return fmt.Errorf("plugin %s send: %s", a.name, resp.Error)
	}
	return nil
}

// toProtoMessage converts a domain notification message to the plugin ABI
// message. Metadata is flattened to string values.
func toProtoMessage(msg *domain.NotificationMessage) *gen.NotificationMessage {
	metadata := make(map[string]string, len(msg.Metadata))
	for k, v := range msg.Metadata {
		if v == nil {
			continue
		}
		metadata[k] = fmt.Sprint(v)
	}
	return &gen.NotificationMessage{
		Id:        msg.ID,
		Type:      string(msg.Type),
		Channel:   string(msg.Channel),
		To:        msg.To,
		Subject:   msg.Subject,
		TextBody:  msg.TextBody,
		HtmlBody:  msg.HTMLBody,
		Metadata:  metadata,
		CreatedAt: msg.CreatedAt.Format(time.RFC3339),
	}
}
