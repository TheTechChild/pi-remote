// SPDX-License-Identifier: MIT
package socket

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"

	pb "github.com/TheTechChild/pi-remote-daemon/internal/proto/extension-daemon"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// maxFrameSize bounds JSONL lines on the extension socket. Pty bytes do not
// flow through this socket — control messages alone are well under 1MB.
const maxFrameSize = 1 << 20

// Handler dispatches JSONL frames from a single extension connection into
// the shared session registry.
type Handler struct {
	registry *session.Registry
	log      *slog.Logger
}

func NewHandler(reg *session.Registry, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{registry: reg, log: log}
}

// frameType peeks at just the `type` discriminator without binding the rest
// of the payload — the per-message struct's UnmarshalJSON does the strict
// schema check.
type frameType struct {
	Type string `json:"type"`
}

// Serve owns c's lifecycle: it reads JSONL frames until EOF or error, then
// closes c. The first frame must be `register`. Subsequent frames may be
// `heartbeat`, `event` (no-op for M1), or `disconnect`.
func (h *Handler) Serve(c net.Conn) {
	defer c.Close()

	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), maxFrameSize)

	// First frame: register.
	if !sc.Scan() {
		h.logScanErr(sc.Err(), "read register")
		return
	}
	sessionID, accepted := h.handleRegister(c, sc.Bytes())
	if !accepted {
		// Either the registration was rejected (ack written) or the frame
		// was malformed/wrong type (no ack written). Either way, we drop
		// the connection without touching the registry further.
		return
	}

	// Subsequent frames until EOF or `disconnect`.
	disconnected := false
	for sc.Scan() {
		if h.dispatch(sessionID, sc.Bytes()) {
			disconnected = true
			break
		}
	}
	if err := sc.Err(); err != nil && !disconnected {
		h.logScanErr(err, "read frame")
	}

	if !disconnected {
		// Connection dropped without an explicit disconnect frame: keep the
		// registry entry but flip its state to ended (deferred reaping in
		// a later milestone).
		h.registry.MarkEnded(sessionID)
		h.log.Info("session connection closed",
			slog.String("session_id", sessionID),
			slog.String("reason", "conn_closed"))
	}
}

// handleRegister parses the first frame and either writes a register_ack
// and returns (sessionID, true) on accept, or writes a rejection ack and
// returns ("", false), or returns ("", false) silently for malformed input.
func (h *Handler) handleRegister(c net.Conn, line []byte) (string, bool) {
	var ft frameType
	if err := json.Unmarshal(line, &ft); err != nil {
		h.log.Warn("malformed first frame", slog.String("err", err.Error()))
		return "", false
	}
	if ft.Type != "register" {
		h.log.Warn("first frame was not register", slog.String("type", ft.Type))
		return "", false
	}
	var reg pb.RegisterJson
	if err := json.Unmarshal(line, &reg); err != nil {
		h.log.Warn("invalid register payload", slog.String("err", err.Error()))
		return "", false
	}

	spawnToken := ""
	if reg.SpawnToken != nil {
		spawnToken = *reg.SpawnToken
	}
	s := &session.Session{
		SessionID:       reg.SessionId,
		SpawnToken:      spawnToken,
		TmuxTarget:      reg.TmuxTarget,
		PID:             reg.Pid,
		CWD:             reg.Cwd,
		ProjectName:     reg.ProjectName,
		Hostname:        reg.Hostname,
		Model:           reg.Model,
		StartedAt:       time.UnixMilli(int64(reg.StartedAt)),
		State:           session.StateRunning,
		AttachedClients: map[string]bool{},
	}
	accepted, reason := h.registry.Register(s)
	if err := writeAck(c, reg.SessionId, accepted, reason); err != nil {
		h.log.Warn("write register_ack", slog.String("err", err.Error()))
		return "", false
	}
	if !accepted {
		h.log.Info("register rejected",
			slog.String("session_id", reg.SessionId),
			slog.String("reason", reason))
		return "", false
	}
	h.log.Info("register accepted",
		slog.String("session_id", reg.SessionId),
		slog.Int("pid", reg.Pid))
	return reg.SessionId, true
}

// dispatch returns true when the session disconnected (peer or local) and
// the read loop should exit.
func (h *Handler) dispatch(sessionID string, line []byte) (done bool) {
	var ft frameType
	if err := json.Unmarshal(line, &ft); err != nil {
		h.log.Warn("malformed frame, dropping connection",
			slog.String("session_id", sessionID),
			slog.String("err", err.Error()))
		return true
	}

	switch ft.Type {
	case "heartbeat":
		var hb pb.HeartbeatJson
		if err := json.Unmarshal(line, &hb); err != nil {
			h.log.Warn("invalid heartbeat", slog.String("err", err.Error()))
			return true
		}
		// TODO(M3): heartbeat-timeout detection (3 missed → unresponsive
		// per SPEC § 12.2). M1 only records LastHeartbeat.
		if err := h.registry.UpdateHeartbeat(sessionID, time.UnixMilli(int64(hb.Ts))); err != nil {
			h.log.Warn("heartbeat for unknown session",
				slog.String("session_id", sessionID))
			return true
		}
		h.log.Debug("heartbeat", slog.String("session_id", sessionID))
		return false

	case "event":
		// M1 acknowledges events but does not project them. Coordinator
		// fan-out lands in M3/M4.
		return false

	case "disconnect":
		var d pb.DisconnectJson
		if err := json.Unmarshal(line, &d); err != nil {
			h.log.Warn("invalid disconnect frame", slog.String("err", err.Error()))
			return true
		}
		h.registry.Remove(sessionID)
		h.log.Info("session disconnected",
			slog.String("session_id", sessionID),
			slog.String("reason", string(d.Reason)))
		return true

	default:
		h.log.Warn("unknown frame type",
			slog.String("session_id", sessionID),
			slog.String("type", ft.Type))
		return false
	}
}

// writeAck serializes a register_ack and writes it as a JSONL frame.
func writeAck(c net.Conn, sessionID string, accepted bool, reason string) error {
	ack := map[string]any{
		"type":       "register_ack",
		"v":          1,
		"session_id": sessionID,
		"accepted":   accepted,
	}
	if !accepted {
		ack["reason"] = reason
	}
	b, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.Write(b)
	return err
}

func (h *Handler) logScanErr(err error, ctx string) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	h.log.Warn("scan error",
		slog.String("ctx", ctx),
		slog.String("err", err.Error()))
}
