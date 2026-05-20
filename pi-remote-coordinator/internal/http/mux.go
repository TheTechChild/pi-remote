// SPDX-License-Identifier: MIT
package http

import (
	"log/slog"
	"net/http"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// Deps wires the HTTP mux's dependencies. All fields are required for the
// /v1/daemon and /v1/client endpoints; the /v1/health endpoint is
// unauthenticated and uses none of them.
type Deps struct {
	Auth     auth.Middleware
	Machines *machines.Registry
	Sessions *sessions.Registry
	Clients  *clients.Registry
	Logger   *slog.Logger
}

// NewMux constructs the coordinator's HTTP routes:
//
//	GET  /v1/health  → unauthenticated, returns {"status":"ok"}
//	     /v1/daemon  → CF service-token, WS upgrade (SPEC § 10.2)
//	     /v1/client  → CF Access JWT cookie, WS upgrade (SPEC § 10.3)
//
// See docs/phase1/batch-2-coordinator-link.md.
func NewMux(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", handleHealth)

	ingestor := machines.NewIngestor(deps.Machines, deps.Sessions, deps.Logger)

	mux.Handle("/v1/daemon", &daemonWS{
		auth:     deps.Auth,
		sessions: deps.Sessions,
		ingestor: ingestor,
		log:      deps.Logger,
	})
	mux.Handle("/v1/client", &clientWS{
		auth:     deps.Auth,
		clients:  deps.Clients,
		sessions: deps.Sessions,
		log:      deps.Logger,
	})
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// writeAuthRequired emits the canonical 403 + ERR_COORD_AUTH_REQUIRED body.
// It MUST be called before any WebSocket upgrade attempt — once the
// response has been 101'd you can only close the socket, not 403.
func writeAuthRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"code":"ERR_COORD_AUTH_REQUIRED"}`))
}
