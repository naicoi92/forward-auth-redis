package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/naicoi92/forward-auth-redis/internal/auth"
	"github.com/naicoi92/forward-auth-redis/internal/webui"
)

// loginForm renders the login page.
func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	data := webui.LoginData{
		BasePath: h.cfg.BasePath,
		Error:    "",
		ReturnTo: r.URL.Query().Get("return_to"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteLogin(w, data); err != nil {
		slog.ErrorContext(r.Context(), "failed to render login page", "error", err)
		http.Error(w, "failed to render login page", http.StatusInternalServerError)
	}
}

// loginSubmit verifies the TOTP code and establishes a session.
func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, r, "invalid form", http.StatusBadRequest)
		return
	}

	username := r.PostFormValue("username")
	code := r.PostFormValue("code")
	returnTo := r.PostFormValue("return_to")

	result, err := h.svc.Login(r.Context(), username, code, returnTo)
	if err != nil {
		// All TOTP/replay failures are reported with the same generic message to
		// avoid leaking whether a username exists or whether a code was replayed.
		msg := "invalid credentials"
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrTooManyAttempts) {
			status = http.StatusTooManyRequests
			msg = err.Error()
		}
		h.renderLoginError(w, r, msg, status)
		return
	}

	h.cookie.Set(w, result.Token)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", result.ReturnTo)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
}

func (h *Handler) renderLoginError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if r.Header.Get("HX-Request") == "true" {
		// Return an HTML fragment so htmx swaps cleanly into #form-msg.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := h.templates.ExecuteErrorFragment(w, msg); err != nil {
			slog.ErrorContext(r.Context(), "failed to render error fragment", "error", err)
		}
		return
	}

	// Prefer the POSTed return_to so error rendering stays consistent with the
	// successful submission path.
	returnTo := r.PostFormValue("return_to")
	if returnTo == "" {
		returnTo = r.URL.Query().Get("return_to")
	}
	data := webui.LoginData{
		BasePath: h.cfg.BasePath,
		Error:    msg,
		ReturnTo: returnTo,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates.ExecuteLogin(w, data); err != nil {
		slog.ErrorContext(r.Context(), "failed to render login page", "error", err)
	}
}
