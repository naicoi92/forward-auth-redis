package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// authorize is the forward-auth endpoint invoked by Caddy for every request.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	token := h.cookie.Read(r)
	username, err := h.svc.Authorize(r.Context(), token)
	if err != nil {
		returnTo := r.Header.Get("X-Forwarded-Uri")
		// Defense-in-depth: if the original request targets the auth service's
		// own endpoints (BASE_PATH), return 200 instead of redirecting to login.
		// This breaks a redirect loop if the reverse proxy accidentally applies
		// forward_auth to /com.auth.forward/* paths.
		if strings.HasPrefix(returnTo, h.cfg.BasePath+"/") || returnTo == h.cfg.BasePath {
			w.WriteHeader(http.StatusOK)
			return
		}
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
