package cache

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrClientMismatch = errors.New("client mismatch")

type SessionStore struct {
	vk  *Valkey
	ttl time.Duration
}

func NewSessionStore(vk *Valkey) *SessionStore {
	return &SessionStore{
		vk:  vk,
		ttl: 5 * time.Minute,
	}
}

func hashClientInfo(info string) string {
	h := sha256.Sum256([]byte(info))
	return hex.EncodeToString(h[:])
}

func (s *SessionStore) Generate(ctx context.Context, userID, clientInfo string) (string, error) {
	if clientInfo == "" {
		return "", fmt.Errorf("client info required")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	key := s.vk.key("sess:" + token)
	value := userID + "\n" + hashClientInfo(clientInfo)

	if err := s.vk.client.Set(ctx, key, value, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}

	return token, nil
}

func (s *SessionStore) Validate(ctx context.Context, token string) (string, error) {
	key := s.vk.key("sess:" + token)
	val, err := s.vk.client.Get(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("invalid session")
	}
	parts := strings.SplitN(val, "\n", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid session format")
	}
	return parts[0], nil
}

func (s *SessionStore) Extend(ctx context.Context, token, userID, clientInfo string) error {
	key := s.vk.key("sess:" + token)
	val, err := s.vk.client.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("session not found")
	}
	parts := strings.SplitN(val, "\n", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid session format")
	}
	if parts[0] != userID {
		return fmt.Errorf("session user mismatch")
	}
	if parts[1] != hashClientInfo(clientInfo) {
		return ErrClientMismatch
	}
	if err := s.vk.client.Expire(ctx, key, s.ttl).Err(); err != nil {
		return fmt.Errorf("extend session: %w", err)
	}
	return nil
}

func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	key := s.vk.key("sess:" + token)
	return s.vk.client.Del(ctx, key).Err()
}
