// SPDX-License-Identifier: MIT
package http

import "net/http"

// NewMux constructs the coordinator's HTTP routes. Phase-0 wires only the
// unauthenticated health endpoint; the WebSocket endpoints (/v1/daemon and
// /v1/client) and the /v1/clients/* endpoints land in milestones M1-M9.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
