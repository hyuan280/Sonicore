package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/logger"
	emailpkg "github.com/sonicore/server/internal/infrastructure/notification/email"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/secrets"
)

type NotificationService struct {
	cfg          config.NotificationConfig
	notifiers    map[domain.ChannelType]port.Notifier
	users        port.UserRepository
	settingsRepo *repository.SettingsRepo
	emailSender  *emailpkg.Sender
	enc          *secrets.Encryptor
	mu           sync.RWMutex
}

func NewNotificationService(cfg config.NotificationConfig, users port.UserRepository, settingsRepo *repository.SettingsRepo, enc *secrets.Encryptor) *NotificationService {
	svc := &NotificationService{
		cfg:          cfg,
		notifiers:    make(map[domain.ChannelType]port.Notifier),
		users:        users,
		settingsRepo: settingsRepo,
		enc:          enc,
	}
	emailCfg := svc.loadEmailConfig(context.Background())
	svc.emailSender = emailpkg.NewSender(emailCfg)
	svc.notifiers[domain.ChannelEmail] = svc.emailSender
	return svc
}

func (s *NotificationService) RegisterNotifier(n port.Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifiers[n.ChannelType()] = n
}

func (s *NotificationService) Channels() []ChannelInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ChannelInfo
	for _, n := range s.notifiers {
		out = append(out, ChannelInfo{
			Type:    n.ChannelType(),
			Name:    n.Name(),
			Enabled: n.Enabled(),
		})
	}
	return out
}

type ChannelInfo struct {
	Type    domain.ChannelType `json:"type"`
	Name    string             `json:"name"`
	Enabled bool               `json:"enabled"`
}

func (s *NotificationService) EmailConfig(ctx context.Context) config.EmailConfig {
	return s.loadEmailConfig(ctx)
}

func (s *NotificationService) ReloadEmailConfig(ctx context.Context) {
	cfg := s.loadEmailConfig(ctx)
	s.mu.Lock()
	s.emailSender = emailpkg.NewSender(cfg)
	s.notifiers[domain.ChannelEmail] = s.emailSender
	s.mu.Unlock()
}

func mergeEmailConfig(cfg *config.EmailConfig, m map[string]any) {
	if v, ok := m["smtp_host"].(string); ok && v != "" {
		cfg.SMTPHost = v
	}
	if v, ok := m["smtp_port"].(string); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
			cfg.SMTPPort = n
		}
	}
	if v, ok := m["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := m["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := m["from_address"].(string); ok && v != "" {
		cfg.FromAddress = v
	}
	if v, ok := m["from_name"].(string); ok {
		cfg.FromName = v
	}
	if v, ok := m["tls"].(bool); ok {
		cfg.TLS = v
	}
}

func (s *NotificationService) loadEmailConfig(ctx context.Context) config.EmailConfig {
	cfg := s.cfg.Email
	if s.settingsRepo == nil {
		return cfg
	}
	keys := []string{
		"notification_email_enabled",
		"notification_email_smtp_host",
		"notification_email_smtp_port",
		"notification_email_username",
		"notification_email_password",
		"notification_email_from_address",
		"notification_email_from_name",
		"notification_email_tls",
	}
	values, err := s.settingsRepo.GetMany(ctx, keys)
	if err != nil {
		logger.Error("[notification] failed to load email config from db: %v, using defaults", err)
		return cfg
	}
	if v := values["notification_email_enabled"]; v != "" {
		cfg.Enabled = v == "true"
	}
	if v := values["notification_email_smtp_host"]; v != "" {
		cfg.SMTPHost = v
	}
	if v := values["notification_email_smtp_port"]; v != "" {
		if n, err := strconv.Atoi(v); err != nil {
			logger.Warn("[notification] invalid smtp_port %q: %v", v, err)
		} else {
			cfg.SMTPPort = n
		}
	}
	if v := values["notification_email_username"]; v != "" {
		cfg.Username = v
	}
	if v := values["notification_email_password"]; v != "" {
		if s.enc != nil {
			if d, err := s.enc.Decrypt(v); err == nil {
				cfg.Password = d
			} else {
				logger.Warn("[notification] failed to decrypt stored password, keeping existing: %v", err)
			}
		} else {
			cfg.Password = v
		}
	}
	if v := values["notification_email_from_address"]; v != "" {
		cfg.FromAddress = v
	}
	if v := values["notification_email_from_name"]; v != "" {
		cfg.FromName = v
	}
	if v := values["notification_email_tls"]; v != "" {
		cfg.TLS = v == "true"
	}
	return cfg
}

