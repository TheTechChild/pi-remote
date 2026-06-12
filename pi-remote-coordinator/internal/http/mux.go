// SPDX-License-Identifier: MIT
package http

import (
	"log/slog"
	"net/http"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/push"
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

	// Keypair is the coordinator's X25519 identity (SPEC § 19.2). When
	// nil, POST /v1/clients/register is not routed — push registration
	// requires the keypair.
	Keypair *push.Keypair

	// Focus tracks per-(client,session) foreground state for push
	// suppression (SPEC § 9.7). Optional.
	Focus *push.FocusTracker

	// Push, when non-nil, receives a Notification for every push-worthy
	// occurrence ingested from daemons (SPEC §§ 8.7-8.8). Dispatch runs
	// in its own goroutine to keep the daemon read loop hot.
	Push *push.Dispatcher
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
	mux.HandleFunc("GET /v1/health", healthHandler(deps.Keypair != nil))

	ingestor := machines.NewIngestor(deps.Machines, deps.Sessions, deps.Logger)

	cWS := &clientWS{
		auth:     deps.Auth,
		clients:  deps.Clients,
		sessions: deps.Sessions,
		machines: deps.Machines,
		focus:    deps.Focus,
		log:      deps.Logger,
	}

	ingestor.SetOnMachineChange(cWS.broadcastMachineList)
	ingestor.SetOnSpawnResponse(cWS.handleSpawnResponse)
	if deps.Push != nil {
		dispatcher := deps.Push
		ingestor.SetOnPush(func(n push.Notification) { go dispatcher.Dispatch(n) })
	}

	mux.Handle("/v1/daemon", &daemonWS{
		auth:            deps.Auth,
		sessions:        deps.Sessions,
		ingestor:        ingestor,
		log:             deps.Logger,
		onMachineChange: cWS.broadcastMachineList,
	})
	mux.Handle("/v1/client", cWS)
	if deps.Keypair != nil {
		mux.Handle("POST /v1/clients/register", &clientsRegister{
			auth:    deps.Auth,
			clients: deps.Clients,
			keypair: deps.Keypair,
			log:     deps.Logger,
		})
	}
	mux.Handle("GET /v1/auth/app-callback", &authCallback{auth: deps.Auth, log: deps.Logger})
	mux.Handle("POST /v1/clients/{client_id}/preferences", &clientsPreferences{
		auth:    deps.Auth,
		clients: deps.Clients,
		log:     deps.Logger,
	})
	return mux
}

// healthHandler reports overall liveness plus push-subsystem readiness.
// A coordinator whose keypair could not be loaded or generated still
// brokers sessions, but push registration is disabled — that degraded
// state must be observable (not just one startup WARN line), so
// monitoring and smoke tests can catch e.g. a /data permission sweep.
func healthHandler(pushReady bool) http.HandlerFunc {
	body := []byte(`{"status":"ok","push":"ready"}`)
	if !pushReady {
		body = []byte(`{"status":"ok","push":"disabled"}`)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// writeAuthRequired emits the canonical 403 + ERR_COORD_AUTH_REQUIRED body.
// It MUST be called before any WebSocket upgrade attempt — once the
// response has been 101'd you can only close the socket, not 403.
func writeAuthRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"code":"ERR_COORD_AUTH_REQUIRED"}`))
}
