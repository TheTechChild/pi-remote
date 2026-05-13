// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
)

// clientWS is the /v1/client handler: CF Access JWT cookie auth → WS upgrade
// → read client_hello → lookup in the clients registry (SPEC.md § 10.3).
//
// Workstream C scope: validate the hello, log "client_hello accepted", then
// keep the conn open just to detect close (no reply frames; no subscriptions;
// no fan-out). Those land with the broker in Batch 3.
type clientWS struct {
	auth    auth.Middleware
	clients *clients.Registry
	log     *slog.Logger
}

func (h *clientWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, err := h.auth.AccessJWT(r)
	if err != nil {
		h.log.Info("client WS auth rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

	// TODO(cf-tunnel): origin check is enforced at the CF tunnel.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.log.Warn("websocket.Accept failed (client)", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer func() { _ = c.CloseNow() }()

	h.log.Info("client WS connected",
		"email", identity.Email, "client_id", identity.ClientID, "remote", r.RemoteAddr)

	// First frame must be client_hello with a known client_id and the
	// required fields populated.
	msgType, data, err := c.Read(ctx)
	if err != nil {
		h.log.Debug("client WS read (hello) end", "err", err)
		return
	}
	if msgType != websocket.MessageText {
		_ = c.Close(websocket.StatusPolicyViolation, "first frame must be client_hello (text)")
		return
	}

	// Peek at "type" first; client_hello is the only allowed first frame.
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		h.log.Info("client WS malformed first frame", "err", err)
		_ = c.Close(websocket.StatusPolicyViolation, "malformed first frame")
		return
	}
	if head.Type != "client_hello" {
		h.log.Info("client WS first frame not client_hello", "type", head.Type)
		_ = c.Close(websocket.StatusPolicyViolation, "first frame must be client_hello")
		return
	}

	// Strict decode: the generated UnmarshalJSON enforces required fields.
	var hello coordinator_app.ClientHelloJson
	if err := json.Unmarshal(data, &hello); err != nil {
		h.log.Info("client WS client_hello missing required fields", "err", err)
		_ = c.Close(websocket.StatusPolicyViolation, "client_hello missing required fields")
		return
	}

	if _, ok := h.clients.Get(hello.ClientId); !ok {
		h.log.Info("client WS unknown client_id", "client_id", hello.ClientId)
		_ = c.Close(websocket.StatusPolicyViolation, "unknown client_id")
		return
	}

	h.log.Info("client_hello accepted",
		"client_id", hello.ClientId, "app_version", hello.AppVersion,
		"auth_email", identity.Email)

	// Workstream C: no reply frames, no subscription handling. Read loop
	// exists only to detect close and (in Batch 3) future client-to-
	// coordinator frames.
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.log.Debug("client WS read end", "err", err)
			}
			return
		}
		// Phase-1 forward-compat: log + ignore unknown client frames.
		// We don't even bother decoding them — the broker work will
		// route subscribe_machine_list / spawn_session / pty_input.
	}
}
