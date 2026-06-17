package store

import (
	"context"
	"fmt"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/redis/go-redis/v9"
)

const (
	loginAttemptsPrefix = "login:attempts:"
	usedCodeFormat      = "login:used:%s:%s"
)

// LoginGuard tracks per-username brute-force attempts and prevents OTP code replay.
// All operations run against the Redis master so counters are accurate and not
// subject to replica lag.
type LoginGuard struct {
	writer *redis.Client
	cfg    *config.Config
}

// NewLoginGuard creates a new LoginGuard backed by the Redis master.
func NewLoginGuard(writer *redis.Client, cfg *config.Config) *LoginGuard {
	return &LoginGuard{writer: writer, cfg: cfg}
}

// RecordAttempt increments the per-username attempt counter and reports whether
// the user is currently blocked.
func (g *LoginGuard) RecordAttempt(ctx context.Context, username string) (count int, blocked bool, err error) {
	key := loginAttemptsPrefix + username

	pipe := g.writer.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, g.cfg.LoginWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, false, fmt.Errorf("redis record attempt: %w", err)
	}

	count = int(incr.Val())
	return count, count >= g.cfg.MaxLoginAttempts, nil
}

// ResetAttempts clears the per-username attempt counter, typically after a
// successful login.
func (g *LoginGuard) ResetAttempts(ctx context.Context, username string) error {
	if err := g.writer.Del(ctx, loginAttemptsPrefix+username).Err(); err != nil {
		return fmt.Errorf("redis reset attempts: %w", err)
	}
	return nil
}

// MarkCodeUsed marks a TOTP code as used for the given username. It returns true
// if the code has already been used within the configured TTL window.
func (g *LoginGuard) MarkCodeUsed(ctx context.Context, username, code string) (alreadyUsed bool, err error) {
	key := fmt.Sprintf(usedCodeFormat, username, code)
	ok, err := g.writer.SetNX(ctx, key, "1", g.cfg.OTPReuseTTL).Result()
	if err != nil {
		return false, fmt.Errorf("redis mark code used: %w", err)
	}
	return !ok, nil
}
