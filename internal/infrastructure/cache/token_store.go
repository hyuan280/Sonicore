package cache

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type TokenStore struct {
	vk *Valkey
}

func NewTokenStore(vk *Valkey) *TokenStore {
	return &TokenStore{vk: vk}
}

func (s *TokenStore) Generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *TokenStore) Store(ctx context.Context, userID, refreshToken string, ttl time.Duration) error {
	hash := s.hash(refreshToken)
	pipe := s.vk.client.Pipeline()
	pipe.Set(ctx, s.vk.key("rt:"+hash), userID, ttl)
	pipe.SAdd(ctx, s.vk.key("user_rt:"+userID), hash)
	pipe.Expire(ctx, s.vk.key("user_rt:"+userID), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *TokenStore) Validate(ctx context.Context, refreshToken string) (string, error) {
	hash := s.hash(refreshToken)
	userID, err := s.vk.client.Get(ctx, s.vk.key("rt:"+hash)).Result()
	if err != nil {
		return "", fmt.Errorf("refresh token not found or expired")
	}
	return userID, nil
}

func (s *TokenStore) Revoke(ctx context.Context, refreshToken string) error {
	hash := s.hash(refreshToken)
	userID, err := s.vk.client.GetDel(ctx, s.vk.key("rt:"+hash)).Result()
	if err != nil {
		return nil
	}
	s.vk.client.SRem(ctx, s.vk.key("user_rt:"+userID), hash)
	return nil
}

func (s *TokenStore) RevokeAll(ctx context.Context, userID string) error {
	members, err := s.vk.client.SMembers(ctx, s.vk.key("user_rt:"+userID)).Result()
	if err != nil {
		return err
	}

	if len(members) > 0 {
		keys := make([]string, len(members))
		for i, h := range members {
			keys[i] = s.vk.key("rt:" + h)
		}
		s.vk.client.Del(ctx, keys...)
	}

	s.vk.client.Del(ctx, s.vk.key("user_rt:"+userID))
	return nil
}

func (s *TokenStore) hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
