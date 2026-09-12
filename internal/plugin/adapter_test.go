package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"

	"github.com/sonicore/server/internal/core/domain"
)

type fakeNotifierClient struct {
	req  *gen.SendRequest
	err  error
	resp *gen.SendResponse
}

func (f *fakeNotifierClient) Send(
	ctx context.Context,
	in *gen.SendRequest,
	opts ...grpc.CallOption,
) (*gen.SendResponse, error) {
	f.req = in
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &gen.SendResponse{}, nil
}

func TestToProtoMessage(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p := toProtoMessage(&domain.NotificationMessage{
		ID:        "n1",
		Type:      domain.NotifScanComplete,
		Channel:   "notifydemo",
		To:        []string{"a@b.c"},
		Subject:   "S",
		TextBody:  "T",
		HTMLBody:  "H",
		Metadata:  map[string]any{"k": 1, "drop": nil},
		CreatedAt: created,
	})
	require.Equal(t, "n1", p.Id)
	require.Equal(t, "scan_complete", p.Type)
	require.Equal(t, "notifydemo", p.Channel)
	require.Equal(t, []string{"a@b.c"}, p.To)
	require.Equal(t, "S", p.Subject)
	require.Equal(t, "T", p.TextBody)
	require.Equal(t, "H", p.HtmlBody)
	require.Equal(t, map[string]string{"k": "1"}, p.Metadata)
	require.Equal(t, "2026-01-02T03:04:05Z", p.CreatedAt)
}

func TestNotifierAdapterBasics(t *testing.T) {
	a := &notifierAdapter{name: "notifydemo", running: func() bool { return true }}
	require.Equal(t, domain.ChannelType("notifydemo"), a.ChannelType())
	require.Equal(t, "notifydemo", a.Name())
	require.True(t, a.Enabled())

	a.running = func() bool { return false }
	require.False(t, a.Enabled())
}

func TestNotifierAdapterSend(t *testing.T) {
	client := &fakeNotifierClient{}
	a := &notifierAdapter{name: "notifydemo", client: client, running: func() bool { return true }}

	err := a.Send(context.Background(), &domain.NotificationMessage{
		ID: "n1", Type: domain.NotifScanFailed, CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.ABIVersion, client.req.ApiVersion)
	require.Equal(t, "n1", client.req.Message.Id)
	require.Equal(t, "scan_failed", client.req.Message.Type)
}

func TestNotifierAdapterSendErrors(t *testing.T) {
	msg := &domain.NotificationMessage{ID: "n1", CreatedAt: time.Now()}

	client := &fakeNotifierClient{err: errors.New("rpc down")}
	a := &notifierAdapter{name: "demo", client: client, running: func() bool { return false }}
	err := a.Send(context.Background(), msg)
	require.ErrorContains(t, err, "plugin demo send")
	require.ErrorContains(t, err, "rpc down")

	client = &fakeNotifierClient{resp: &gen.SendResponse{Error: "rejected"}}
	a.client = client
	err = a.Send(context.Background(), msg)
	require.EqualError(t, err, "plugin demo send: rejected")
}
