// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

var clientConnCount uint64

type attachedClient struct {
	id       string
	sendChan chan []byte
}

func (a *attachedClient) ID() string {
	return a.id
}

func (a *attachedClient) Send(msg []byte) {
	select {
	case a.sendChan <- msg:
	default:
		// Queue full or client slow, drop frame to avoid blocking daemon.
	}
}

// clientWS is the /v1/client handler: CF Access JWT cookie auth → WS upgrade
// → read client_hello → lookup in the clients registry.
type clientWS struct {
	auth     auth.Middleware
	clients  *clients.Registry
	sessions *sessions.Registry
	log      *slog.Logger
}

func (h *clientWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, err := h.auth.AccessJWT(r)
	if err != nil {
		h.log.Info("client WS auth rejected", "err", err.Error(), "remote", r.RemoteAddr)
		writeAuthRequired(w)
		return
	}

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

	// Unique connection ID for fan-out
	connID := fmt.Sprintf("%s-%d", hello.ClientId, atomic.AddUint64(&clientConnCount, 1))

	// Dedicated serialize-writes channel & loop
	sendChan := make(chan []byte, 512)
	writeCtx, writeCancel := context.WithCancel(ctx)
	defer writeCancel()

	go func() {
		for {
			select {
			case <-writeCtx.Done():
				return
			case msg, ok := <-sendChan:
				if !ok {
					return
				}
				err := c.Write(writeCtx, websocket.MessageText, msg)
				if err != nil {
					h.log.Debug("client WS write error", "err", err)
					cancel()
					return
				}
			}
		}
	}()

	var attachedSession *sessions.Session
	defer func() {
		if attachedSession != nil {
			attachedSession.Detach(connID)
		}
	}()

	// Main client read/dispatch loop
	for {
		msgType, data, err := c.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.log.Debug("client WS read end", "err", err)
			}
			return
		}
		if msgType != websocket.MessageText {
			h.log.Debug("client WS ignoring non-text frame", "type", msgType)
			continue
		}

		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &peek); err != nil {
			h.log.Debug("client WS malformed frame", "err", err)
			continue
		}

		switch peek.Type {
		case "attach":
			var attach coordinator_app.AttachJson
			if err := json.Unmarshal(data, &attach); err != nil {
				h.log.Debug("client WS malformed attach", "err", err)
				continue
			}

			s, ok := h.sessions.Get(attach.SessionId)
			if !ok {
				h.log.Debug("client WS attach: session not found", "session_id", attach.SessionId)
				continue
			}

			// Detach from previous session if attached
			if attachedSession != nil {
				attachedSession.Detach(connID)
				attachedSession = nil
			}

			// Replay history logic
			entries, okRange, earliest, latest := s.Ring.Replay(uint64(attach.LastSeq))
			if !okRange {
				// Dispatch replay_unavailable control frame before switching to live
				ru := coordinator_app.ReplayUnavailableJson{
					Type:                 "replay_unavailable",
					V:                    1,
					SessionId:            s.SessionID,
					EarliestAvailableSeq: int(earliest),
					CurrentSeq:           int(latest),
				}
				ruBytes, _ := json.Marshal(ru)
				select {
				case sendChan <- ruBytes:
				default:
				}
			} else {
				// Replay matching history
				for _, entry := range entries {
					select {
					case sendChan <- entry.Payload:
					default:
					}
				}
			}

			// Attach to receive live frames
			s.Attach(&attachedClient{id: connID, sendChan: sendChan})
			attachedSession = s
			h.log.Info("client attached to session", "client_id", hello.ClientId, "session_id", s.SessionID)

		case "detach":
			var detach coordinator_app.DetachJson
			if err := json.Unmarshal(data, &detach); err != nil {
				h.log.Debug("client WS malformed detach", "err", err)
				continue
			}

			if attachedSession != nil && attachedSession.SessionID == detach.SessionId {
				attachedSession.Detach(connID)
				attachedSession = nil
				h.log.Info("client detached from session", "client_id", hello.ClientId, "session_id", detach.SessionId)
			}

		default:
			h.log.Info("client WS unknown frame type (ignored)", "type", peek.Type)
		}
	}
}
