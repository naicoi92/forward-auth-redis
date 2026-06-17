package httpapi

import (
	"net/http"
	"net/url"
)

// authorize is the forward-auth endpoint invoked by Caddy for every request.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	token := h.cookie.Read(r)
	username, err := h.svc.Authorize(r.Context(), token)
	if err != nil {
		returnTo := r.Header.Get("X-Forwarded-Uri")
		loc := url.URL{}
		loc.Path = h.cfg.BasePath + "/login"
		q := loc.Query()
		q.Set("return_to", h.svc.SafeReturnTo(returnTo))
		loc.RawQuery = q.Encode()
		http.Redirect(w, r, loc.String(), http.StatusFound)
		return
	}

	w.Header().Set("X-Auth-User", username)
	w.WriteHeader(http.StatusOK)
}