func (s *NotificationService) Send(ctx context.Context, msg *domain.NotificationMessage) error {
	s.mu.RLock()
	notifier, ok := s.notifiers[msg.Channel]
	s.mu.RUnlock()
	if !ok {
		err := fmt.Errorf("no notifier for channel %q", msg.Channel)
		logger.Error("[notification] send failed: %v", err)
		return err
	}
	if !notifier.Enabled() {
		err := fmt.Errorf("notifier %q is disabled", msg.Channel)
		logger.Error("[notification] send failed: %v", err)
		return err
	}
	if err := notifier.Send(ctx, msg); err != nil {
		logger.Error("[notification] send via %s failed: %v", notifier.Name(), err)
		return err
	}
	return nil
}

func (s *NotificationService) NotifyScanComplete(ctx context.Context, job *domain.ScanJob, libraryName string) error {
	admins, err := s.users.FindByRole(ctx, domain.RoleSuperAdmin)
	if err != nil {
		return fmt.Errorf("find admins: %w", err)
	}
	if len(admins) == 0 {
		return nil
	}

	to := make([]string, 0, len(admins))
	for _, u := range admins {
		if u.Email != "" {
			to = append(to, u.Email)
		}
	}
	if len(to) == 0 {
		return nil
	}

	errorsStr := job.Errors
	if errorsStr == "" {
		errorsStr = "0"
	}
	msg := &domain.NotificationMessage{
		ID:        domain.NewID(),
		Type:      domain.NotifScanComplete,
		Channel:   domain.ChannelEmail,
		To:        to,
		Subject:   fmt.Sprintf("Scan Complete — %s", libraryName),
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			"LibraryName":   libraryName,
			"NewTracks":     job.NewTracks,
			"UpdatedTracks": job.UpdatedTracks,
			"DeletedTracks": job.DeletedTracks,
			"Errors":        errorsStr,
		},
	}

	return s.Send(ctx, msg)
}

func (s *NotificationService) NotifyScanFailed(ctx context.Context, libraryName, errorMsg string) error {
	admins, err := s.users.FindByRole(ctx, domain.RoleSuperAdmin)
	if err != nil {
		return fmt.Errorf("find admins: %w", err)
	}
	if len(admins) == 0 {
		return nil
	}

	to := make([]string, 0, len(admins))
	for _, u := range admins {
		if u.Email != "" {
			to = append(to, u.Email)
		}
	}
	if len(to) == 0 {
		return nil
	}

	msg := &domain.NotificationMessage{
		ID:        domain.NewID(),
		Type:      domain.NotifScanFailed,
		Channel:   domain.ChannelEmail,
		To:        to,
		Subject:   fmt.Sprintf("Scan Failed — %s", libraryName),
		CreatedAt: time.Now(),
		Metadata: map[string]any{
			"LibraryName": libraryName,
			"Error":       errorMsg,
		},
	}

	return s.Send(ctx, msg)
}

func buildTestMessage(to []string, channel domain.ChannelType) *domain.NotificationMessage {
	return &domain.NotificationMessage{
		ID:        domain.NewID(),
		Type:      domain.NotifSystemError,
		Channel:   channel,
		To:        to,
		Subject:   "Sonicore — Test Notification",
		TextBody:  "This is a test notification from Sonicore.\n\nIf you received this, email delivery is working correctly.",
		HTMLBody:  "<h2>Sonicore — Test Notification</h2><p>This is a test notification from Sonicore.</p><p>If you received this, email delivery is working correctly.</p>",
		CreatedAt: time.Now(),
	}
}

func (s *NotificationService) SendTest(ctx context.Context, channel domain.ChannelType, to []string, config map[string]any) error {
	switch channel {
	case domain.ChannelEmail:
		cfg := s.loadEmailConfig(ctx)
		mergeEmailConfig(&cfg, config)
		sender := emailpkg.NewSender(cfg)
		return sender.Send(ctx, buildTestMessage(to, channel))
	default:
		s.mu.RLock()
		notifier, ok := s.notifiers[channel]
		s.mu.RUnlock()
		if !ok {
			return fmt.Errorf("no notifier for channel %q", channel)
		}
		return notifier.Send(ctx, buildTestMessage(to, channel))
	}
}
