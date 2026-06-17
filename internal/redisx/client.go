package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/redis/go-redis/v9"
)

// Reader is the minimal Redis read interface used by the stores.
type Reader interface {
	HGet(ctx context.Context, key, field string) *redis.StringCmd
	HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

// Clients exposes the Redis writer and a Reader.
// When no replica is configured, Reader and Writer point to the same
// *redis.Client to avoid maintaining two connection pools.
type Clients struct {
	Writer *redis.Client
	Reader *redis.Client
}

// Close closes the underlying Redis clients. It is safe to call when Reader and
// Writer share the same client.
func (c *Clients) Close() error {
	if c.Writer != nil {
		if err := c.Writer.Close(); err != nil {
			return err
		}
	}
	if c.Reader != nil && c.Reader != c.Writer {
		if err := c.Reader.Close(); err != nil {
			return err
		}
	}
	return nil
}

// New creates Redis clients from configuration and pings both writer and reader.
func New(cfg *config.Config) (*Clients, error) {
	writer := newClient(cfg.RedisMaster, cfg)

	reader := writer
	if cfg.RedisReplicaAddr() != "" {
		reader = newClient(cfg.RedisReplicaAddr(), cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := writer.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis writer: %w", err)
	}
	if reader != writer {
		if err := reader.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("ping redis reader: %w", err)
		}
	}

	return &Clients{Writer: writer, Reader: reader}, nil
}

func newClient(addr string, cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.RedisPass,
		DB:       cfg.RedisDB,
	})
}
