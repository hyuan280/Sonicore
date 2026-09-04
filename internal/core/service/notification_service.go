package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	cfg           config.NotificationConfig
	notifiers     map[domain.ChannelType]port.Notifier
	users         port.UserRepository
	settingsRepo  *repository.SettingsRepo
	prefRepo      port.NotificationPrefRepository
	emailSender   *emailpkg.Sender
	enc           *secrets.Encryptor
	categoryPrefs map[domain.NotificationCategory]domain.CategoryPreference
	mu            sync.RWMutex
}

func NewNotificationService(cfg config.NotificationConfig, users port.UserRepository, settingsRepo *repository.SettingsRepo, enc *secrets.Encryptor, prefRepo port.NotificationPrefRepository) *NotificationService {
	svc := &NotificationService{
		cfg:          cfg,
		notifiers:    make(map[domain.ChannelType]port.Notifier),
		users:        users,
		settingsRepo: settingsRepo,
		enc:          enc,
		prefRepo:     prefRepo,
	}
	emailCfg := svc.loadEmailConfig(context.Background())
	svc.emailSender = emailpkg.NewSender(emailCfg)
	svc.notifiers[domain.ChannelEmail] = svc.emailSender
	svc.categoryPrefs = make(map[domain.NotificationCategory]domain.CategoryPreference)
	if err := svc.LoadCategoryPrefs(context.Background()); err != nil {
		logger.Error("[notification] failed to load category prefs, using built-in defaults: %v", err)
		svc.categoryPrefs = defaultCategoryPrefs()
	}
	return svc
}

// defaultCategoryPrefs mirrors the migration seed in db.go so notifications
// keep working when the DB is unavailable at startup. Any later successful
// LoadCategoryPrefs (e.g. via ReloadEmailConfig or UpdateCategoryPrefs)
// replaces these with persisted values.
func defaultCategoryPrefs() map[domain.NotificationCategory]domain.CategoryPreference {
	return map[domain.NotificationCategory]domain.CategoryPreference{
		domain.CategoryScan: {
			Roles:    []string{string(domain.RecipientOperator), string(domain.RecipientAdmin)},
			Channels: []string{string(domain.ChannelEmail)},
		},
		domain.CategorySystem: {
			Roles:    []string{string(domain.RecipientAdmin)},
			Channels: []string{string(domain.ChannelEmail)},
		},
	}
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
		if !n.Enabled() {
			continue
		}
		out = append(out, ChannelInfo{
			Type:    n.ChannelType(),
			Name:    n.Name(),
			Enabled: true,
		})
	}
	return out
}

type ChannelInfo struct {
	Type    domain.ChannelType `json:"type"`
	Name    string             `json:"name"`
	Enabled bool               `json:"enabled"`
}

