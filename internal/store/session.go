package store

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/randutil"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRenewLockTTL = 60 * time.Second
	renewLockKeyPrefix  = "session:renew-lock:"
)

// Session holds the data stored in Redis for a login session.
type Session struct {
	Username    string
	LastRenewal int64 // Unix timestamp in seconds
}

// SessionStore writes to the Redis master and reads from the reader (replica or
// master fallback). It creates sessions with WAIT when replicas are configured
// to avoid replica-lag races immediately after login.
type SessionStore struct {
	writer     *redis.Client
	reader     *redis.Client
	cfg        *config.Config
	hasReplica bool
}

// NewSessionStore creates a session store.
func NewSessionStore(writer, reader *redis.Client, cfg *config.Config) *SessionStore {
	return &SessionStore{
		writer:     writer,
		reader:     reader,
		cfg:        cfg,
		hasReplica: cfg.RedisReplicaAddr() != "",
	}
}

// Create creates a new session for the given username, stores it on the Redis
// master with a TTL, and waits for replication if replicas are configured.
func (s *SessionStore) Create(ctx context.Context, username string) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()

	pipe := s.writer.Pipeline()
	pipe.HSet(ctx, sessionKey(id), "username", username, "last_renewal", now)
	pipe.Expire(ctx, sessionKey(id), s.cfg.SessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("redis create session: %w", err)
	}

	if s.hasReplica && s.cfg.SessionWaitReplicas > 0 {
		// WAIT is best-effort: if it times out we still return the session so the
		// login request does not fail due to transient replication lag. However,
		// we log errors to make replica problems observable.
		replicas, err := s.writer.Wait(ctx, s.cfg.SessionWaitReplicas, s.cfg.SessionWaitTimeout).Result()
		if err != nil {
			slog.ErrorContext(ctx, "session WAIT failed", "error", err)
		} else if int(replicas) < s.cfg.SessionWaitReplicas {
			slog.ErrorContext(ctx, "session WAIT did not reach required replicas", "reached", replicas, "required", s.cfg.SessionWaitReplicas)
		}
	}

	return id, nil
}

// Get fetches a session by ID from the reader.
func (s *SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	vals, err := s.reader.HGetAll(ctx, sessionKey(id)).Result()
	if err != nil {
		return nil, fmt.Errorf("redis hgetall session: %w", err)
	}
	if len(vals) == 0 {
		return nil, nil
	}

	sess := &Session{Username: vals["username"]}
	if v := vals["last_renewal"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			sess.LastRenewal = n
		} else {
			slog.WarnContext(ctx, "failed to parse last_renewal in session", "session_id", id, "value", v, "error", err)
		}
	}
	return sess, nil
}

// Renew extends the session TTL and updates last_renewal to now.
// It is safe to call from a goroutine with an independent context.
// It first acquires a short-lived renew lock to prevent concurrent renewals.
func (s *SessionStore) Renew(ctx context.Context, id string) error {
	lockKey := renewLockKeyPrefix + id
	// The renew operation takes milliseconds; keep the lock short-lived so a
	// stalled goroutine doesn't block legitimate renewals for the full interval.
	lockTTL := 5 * time.Second
	if cfgTTL := s.cfg.RenewInterval; cfgTTL > 0 && cfgTTL < lockTTL {
		lockTTL = cfgTTL
	}
	// Try to take the lock. If we can't, another goroutine is already renewing.
	ok, err := s.writer.SetNX(ctx, lockKey, "1", lockTTL).Result()
	if err != nil {
		return fmt.Errorf("redis renew lock: %w", err)
	}
	if !ok {
		return nil
	}

	now := time.Now().Unix()
	pipe := s.writer.Pipeline()
	pipe.Expire(ctx, sessionKey(id), s.cfg.SessionTTL)
	pipe.HSet(ctx, sessionKey(id), "last_renewal", now)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis renew session: %w", err)
	}
	return nil
}

// Delete removes a session from Redis.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	if err := s.writer.Del(ctx, sessionKey(id)).Err(); err != nil {
		return fmt.Errorf("redis delete session: %w", err)
	}
	return nil
}

func sessionKey(id string) string { return "session:" + id }

func newSessionID() (string, error) {
	return randutil.Hex(32)
}
