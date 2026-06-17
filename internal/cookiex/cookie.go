package cookiex

import (
	"net/http"
	"time"

	"github.com/naicoi92/forward-auth-redis/internal/config"
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
