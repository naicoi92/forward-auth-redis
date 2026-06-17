package store

import (
	"context"
	"fmt"

	"github.com/naicoi92/forward-auth-redis/internal/redisx"
	"github.com/redis/go-redis/v9"
)

const totpHashKey = "totp:secrets"

// TOTPStore reads TOTP secrets from Redis (via reader; replica or master fallback).
type TOTPStore struct {
	reader redisx.Reader
}

// NewTOTPStore creates a new TOTPStore backed by the given Redis reader.
func NewTOTPStore(reader redisx.Reader) *TOTPStore {
	return &TOTPStore{reader: reader}
}

// GetSecret returns the TOTP secret for a username, or an error if not found.
func (s *TOTPStore) GetSecret(ctx context.Context, username string) (string, error) {
	secret, err := s.reader.HGet(ctx, totpHashKey, username).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("totp secret not found for user %q", username)
	}
	if err != nil {
		return "", fmt.Errorf("redis hget %s: %w", totpHashKey, err)
	}
	return secret, nil
}
