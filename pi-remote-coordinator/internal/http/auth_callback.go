// SPDX-License-Identifier: MIT
package http

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
)

// authCallback is GET /v1/auth/app-callback: the bridge that gets a CF
// Access JWT out of the system browser and into the Android app (SPEC.md
// § D5, issue #27).
//
// Custom Tabs cannot expose the browser's cookie jar to the app, so the
// app opens this URL in the Custom Tab instead: Cloudflare Access
// authenticates the browser at the edge (email PIN) and forwards the
// request with the CF_Authorization cookie attached; we validate it and
// reflect the JWT into a pi-remote://auth/callback deep link, which
// Android routes back into the app.
type authCallback struct {
	auth auth.Middleware
	log  *slog.Logger
}

func (h *authCallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, err := h.auth.AccessJWT(r)
	if err != nil {
		h.log.Info("auth callback rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

	token := ""
	if cookie, cerr := r.Cookie("CF_Authorization"); cerr == nil {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("Cf-Access-Jwt-Assertion")
	}
	if token == "" {
		// AccessJWT passed, so this only happens with auth middlewares
		// that accept requests without a token (not the stub, not CF).
		writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "no token to reflect")
		return
	}

	h.log.Info("auth callback issued deep link", "email", identity.Email)
	http.Redirect(w, r,
		"pi-remote://auth/callback?jwt="+url.QueryEscape(token),
		http.StatusFound)
}
