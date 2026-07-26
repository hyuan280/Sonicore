package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type SessionStore struct {
	vk  *Valkey
	ttl time.Duration
}

func NewSessionStore(vk *Valkey) *SessionStore {
	return &SessionStore{
		vk:  vk,
		ttl: 720 * time.Hour,
	}
}

func (s *SessionStore) Generate(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	key := s.vk.key("sess:" + token)

	if err := s.vk.client.Set(ctx, key, userID, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}

	return token, nil
}

func (s *SessionStore) Validate(ctx context.Context, token string) (string, error) {
	key := s.vk.key("sess:" + token)
	userID, err := s.vk.client.Get(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("invalid session")
	}
	s.vk.client.Expire(ctx, key, s.ttl)
	return userID, nil
}

func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	key := s.vk.key("sess:" + token)
	return s.vk.client.Del(ctx, key).Err()
}
