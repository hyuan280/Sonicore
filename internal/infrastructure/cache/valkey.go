package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sonicore/server/internal/config"
)

type Valkey struct {
	client *redis.Client
	prefix string
}

func NewValkey(cfg config.RedisConfig) *Valkey {
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	})

	return &Valkey{
		client: rdb,
		prefix: cfg.KeyPrefix,
	}
}

func (v *Valkey) key(k string) string {
	return v.prefix + k
}

func (v *Valkey) Ping(ctx context.Context) error {
	return v.client.Ping(ctx).Err()
}

func (v *Valkey) Close() error {
	return v.client.Close()
}

func (v *Valkey) Client() *redis.Client {
	return v.client
}
