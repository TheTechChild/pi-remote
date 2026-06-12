// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-coordinator/internal/push"
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
	machines *machines.Registry
	focus    *push.FocusTracker // nil-safe: focus tracking disabled when nil
	log      *slog.Logger

	subsMu sync.RWMutex
	subs   map[string]chan []byte

	spawnsMu sync.RWMutex
	spawns   map[string]chan []byte
}

func (h *clientWS) buildMachineListFrame() ([]byte, error) {
	machs := h.machines.List()
	sessList := h.sessions.List()

	// Group sessions by machine ID
	sessByMachine := make(map[string][]coordinator_app.MachineListJsonMachinesElemSessionsElem)
	for _, s := range sessList {
		sID := s.SessionID
		mID := s.MachineID
		sState := s.GetState()
		sMeta := s.GetMetadata()

		metaBytes, _ := json.Marshal(sMeta)
		var appMeta coordinator_app.MachineListJsonMachinesElemSessionsElemMetadata
		_ = json.Unmarshal(metaBytes, &appMeta)

		elem := coordinator_app.MachineListJsonMachinesElemSessionsElem{
			SessionId: sID,
			State:     coordinator_app.MachineListJsonMachinesElemSessionsElemState(sState),
			Metadata:  appMeta,
		}
		sessByMachine[mID] = append(sessByMachine[mID], elem)
	}

	appMachs := make([]coordinator_app.MachineListJsonMachinesElem, 0, len(machs))
	for _, m := range machs {
		state := coordinator_app.MachineListJsonMachinesElemStateOnline
		if m.State == "suspended" {
			state = coordinator_app.MachineListJsonMachinesElemStateSuspended
		}

		sessions := sessByMachine[m.ID]
		if sessions == nil {
			sessions = []coordinator_app.MachineListJsonMachinesElemSessionsElem{}
		}

		appMachs = append(appMachs, coordinator_app.MachineListJsonMachinesElem{
			MachineId:          m.ID,
			MachineDisplayName: m.DisplayName,
			State:              state,
			Sessions:           sessions,
		})
	}

	frame := coordinator_app.MachineListJson{
		Type:     "machine_list",
		V:        1,
		Machines: appMachs,
	}

	return json.Marshal(frame)
}

func (h *clientWS) broadcastMachineList() {
	b, err := h.buildMachineListFrame()
	if err != nil {
		h.log.Error("failed to build machine list frame", "err", err)
		return
	}

	h.subsMu.RLock()
	defer h.subsMu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- b:
		default:
			// Queue full or client slow, skip
		}
	}
}

