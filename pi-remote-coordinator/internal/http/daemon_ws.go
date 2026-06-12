// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// daemonWS is the /v1/daemon handler: CF service-token auth → WS upgrade →
// single read loop dispatched into the Ingestor (SPEC.md § 10.2).
//
// Framing-correctness invariant: auth runs BEFORE websocket.Accept. Once a
// 101 has been sent we can only close, not 403.
//
// Workstream C ships no write goroutine — that lands with the broker
// (Batch 3). One goroutine per connection, reading frames, dispatching
// synchronously into the Ingestor.
type daemonWS struct {
	auth auth.Middleware
	// machines is intentionally not held here: the Ingestor owns writes
	// into the registry, and the WS handler only needs to pause sessions
	// on socket close. The registry pointer lives on the Ingestor.
	sessions        *sessions.Registry
	ingestor        *machines.Ingestor
	log             *slog.Logger
	onMachineChange func()
}

// daemonConnAdapter wraps *websocket.Conn to satisfy the machines.Conn
// interface (Close + Context). websocket.Conn has CloseNow which we use as
// a forceful close in the take-over path; Close(code, reason) is sufficient
// here because the registry only needs to signal the peer.
type daemonConnAdapter struct {
	mu  sync.Mutex
	c   *websocket.Conn
	ctx context.Context
}

func (a *daemonConnAdapter) Close(code websocket.StatusCode, reason string) error {
	return a.c.Close(code, reason)
}

func (a *daemonConnAdapter) Context() context.Context { return a.ctx }

func (a *daemonConnAdapter) Write(ctx context.Context, typ websocket.MessageType, b []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.c.Write(ctx, typ, b)
}

func (h *daemonWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, err := h.auth.ServiceToken(r)
	if err != nil {
		h.log.Info("daemon WS auth rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

	// TODO(cf-tunnel): the Phase-1 origin check is enforced at the
	// Cloudflare tunnel boundary; we accept any Origin here. Tighten when
	// the coordinator is exposed directly.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.log.Warn("websocket.Accept failed", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn := &daemonConnAdapter{c: c, ctx: ctx}

	h.log.Info("daemon WS connected",
		"auth_machine_id", identity.MachineID, "remote", r.RemoteAddr)

	// On exit: pause any sessions tied to the registered machine and
	// forget per-conn ingest state. The machine entry is RETAINED — per
	// the design notes, there is no "machine disconnected" state in
	// Phase-1 Workstream C; a subsequent reconnect re-registers and
	// replaces the Conn. (UnregisterByConn is reserved for the take-over
	// path and the eventual disconnected-state followup.)
	defer func() {
		_ = c.CloseNow()
		if mid, ok := h.machineIDForConn(conn); ok {
			h.sessions.PauseAllForMachine(mid)
			if h.onMachineChange != nil {
				h.onMachineChange()
			}
		}
		h.ingestor.ForgetConn(conn)
	}()

	// Read loop. Synchronous dispatch into the ingestor.
	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.log.Debug("daemon WS read end", "err", err)
			}
			return
		}
		if msgType != websocket.MessageText {
			// Phase-1 protocol is JSON text frames only. Ignore binary
			// per forward-compat policy.
			h.log.Debug("daemon WS ignoring non-text frame", "type", msgType)
			continue
		}
		if err := h.ingestor.Handle(ctx, conn, data); err != nil {
			switch {
			case errors.Is(err, machines.ErrFirstFrameNotRegister):
				h.log.Info("daemon WS first frame not machine_register; closing",
					"remote", r.RemoteAddr)
				_ = c.Close(websocket.StatusPolicyViolation, "first frame must be machine_register")
				return
			case errors.Is(err, machines.ErrMalformedFrame):
				h.log.Info("daemon WS malformed frame; closing", "remote", r.RemoteAddr)
				_ = c.Close(websocket.StatusPolicyViolation, "malformed frame")
				return
			default:
				h.log.Warn("daemon WS ingest error; closing",
					"err", err, "remote", r.RemoteAddr)
				_ = c.Close(websocket.StatusInternalError, "ingest error")
				return
			}
		}
	}
}

// machineIDForConn returns the machine_id this conn last registered as, or
// "" if the conn never registered (e.g. closed before first frame). We
// implement this by asking the Ingestor's per-conn state, which is the
// canonical source of truth.
func (h *daemonWS) machineIDForConn(conn machines.Conn) (string, bool) {
	return h.ingestor.MachineIDForConn(conn)
}
