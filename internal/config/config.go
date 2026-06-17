package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	ListenAddr   string `env:"LISTEN_ADDR" envDefault:":8080"`
	BasePath     string `env:"BASE_PATH" envDefault:"/com.auth.forward"`
	RedisMaster  string `env:"REDIS_MASTER_ADDR" envDefault:"localhost:6379"`
	RedisReplica string `env:"REDIS_REPLICA_ADDR" envDefault:""` // single optional replica address
	RedisPass    string `env:"REDIS_PASSWORD"`
	RedisDB      int    `env:"REDIS_DB" envDefault:"0"`

	JWTSecret string `env:"JWT_SECRET,required"`

	SessionTTL    time.Duration `env:"SESSION_TTL" envDefault:"15m"`
	RenewInterval time.Duration `env:"RENEW_INTERVAL" envDefault:"60s"`

	CookieName   string `env:"COOKIE_NAME" envDefault:"fa_token"`
	CookieMaxAge int    `env:"COOKIE_MAX_AGE" envDefault:"0"`
	CookieSecure bool   `env:"COOKIE_SECURE" envDefault:"true"`
	CookieDomain string `env:"COOKIE_DOMAIN"`

	TOTPIssuer string `env:"TOTP_ISSUER" envDefault:"forward-auth"`
	TOTPSkew   uint   `env:"TOTP_SKEW" envDefault:"1"`

	MaxLoginAttempts int           `env:"MAX_LOGIN_ATTEMPTS" envDefault:"5"`
	LoginWindow      time.Duration `env:"LOGIN_WINDOW" envDefault:"5m"`
	OTPReuseTTL      time.Duration `env:"OTP_REUSE_TTL" envDefault:"5m"`

	SessionWaitReplicas int           `env:"SESSION_WAIT_REPLICAS" envDefault:"1"`
	SessionWaitTimeout  time.Duration `env:"SESSION_WAIT_TIMEOUT" envDefault:"500ms"`
}

// Load parses environment variables into Config and validates the result.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes, got %d", len(c.JWTSecret))
	}
	if err := validateCookieDomain(c.CookieDomain); err != nil {
		return err
	}
	if err := validateDurations(c); err != nil {
		return err
	}
	if err := validateLoginGuard(c); err != nil {
		return err
	}
	if err := validateTOTP(c); err != nil {
		return err
	}
	if err := validateSessionWait(c); err != nil {
		return err
	}
	c.BasePath = normalizeBasePath(c.BasePath)
	return nil
}

func validateDurations(c *Config) error {
	if c.SessionTTL <= 0 {
		return fmt.Errorf("SESSION_TTL must be positive, got %v", c.SessionTTL)
	}
	if c.RenewInterval <= 0 {
		return fmt.Errorf("RENEW_INTERVAL must be positive, got %v", c.RenewInterval)
	}
	if c.RenewInterval >= c.SessionTTL {
		return fmt.Errorf("RENEW_INTERVAL (%v) must be less than SESSION_TTL (%v)", c.RenewInterval, c.SessionTTL)
	}
	return nil
}

func validateLoginGuard(c *Config) error {
	if c.MaxLoginAttempts <= 0 {
		return fmt.Errorf("MAX_LOGIN_ATTEMPTS must be positive, got %d", c.MaxLoginAttempts)
	}
	if c.LoginWindow <= 0 {
		return fmt.Errorf("LOGIN_WINDOW must be positive, got %v", c.LoginWindow)
	}
	if c.OTPReuseTTL <= 0 {
		return fmt.Errorf("OTP_REUSE_TTL must be positive, got %v", c.OTPReuseTTL)
	}
	return nil
}

func validateTOTP(c *Config) error {
	if c.TOTPSkew <= 0 {
		return fmt.Errorf("TOTP_SKEW must be positive, got %d", c.TOTPSkew)
	}
	return nil
}

func validateSessionWait(c *Config) error {
	if c.SessionWaitReplicas < 0 {
		return fmt.Errorf("SESSION_WAIT_REPLICAS must be non-negative, got %d", c.SessionWaitReplicas)
	}
	if c.SessionWaitTimeout < 0 {
		return fmt.Errorf("SESSION_WAIT_TIMEOUT must be non-negative, got %v", c.SessionWaitTimeout)
	}
	return nil
}

func validateCookieDomain(d string) error {
	if d == "" {
		return nil
	}
	// RFC 6265 allows leading dots for subdomain matching. We strip all of
	// them and reject trailing dots, which are invalid and cause undefined
	// browser behaviour.
	trimmed := d
	for strings.HasPrefix(trimmed, ".") {
		trimmed = strings.TrimPrefix(trimmed, ".")
	}
	if trimmed == "" || !strings.Contains(trimmed, ".") {
		return fmt.Errorf("COOKIE_DOMAIN must contain a valid registered domain: %q", d)
	}
	if strings.HasSuffix(trimmed, ".") {
		return fmt.Errorf("COOKIE_DOMAIN must not end with a trailing dot: %q", d)
	}
	return nil
}

// RedisReplicaAddr returns the single optional Redis replica address, or an
// empty string if no replica is configured.
func (c *Config) RedisReplicaAddr() string {
	return strings.TrimSpace(c.RedisReplica)
}

// normalizeBasePath ensures the base path starts with exactly one leading slash
// and has no trailing slash, so chi router mounts deterministically.
func normalizeBasePath(p string) string {
	if p == "" {
		return "/com.auth.forward"
	}
	p = strings.TrimLeft(p, "/")
	p = "/" + p
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		p = "/"
	}
	return p
}