func (h *clientWS) handleSpawnResponse(reqID string, success bool, sessionID *string, errStr *string) {
	h.spawnsMu.Lock()
	ch, ok := h.spawns[reqID]
	delete(h.spawns, reqID)
	h.spawnsMu.Unlock()

	if !ok {
		h.log.Warn("spawn response: request_id not tracked", "request_id", reqID)
		return
	}

	resp := coordinator_app.SpawnResponseJson{
		Type:      "spawn_response",
		V:         1,
		RequestId: reqID,
		Success:   success,
	}
	if success && sessionID != nil {
		resp.SessionId = sessionID
	} else if errStr != nil {
		resp.Error = errStr
	} else {
		errVal := "spawn failed"
		resp.Error = &errVal
	}

	b, err := json.Marshal(resp)
	if err != nil {
		h.log.Error("failed to marshal spawn_response", "err", err)
		return
	}

	select {
	case ch <- b:
	default:
		h.log.Warn("failed to send spawn response to client (channel full)")
	}
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
		h.subsMu.Lock()
		if h.subs != nil {
			delete(h.subs, connID)
		}
		h.subsMu.Unlock()

		if attachedSession != nil {
			attachedSession.Detach(connID)
		}
		// A closed connection implicitly flips all of this client's
		// sessions to unfocused (issue #25, SPEC § 9.7).
		if h.focus != nil {
			h.focus.DropConn(connID)
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

			// Replay + attach atomically: frames published while we replay
			// can neither be missed nor double-delivered (SPEC.md § 18.4).
			replayed, okRange := s.AttachWithReplay(
				&attachedClient{id: connID, sendChan: sendChan},
				uint64(attach.LastSeq),
				func(earliest, latest uint64) []byte {
					ru := coordinator_app.ReplayUnavailableJson{
						Type:                 "replay_unavailable",
						V:                    1,
						SessionId:            s.SessionID,
						EarliestAvailableSeq: int(earliest),
						CurrentSeq:           int(latest),
					}
					ruBytes, _ := json.Marshal(ru)
					return ruBytes
				},
			)
			attachedSession = s
			h.log.Info("client attached to session",
				"client_id", hello.ClientId, "session_id", s.SessionID,
				"replayed", replayed, "replay_ok", okRange)

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

		case "subscribe_machine_list":
			h.subsMu.Lock()
			if h.subs == nil {
				h.subs = make(map[string]chan []byte)
			}
			h.subs[connID] = sendChan
			h.subsMu.Unlock()

			// Immediately send the current snapshot
			b, err := h.buildMachineListFrame()
			if err != nil {
				h.log.Error("failed to build machine list snapshot", "err", err)
				continue
			}
			select {
			case sendChan <- b:
			default:
			}

		case "spawn_session":
			var spawn coordinator_app.SpawnSessionJson
			if err := json.Unmarshal(data, &spawn); err != nil {
				h.log.Debug("client WS malformed spawn_session", "err", err)
				continue
			}

			// Verify target machine is registered and online
			mach, ok := h.machines.Get(spawn.MachineId)
			if !ok || mach.State != "online" || mach.Conn == nil {
				errStr := "Machine offline"
				resp := coordinator_app.SpawnResponseJson{
					Type:      "spawn_response",
					V:         1,
					RequestId: spawn.RequestId,
					Success:   false,
					Error:     &errStr,
				}
				b, _ := json.Marshal(resp)
				select {
				case sendChan <- b:
				default:
				}
				continue
			}

			// Store request_id -> client sendChan
			h.spawnsMu.Lock()
			if h.spawns == nil {
				h.spawns = make(map[string]chan []byte)
			}
			h.spawns[spawn.RequestId] = sendChan
			h.spawnsMu.Unlock()

			// Forward spawn_request to the daemon connection
			var po daemon_coordinator.SpawnRequestJsonProjectOverride
			if spawn.ProjectOverride != nil {
				po = daemon_coordinator.SpawnRequestJsonProjectOverride(spawn.ProjectOverride)
			}
			daemonReq := daemon_coordinator.SpawnRequestJson{
				Type:            "spawn_request",
				V:               1,
				RequestId:       spawn.RequestId,
				Cwd:             spawn.Cwd,
				ProjectOverride: po,
			}
			reqBytes, err := json.Marshal(daemonReq)
			if err != nil {
				h.log.Error("failed to marshal spawn request for daemon", "err", err)
				h.handleSpawnResponse(spawn.RequestId, false, nil, &[]string{"Internal marshal error"}[0])
				continue
			}

			err = mach.Conn.Write(ctx, websocket.MessageText, reqBytes)
			if err != nil {
				h.log.Warn("failed to write spawn request to daemon", "err", err)
				h.handleSpawnResponse(spawn.RequestId, false, nil, &[]string{"Daemon write error"}[0])
			}

		case "pty_input":
			var input coordinator_app.PtyInputJson
			if err := json.Unmarshal(data, &input); err != nil {
				h.log.Debug("client WS malformed pty_input", "err", err)
				continue
			}

			s, ok := h.sessions.Get(input.SessionId)
			if !ok {
				continue
			}

			mach, ok := h.machines.Get(s.MachineID)
			if !ok || mach.State != "online" || mach.Conn == nil {
				continue
			}

			daemonInput := daemon_coordinator.PtyInputJson{
				Type:      "pty_input",
				V:         1,
				SessionId: input.SessionId,
				ClientId:  hello.ClientId,
				Bytes:     input.Bytes,
			}
			inputBytes, err := json.Marshal(daemonInput)
			if err != nil {
				h.log.Error("failed to marshal pty_input", "err", err)
				continue
			}

			err = mach.Conn.Write(ctx, websocket.MessageText, inputBytes)
			if err != nil {
				h.log.Warn("failed to write pty_input to daemon", "err", err)
			}

		case "pty_resize":
			var resize struct {
				SessionId string `json:"session_id"`
				Cols      int    `json:"cols"`
				Rows      int    `json:"rows"`
			}
			if err := json.Unmarshal(data, &resize); err != nil {
				h.log.Debug("client WS malformed pty_resize", "err", err)
				continue
			}

			s, ok := h.sessions.Get(resize.SessionId)
			if !ok {
				continue
			}

			mach, ok := h.machines.Get(s.MachineID)
			if !ok || mach.State != "online" || mach.Conn == nil {
				continue
			}

			daemonResize := daemon_coordinator.PtyResizeJson{
				Type:      "pty_resize",
				V:         1,
				SessionId: resize.SessionId,
				Cols:      resize.Cols,
				Rows:      resize.Rows,
			}
			resizeBytes, err := json.Marshal(daemonResize)
			if err != nil {
				h.log.Error("failed to marshal pty_resize", "err", err)
				continue
			}

			err = mach.Conn.Write(ctx, websocket.MessageText, resizeBytes)
			if err != nil {
				h.log.Warn("failed to write pty_resize to daemon", "err", err)
			}

		case "client_focus":
			var focus coordinator_app.ClientFocusJson
			if err := json.Unmarshal(data, &focus); err != nil {
				h.log.Debug("client WS malformed client_focus", "err", err)
				continue
			}
			if h.focus != nil {
				h.focus.SetFocus(connID, hello.ClientId, focus.SessionId, focus.Focused)
			}
			h.log.Debug("client WS client_focus", "session_id", focus.SessionId, "focused", focus.Focused)

		default:
			h.log.Info("client WS unknown frame type (ignored)", "type", peek.Type)
		}
	}
}