func (s *NotificationService) LoadCategoryPrefs(ctx context.Context) error {
	prefs, err := s.prefRepo.GetCategoryPrefs(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.categoryPrefs = prefs
	s.mu.Unlock()
	return nil
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
	if err := s.LoadCategoryPrefs(ctx); err != nil {
		logger.Error("[notification] reload category prefs failed: %v", err)
	}
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

// recipient is a resolved notification target: a user ID plus their email.
type recipient struct {
	ID    string
	Email string
}

func (s *NotificationService) resolveRecipients(ctx context.Context, pref domain.CategoryPreference, operatorID string) ([]recipient, error) {
	// byID dedupes users that match several roles and maps them to email
	// directly (FindByRole/ListAll already return full users).
	byID := make(map[string]string)
	addUsers := func(users []domain.User) {
		for _, u := range users {
			if u.Email != "" {
				byID[u.ID] = u.Email
			}
		}
	}
	for _, r := range pref.Roles {
		switch domain.RecipientRole(r) {
		case domain.RecipientOperator:
			if operatorID == "" {
				continue
			}
			u, err := s.users.FindByID(ctx, operatorID)
			if err != nil {
				return nil, fmt.Errorf("find operator %s: %w", operatorID, err)
			}
			if u != nil && u.Email != "" {
				byID[u.ID] = u.Email
			}
		case domain.RecipientAdmin:
			for _, role := range []domain.Role{domain.RoleSuperAdmin, domain.RoleAdmin} {
				users, err := s.users.FindByRole(ctx, role)
				if err != nil {
					return nil, fmt.Errorf("find by role %s: %w", role, err)
				}
				addUsers(users)
			}
		case domain.RecipientAll:
			users, err := s.users.ListAll(ctx)
			if err != nil {
				return nil, fmt.Errorf("list all users: %w", err)
			}
			addUsers(users)
		}
	}
	out := make([]recipient, 0, len(byID))
	for id, email := range byID {
		out = append(out, recipient{ID: id, Email: email})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *NotificationService) Notify(ctx context.Context, nType domain.NotificationType, operatorID string, metadata map[string]any) error {
	cat, ok := domain.TypeCategory[nType]
	if !ok {
		return nil
	}

	s.mu.RLock()
	globalPref := s.categoryPrefs[cat]
	s.mu.RUnlock()

	recipients, err := s.resolveRecipients(ctx, globalPref, operatorID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}

	// Apply per-user overrides: skip users that disabled the category and
	// honor their channel override (nil Channels means inherit the global).
	userPrefs, err := s.prefRepo.GetUserPrefsByCategory(ctx, cat)
	if err != nil {
		logger.Warn("[notification] failed to load user prefs for %s: %v", cat, err)
		userPrefs = nil
	}

	// Group recipients by the channel they should receive the message on.
	byChannel := make(map[domain.ChannelType][]string)
	for _, rc := range recipients {
		channels := globalPref.Channels
		if up, ok := userPrefs[rc.ID]; ok {
			if !up.Enabled {
				continue
			}
			if up.Channels != nil {
				channels = *up.Channels
			}
		}
		for _, ch := range channels {
			byChannel[domain.ChannelType(ch)] = append(byChannel[domain.ChannelType(ch)], rc.Email)
		}
	}

	subject := string(nType)
	if v, _ := metadata["Subject"].(string); v != "" {
		subject = v
	}

	var errs []error
	for ch, to := range byChannel {
		s.mu.RLock()
		notifier, ok := s.notifiers[ch]
		s.mu.RUnlock()
		if !ok || !notifier.Enabled() {
			continue
		}
		msg := &domain.NotificationMessage{
			ID:        domain.NewID(),
			Type:      nType,
			Channel:   ch,
			To:        to,
			Subject:   subject,
			CreatedAt: time.Now(),
			Metadata:  metadata,
		}
		if err := s.Send(ctx, msg); err != nil {
			logger.Error("[notification] send via %s failed: %v", ch, err)
			errs = append(errs, fmt.Errorf("send via %s: %w", ch, err))
		}
	}
	return errors.Join(errs...)
}

func (s *NotificationService) NotifyScanComplete(ctx context.Context, job *domain.ScanJob, libraryName string) error {
	errorsStr := job.Errors
	if errorsStr == "" {
		errorsStr = "0"
	}
	return s.Notify(ctx, domain.NotifScanComplete, "", map[string]any{
		"LibraryName":   libraryName,
		"NewTracks":     job.NewTracks,
		"UpdatedTracks": job.UpdatedTracks,
		"DeletedTracks": job.DeletedTracks,
		"Errors":        errorsStr,
		"Subject":       fmt.Sprintf("Scan Complete — %s", libraryName),
	})
}

func (s *NotificationService) NotifyScanFailed(ctx context.Context, libraryName, errorMsg string) error {
	return s.Notify(ctx, domain.NotifScanFailed, "", map[string]any{
		"LibraryName": libraryName,
		"Error":       errorMsg,
		"Subject":     fmt.Sprintf("Scan Failed — %s", libraryName),
	})
}

func (s *NotificationService) GetCategoryPrefs(ctx context.Context) map[domain.NotificationCategory]domain.CategoryPreference {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a deep copy so callers can't mutate the cached slices.
	out := make(map[domain.NotificationCategory]domain.CategoryPreference, len(s.categoryPrefs))
	for k, v := range s.categoryPrefs {
		out[k] = domain.CategoryPreference{
			Roles:    append([]string(nil), v.Roles...),
			Channels: append([]string(nil), v.Channels...),
		}
	}
	return out
}

func (s *NotificationService) UpdateCategoryPrefs(ctx context.Context, prefs map[domain.NotificationCategory]domain.CategoryPreference) error {
	if err := s.prefRepo.UpdateCategoryPrefs(ctx, prefs); err != nil {
		return err
	}
	if err := s.LoadCategoryPrefs(ctx); err != nil {
		// Persist succeeded but the cache refresh failed. Fall back to the
		// values we just persisted so in-memory state matches the DB; the
		// next successful reload will reconcile. The save itself succeeded,
		// so do not surface an error to the client.
		logger.Error("[notification] cache refresh after update failed: %v", err)
		s.mu.Lock()
		s.categoryPrefs = cloneCategoryPrefs(prefs)
		s.mu.Unlock()
	}
	return nil
}

func cloneCategoryPrefs(prefs map[domain.NotificationCategory]domain.CategoryPreference) map[domain.NotificationCategory]domain.CategoryPreference {
	out := make(map[domain.NotificationCategory]domain.CategoryPreference, len(prefs))
	for k, v := range prefs {
		out[k] = domain.CategoryPreference{
			Roles:    append([]string(nil), v.Roles...),
			Channels: append([]string(nil), v.Channels...),
		}
	}
	return out
}

func (s *NotificationService) GetUserPrefs(ctx context.Context, userID string) (map[domain.NotificationCategory]domain.UserNotificationPref, error) {
	return s.prefRepo.GetUserPrefs(ctx, userID)
}

func (s *NotificationService) UpsertUserPrefs(ctx context.Context, userID string, prefs []domain.UserNotificationPref) error {
	return s.prefRepo.UpsertUserPrefs(ctx, userID, prefs)
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
