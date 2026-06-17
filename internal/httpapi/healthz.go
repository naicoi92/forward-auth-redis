package httpapi

import (
	"context"
	"net/http"
	"time"
)

// healthz reports service health by pinging both Redis writer and reader.
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.redis.Writer.Ping(ctx).Err(); err != nil {
		http.Error(w, "writer ping failed", http.StatusServiceUnavailable)
		return
	}
	if err := h.redis.Reader.Ping(ctx).Err(); err != nil {
		http.Error(w, "reader ping failed", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
