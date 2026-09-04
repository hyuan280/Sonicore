package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/core/domain"
)

type NotificationPrefRepo struct {
	db *sql.DB
}

func NewNotificationPrefRepo(db *sql.DB) *NotificationPrefRepo {
	return &NotificationPrefRepo{db: db}
}

func (r *NotificationPrefRepo) GetCategoryPrefs(ctx context.Context) (map[domain.NotificationCategory]domain.CategoryPreference, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT category, roles, channels FROM notification_category_prefs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make(map[domain.NotificationCategory]domain.CategoryPreference)
	for rows.Next() {
		var cat string
		var p domain.CategoryPreference
		if err := rows.Scan(&cat, pq.Array(&p.Roles), pq.Array(&p.Channels)); err != nil {
			return nil, err
		}
		prefs[domain.NotificationCategory(cat)] = p
	}
	return prefs, rows.Err()
}

func (r *NotificationPrefRepo) UpdateCategoryPrefs(ctx context.Context, prefs map[domain.NotificationCategory]domain.CategoryPreference) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for cat, p := range prefs {
		// Build SET clause dynamically: only update columns that are provided.
		sets := ""
		args := []any{string(cat)}
		n := 2
		if p.Roles != nil {
			sets += fmt.Sprintf("roles=$%d, ", n)
			args = append(args, pq.Array(p.Roles))
			n++
		}
		if p.Channels != nil {
			sets += fmt.Sprintf("channels=$%d, ", n)
			args = append(args, pq.Array(p.Channels))
			n++
		}
		if sets == "" {
			continue
		}
		// Remove trailing ", "
		sets = sets[:len(sets)-2]

		query := fmt.Sprintf(
			`INSERT INTO notification_category_prefs (category, roles, channels)
			 VALUES ($1, '{}'::TEXT[], '{}'::TEXT[])
			 ON CONFLICT (category) DO UPDATE SET %s`, sets)
		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *NotificationPrefRepo) GetUserPrefs(ctx context.Context, userID string) (map[domain.NotificationCategory]domain.UserNotificationPref, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT category, enabled, channels, roles FROM user_notification_prefs WHERE user_id=$1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make(map[domain.NotificationCategory]domain.UserNotificationPref)
	for rows.Next() {
		var cat string
		var p domain.UserNotificationPref
		var channels, roles []string
		if err := rows.Scan(&cat, &p.Enabled, pq.Array(&channels), pq.Array(&roles)); err != nil {
			return nil, err
		}
		p.Category = domain.NotificationCategory(cat)
		if channels != nil {
			p.Channels = &channels
		}
		if roles != nil {
			p.Roles = &roles
		}
		prefs[p.Category] = p
	}
	return prefs, rows.Err()
}

func (r *NotificationPrefRepo) GetUserPrefsByCategory(ctx context.Context, category domain.NotificationCategory) (map[string]domain.UserNotificationPref, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT user_id, category, enabled, channels, roles FROM user_notification_prefs WHERE category=$1", string(category))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := make(map[string]domain.UserNotificationPref)
	for rows.Next() {
		var userID, cat string
		var p domain.UserNotificationPref
		var channels, roles []string
		if err := rows.Scan(&userID, &cat, &p.Enabled, pq.Array(&channels), pq.Array(&roles)); err != nil {
			return nil, err
		}
		p.Category = domain.NotificationCategory(cat)
		if channels != nil {
			p.Channels = &channels
		}
		if roles != nil {
			p.Roles = &roles
		}
		prefs[userID] = p
	}
	return prefs, rows.Err()
}

func (r *NotificationPrefRepo) UpsertUserPrefs(ctx context.Context, userID string, prefs []domain.UserNotificationPref) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, pref := range prefs {
		var channels, roles any
		if pref.Channels != nil {
			channels = pq.Array(*pref.Channels)
		}
		if pref.Roles != nil {
			roles = pq.Array(*pref.Roles)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_notification_prefs (user_id, category, enabled, channels, roles)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (user_id, category) DO UPDATE SET enabled=$3, channels=$4, roles=$5`,
			userID, string(pref.Category), pref.Enabled, channels, roles); err != nil {
			return err
		}
	}
	return tx.Commit()
}
