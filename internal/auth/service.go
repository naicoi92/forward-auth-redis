package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/store"
	"github.com/naicoi92/forward-auth-redis/internal/userx"
)

// Service orchestrates TOTP verification, session management, and JWT issuance.
type Service struct {
	cfg      *config.Config
	totp     *store.TOTPStore
	sessions *store.SessionStore
	guard    *store.LoginGuard
	jwt      *JWT
}

// NewService creates the auth service wired to its dependencies.
func NewService(cfg *config.Config, totp *store.TOTPStore, sessions *store.SessionStore, guard *store.LoginGuard, jwt *JWT) *Service {
	return &Service{
		cfg:      cfg,
		totp:     totp,
		sessions: sessions,
		guard:    guard,
		jwt:      jwt,
	}
}

// Sentinel errors returned by Service methods.
var (
	ErrTooManyAttempts = errors.New("too many login attempts, please try again later")
	errInvalidCreds    = errors.New("invalid credentials")
	errCodeUsed        = errors.New("code already used")
)

// LoginResult contains everything the HTTP layer needs after a successful login.
type LoginResult struct {
	Username string
	Token    string
	ReturnTo string
}

const dummyTOTPSecret = "DUMMYDUMMYDUMMYDU" // 16-char base32, same length as real secrets

// Login verifies a TOTP code, prevents brute-force and replay, creates a session
// and returns a signed JWT.
func (s *Service) Login(ctx context.Context, username, code, returnTo string) (*LoginResult, error) {
	if err := userx.ValidateUsername(username); err != nil {
		return nil, errInvalidCreds
	}
	safeReturn := s.safeReturnTo(returnTo)

	// Brute-force protection: record this attempt and block if over threshold.
	_, blocked, err := s.guard.RecordAttempt(ctx, username)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, ErrTooManyAttempts
	}

	// Fetch the user's TOTP secret. To avoid leaking whether the username exists
	// via timing, we always perform a verification step: if the secret is
	// missing we verify against a dummy secret, then return the same generic
	// error either way.
	secret, err := s.totp.GetSecret(ctx, username)
	found := err == nil

	verifySecret := secret
	if !found {
		verifySecret = dummyTOTPSecret
	}
	ok := VerifyTOTP(verifySecret, code)
	if !found || !ok {
		return nil, errInvalidCreds
	}

	// Replay protection: each code can only be used once within OTP_REUSE_TTL.
	alreadyUsed, err := s.guard.MarkCodeUsed(ctx, username, code)
	if err != nil {
		return nil, err
	}
	if alreadyUsed {
		return nil, errInvalidCreds
	}

	// Successful login: clear the attempt counter.
	if err := s.guard.ResetAttempts(ctx, username); err != nil {
		slog.WarnContext(ctx, "failed to reset login attempts", "username", username, "error", err)
	}

	// Create the Redis session (with WAIT when replicas exist).
	sessionID, err := s.sessions.Create(ctx, username)
	if err != nil {
		return nil, err
	}

	// Issue the JWT.
	token, err := s.jwt.Sign(username, sessionID)
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		Username: username,
		Token:    token,
		ReturnTo: safeReturn,
	}, nil
}

// Authorize validates the JWT cookie and the backing Redis session. It spawns
// a background goroutine to renew the session when the rate-limit window allows.
func (s *Service) Authorize(ctx context.Context, cookie string) (string, error) {
	claims, err := s.jwt.Parse(cookie)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	session, err := s.sessions.Get(ctx, claims.Sid)
	if err != nil {
		return "", fmt.Errorf("session error: %w", err)
	}
	if session == nil {
		return "", fmt.Errorf("session not found")
	}

	// Renew asynchronously so the auth response is never blocked by a master write.
	go func(sid string, lastRenewal int64) {
		nowMs := time.Now().UnixMilli()
		lastMs := lastRenewal * 1000
		if nowMs-lastMs < s.cfg.RenewInterval.Milliseconds() {
			return
		}
		// Re-check that the session still exists before renewing, in case it was
		// deleted (e.g. logout) between the auth read and this goroutine running.
		ctx := context.Background()
		session, err := s.sessions.Get(ctx, sid)
		if err != nil {
			slog.WarnContext(ctx, "session get before renew failed", "session_id", sid, "error", err)
			return
		}
		if session == nil {
			return
		}
		if err := s.sessions.Renew(ctx, sid); err != nil {
			slog.WarnContext(ctx, "session renew failed", "session_id", sid, "error", err)
		}
	}(claims.Sid, session.LastRenewal)

	return claims.RegisteredClaims.Subject, nil
}

// Logout removes the Redis session behind the provided JWT cookie.
func (s *Service) Logout(ctx context.Context, cookie string) error {
	claims, err := s.jwt.Parse(cookie)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	return s.sessions.Delete(ctx, claims.Sid)
}

// SafeReturnTo exposes the redirect sanitiser to HTTP handlers.
func (s *Service) SafeReturnTo(returnTo string) string {
	return s.safeReturnTo(returnTo)
}

// safeReturnTo validates redirect targets and prevents open redirects.
// Only relative paths starting with a single "/" are allowed.
func (s *Service) safeReturnTo(returnTo string) string {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return "/"
	}

	// Decode percent-encoding first to catch obfuscated attacks like %2F%2Fevil.com.
	decoded, err := url.QueryUnescape(returnTo)
	if err != nil {
		return "/"
	}

	// Must be a relative path.
	if !strings.HasPrefix(decoded, "/") {
		return "/"
	}
	// Reject protocol-relative URLs (//evil.com).
	if strings.HasPrefix(decoded, "//") {
		return "/"
	}
	// Reject absolute URLs and control characters.
	if strings.Contains(decoded, "://") || strings.ContainsAny(decoded, "\r\n\x00") {
		return "/"
	}

	decoded = strings.TrimSuffix(decoded, "/")
	if decoded == "" {
		return "/"
	}
	return decoded
}
