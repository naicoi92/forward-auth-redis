package cookiex

import (
	"net/http"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/randutil"
)

// Builder constructs and reads authentication cookies.
type Builder struct {
	cfg      *config.Config
	basePath string
}

// New creates a cookie builder from configuration.
func New(cfg *config.Config) *Builder {
	return &Builder{cfg: cfg, basePath: cfg.BasePath}
}

const csrfCookieName = "fa_csrf"

// SetCSRF writes a double-submit CSRF cookie and returns its value.
func (b *Builder) SetCSRF(w http.ResponseWriter) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     b.basePath,
		Domain:   b.cfg.CookieDomain,
		MaxAge:   0,
		HttpOnly: true,
		Secure:   b.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	return token, nil
}

// ReadCSRF returns the value of the CSRF cookie, if present.
func (b *Builder) ReadCSRF(r *http.Request) string {
	c, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// ClearCSRF removes the CSRF cookie.
func (b *Builder) ClearCSRF(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     b.basePath,
		Domain:   b.cfg.CookieDomain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   b.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

// Set writes the JWT cookie to the response. The auth cookie intentionally
// uses Path="/" (not BasePath) so that the browser sends it on every request
// to the protected application, not only to the auth endpoints.
func (b *Builder) Set(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     b.cfg.CookieName,
		Value:    token,
		Path:     "/",
		Domain:   b.cfg.CookieDomain,
		MaxAge:   b.cfg.CookieMaxAge,
		HttpOnly: true,
		Secure:   b.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Clear expires the authentication cookie.
func (b *Builder) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     b.cfg.CookieName,
		Value:    "",
		Path:     "/",
		Domain:   b.cfg.CookieDomain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   b.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Read extracts the JWT value from the request cookie, if present.
func (b *Builder) Read(r *http.Request) string {
	c, err := r.Cookie(b.cfg.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func randomToken() (string, error) {
	return randutil.Hex(32)
}
