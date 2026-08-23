package email

import (
	"context"
	"fmt"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
)

type Receiver struct {
	cfg config.EmailConfig
}

func NewReceiver(cfg config.EmailConfig) *Receiver {
	return &Receiver{cfg: cfg}
}

func (r *Receiver) ChannelType() domain.ChannelType { return domain.ChannelEmail }

func (r *Receiver) Start(ctx context.Context, handler port.ReceivedMessageHandler) error {
	if !r.cfg.IMAPEnabled {
		return nil
	}
	return fmt.Errorf("email receiver: not implemented yet")
}

func (r *Receiver) Stop(ctx context.Context) error {
	return nil
}
