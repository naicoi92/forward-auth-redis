package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/naicoi92/forward-auth-redis/internal/auth"
	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/cookiex"
	"github.com/naicoi92/forward-auth-redis/internal/redisx"
	"github.com/naicoi92/forward-auth-redis/internal/webui"
)

// Handler wires the auth service to Chi HTTP handlers.
type Handler struct {
	cfg       *config.Config
	svc       *auth.Service
	cookie    *cookiex.Builder
	redis     *redisx.Clients
	templates *webui.Templates
}

// New creates a new HTTP handler set.
func New(
	cfg *config.Config,
	svc *auth.Service,
	cookie *cookiex.Builder,
	redis *redisx.Clients,
	templates *webui.Templates,
) *Handler {
	return &Handler{cfg: cfg, svc: svc, cookie: cookie, redis: redis, templates: templates}
}

// Router returns the Chi router mounted under the configured BASE_PATH.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(h.securityHeaders)

	r.Route(h.cfg.BasePath, func(r chi.Router) {
		r.Get("/login", h.loginForm)
		r.Post("/login", h.loginSubmit)
		r.Get("/auth", h.authorize)
		r.Post("/logout", h.logout)
		r.Get("/healthz", h.healthz)
		r.Get("/login.js", h.serveLoginJS)
	})

	return r
}

// serveLoginJS serves the embedded static login.js file.
func (h *Handler) serveLoginJS(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, webui.AssetFS(), "assets/login.js")
}

func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Strict CSP: allow scripts from this origin (login.js) and jsDelivr
		// (htmx), inline styles, same-origin images and form posts, and
		// same-origin fetch requests for the htmx-driven login form.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; "+
				"script-src 'self' https://cdn.jsdelivr.net; "+
				"style-src 'unsafe-inline'; "+
				"img-src 'self'; "+
				"connect-src 'self'; "+
				"form-action 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}
