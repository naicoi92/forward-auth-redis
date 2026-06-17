package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// logout removes the current session and clears the auth cookie.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token := h.cookie.Read(r)
	if token != "" {
		if err := h.svc.Logout(r.Context(), token); err != nil {
			slog.WarnContext(r.Context(), "logout failed", "error", err)
		}
	}
	h.cookie.Clear(w)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
