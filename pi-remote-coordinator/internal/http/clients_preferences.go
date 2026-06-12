// SPDX-License-Identifier: MIT
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/push"
)

// clientsPreferences is the POST /v1/clients/{client_id}/preferences
// handler (SPEC.md § 19.6): per-reason push toggles, applied by the
// dispatcher at push-decision time.
type clientsPreferences struct {
	auth    auth.Middleware
	clients *clients.Registry
	log     *slog.Logger
}

func (h *clientsPreferences) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := h.auth.AccessJWT(r); err != nil {
		h.log.Info("clients/preferences auth rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

	clientID := r.PathValue("client_id")
	var prefs map[string]bool
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&prefs); err != nil {
		writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "body must be a JSON object of {reason: bool}")
		return
	}
	for reason := range prefs {
		if !push.ValidReason(reason) {
			writeJSONError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "unknown reason: "+reason)
			return
		}
	}

	if !h.clients.SetPreferences(clientID, prefs) {
		writeJSONError(w, http.StatusNotFound, "ERR_UNKNOWN_CLIENT", "client_id not registered")
		return
	}

	h.log.Info("client preferences updated", "client_id", clientID, "toggles", len(prefs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
