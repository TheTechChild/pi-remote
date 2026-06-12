// SPDX-License-Identifier: MIT
package machines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// Sentinel errors returned by Ingestor.Handle. The WS handler translates
// these into close 1008 (StatusPolicyViolation).
var (
	// ErrFirstFrameNotRegister is returned when a daemon WebSocket's first
	// frame is anything other than machine_register. The gate is per-conn:
	// even if the machine is already registered via another connection,
	// this conn still must open with machine_register.
	ErrFirstFrameNotRegister = errors.New("ingest: first frame must be machine_register")

	// ErrMalformedFrame is returned when the bytes are not a valid JSON
	// object. Non-object JSON (arrays, scalars, null) is also malformed.
	ErrMalformedFrame = errors.New("ingest: malformed frame")
)

// Ingestor dispatches raw daemon-coordinator frames into the machines and
// sessions registries. There is one Ingestor per coordinator process; per-
// connection state (e.g. "has this conn registered yet?") lives on the
// Ingestor's connState map keyed by sourceConn.
//
// The Ingestor does NOT manage the WebSocket itself: Handle takes raw
// bytes already pulled off the wire by the WS handler. This decoupling
// lets ingest_test.go exercise the full state machine without sockets.
//
// No fan-out happens here in Workstream C — that's the broker, Batch 3.
type Ingestor struct {
	machines *Registry
	sessions *sessions.Registry
	log      *slog.Logger

	mu              sync.Mutex
	connState       map[Conn]*connState
	onMachineChange func()
	onSpawnResponse func(reqID string, success bool, sessionID *string, errStr *string)
}

func (i *Ingestor) SetOnMachineChange(fn func()) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.onMachineChange = fn
}

func (i *Ingestor) SetOnSpawnResponse(fn func(reqID string, success bool, sessionID *string, errStr *string)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.onSpawnResponse = fn
}

func (i *Ingestor) notifyChange() {
	if i.onMachineChange != nil {
		i.onMachineChange()
	}
}

type connState struct {
	registered bool
	machineID  string
}

// NewIngestor constructs an Ingestor. log must not be nil.
func NewIngestor(m *Registry, s *sessions.Registry, log *slog.Logger) *Ingestor {
	return &Ingestor{
		machines:  m,
		sessions:  s,
		log:       log,
		connState: make(map[Conn]*connState),
	}
}

// Handle dispatches one frame. ctx is the per-conn context the WS handler
// holds; it's currently unused but reserved for the broker hookup. Returns
// nil on accepted frames (including unknown-type forward-compat) and
// sentinel errors for fatal cases the WS handler must translate to close
// 1008.
func (i *Ingestor) Handle(_ context.Context, sourceConn Conn, b []byte) error {
	// Top-level type discrimination. We deliberately do NOT unmarshal into
	// the generated types first, because their UnmarshalJSON enforces
	// strict required-field validation per the schema — which is desirable
	// for the well-formed paths but means we lose the "unknown type"
	// forward-compat case. Instead, peek at "type", then decode into the
	// specific generated struct.
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedFrame, err)
	}
	if !looksLikeJSONObject(b) {
		return ErrMalformedFrame
	}

	st := i.stateFor(sourceConn)

	// First-frame gate: per-conn.
	if !st.registered && head.Type != "machine_register" {
		return ErrFirstFrameNotRegister
	}

	switch head.Type {
	case "machine_register":
		return i.handleMachineRegister(sourceConn, st, b)
	case "session_started":
		return i.handleSessionStarted(b)
	case "session_event":
		return i.handleSessionEvent(b)
	case "session_pty":
		return i.handleSessionPty(b)
	case "session_state_change":
		return i.handleSessionStateChange(b)
	case "session_ended":
		return i.handleSessionEnded(b)
	case "session_resume":
		return i.handleSessionResume(st, b)
	case "spawn_response":
		return i.handleSpawnResponse(b)
	case "machine_suspending":
		return i.handleMachineSuspending(st, b)
	default:
		i.log.Info("ingest: unknown frame type (forward-compat, ignored)",
			"type", head.Type, "machine_id", st.machineID)
		return nil
	}
}

// ForgetConn drops per-conn state. Called by the WS handler in its defer
// path after the registry-side UnregisterByConn runs.
func (i *Ingestor) ForgetConn(c Conn) {
	i.mu.Lock()
	delete(i.connState, c)
	i.mu.Unlock()
}

// MachineIDForConn returns the machine_id this conn registered as, or
// ("", false) if it never sent a machine_register frame. Used by the WS
// handler's defer path to know which sessions to pause on socket close.
func (i *Ingestor) MachineIDForConn(c Conn) (string, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	s, ok := i.connState[c]
	if !ok || !s.registered {
		return "", false
	}
	return s.machineID, true
}

func (i *Ingestor) stateFor(c Conn) *connState {
	i.mu.Lock()
	defer i.mu.Unlock()
	if s, ok := i.connState[c]; ok {
		return s
	}
	s := &connState{}
	i.connState[c] = s
	return s
}

func (i *Ingestor) handleMachineRegister(conn Conn, st *connState, b []byte) error {
	var msg daemon_coordinator.MachineRegisterJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: machine_register: %v", ErrMalformedFrame, err)
	}
	caps := make([]string, 0, len(msg.Capabilities))
	for _, c := range msg.Capabilities {
		caps = append(caps, string(c))
	}
	i.machines.Register(msg.MachineId, msg.MachineDisplayName, msg.DaemonVersion, caps, conn)
	st.registered = true
	st.machineID = msg.MachineId
	i.log.Info("machine_register accepted",
		"machine_id", msg.MachineId,
		"display_name", msg.MachineDisplayName,
		"daemon_version", msg.DaemonVersion,
		"capabilities", caps)
	i.notifyChange()
	return nil
}

func (i *Ingestor) handleSessionStarted(b []byte) error {
	var msg daemon_coordinator.SessionStartedJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_started: %v", ErrMalformedFrame, err)
	}
	i.sessions.Register(msg.SessionId, msg.MachineId, msg.Metadata)
	i.log.Info("session_started",
		"session_id", msg.SessionId, "machine_id", msg.MachineId)
	i.notifyChange()
	return nil
}

func (i *Ingestor) handleSessionEvent(b []byte) error {
	var msg daemon_coordinator.SessionEventJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_event: %v", ErrMalformedFrame, err)
	}
	advanced := i.sessions.AdvanceSeq(msg.SessionId, msg.Seq)
	if !advanced {
		i.log.Debug("session_event stale (no regress)",
			"session_id", msg.SessionId, "seq", msg.Seq)
		return nil
	}

	// daemon-coordinator and coordinator-app session_event schemas are
	// identical, so the raw daemon frame is forwarded as-is (ring +
	// attached clients, atomically; SPEC.md § 8.7).
	i.sessions.Publish(msg.SessionId, broker.Entry{
		Seq:     uint64(msg.Seq),
		Kind:    broker.EntryKindEvent,
		Ts:      int64(msg.Ts),
		Payload: b,
	})
	return nil
}

func (i *Ingestor) handleSessionPty(b []byte) error {
	var msg daemon_coordinator.SessionPtyJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_pty: %v", ErrMalformedFrame, err)
	}
	advanced := i.sessions.AdvanceSeq(msg.SessionId, msg.Seq)
	if !advanced {
		i.log.Debug("session_pty stale (no regress)",
			"session_id", msg.SessionId, "seq", msg.Seq)
		return nil
	}

	// Schemas are pass-through compatible; see handleSessionEvent.
	i.sessions.Publish(msg.SessionId, broker.Entry{
		Seq:     uint64(msg.Seq),
		Kind:    broker.EntryKindPty,
		Ts:      int64(msg.Ts),
		Payload: b,
	})
	return nil
}

func (i *Ingestor) handleSessionStateChange(b []byte) error {
	var msg daemon_coordinator.SessionStateChangeJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_state_change: %v", ErrMalformedFrame, err)
	}
	i.sessions.SetState(msg.SessionId, string(msg.To))
	i.sessions.AdvanceSeq(msg.SessionId, msg.Seq)
	i.forwardStateChange(msg.SessionId, string(msg.From), string(msg.To))
	i.log.Info("session_state_change",
		"session_id", msg.SessionId, "from", string(msg.From), "to", string(msg.To))
	i.notifyChange()
	return nil
}

// forwardStateChange builds the coordinator-app session_state_change frame
// (machine_id added, seq/ts dropped per the coordinator-app schema) and
// broadcasts it to the session's attached clients. SPEC.md §§ 8.7, 10.3.
func (i *Ingestor) forwardStateChange(sessionID, from, to string) {
	s, ok := i.sessions.Get(sessionID)
	if !ok {
		return
	}
	frame := coordinator_app.SessionStateChangeJson{
		Type:      "session_state_change",
		V:         1,
		SessionId: sessionID,
		MachineId: s.MachineID,
		From:      coordinator_app.State(from),
		To:        coordinator_app.State(to),
	}
	b, err := json.Marshal(frame)
	if err != nil {
		i.log.Error("forwardStateChange marshal", "err", err)
		return
	}
	s.Broadcast(b)
}

func (i *Ingestor) handleSessionEnded(b []byte) error {
	var msg daemon_coordinator.SessionEndedJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_ended: %v", ErrMalformedFrame, err)
	}
	i.sessions.AdvanceSeq(msg.SessionId, msg.Seq)
	i.sessions.MarkEnded(msg.SessionId)
	if s, ok := i.sessions.Get(msg.SessionId); ok {
		// Forward to attached clients with machine_id added (the
		// coordinator-app schema drops seq). SPEC.md §§ 8.7, 10.3.
		frame := coordinator_app.SessionEndedJson{
			Type:      "session_ended",
			V:         1,
			SessionId: msg.SessionId,
			MachineId: s.MachineID,
			Reason:    coordinator_app.SessionEndedJsonReason(msg.Reason),
		}
		if fb, err := json.Marshal(frame); err == nil {
			s.Broadcast(fb)
		} else {
			i.log.Error("session_ended forward marshal", "err", err)
		}
	}
	i.log.Info("session_ended",
		"session_id", msg.SessionId, "reason", string(msg.Reason))
	i.notifyChange()
	return nil
}

func (i *Ingestor) handleSessionResume(st *connState, b []byte) error {
	var msg daemon_coordinator.SessionResumeJson
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: session_resume: %v", ErrMalformedFrame, err)
	}
	if _, ok := i.sessions.Get(msg.SessionId); ok {
		// Known session: re-register, restore LastSeq via max.
		i.sessions.SetState(msg.SessionId, "running")
		i.sessions.RestoreLastSeq(msg.SessionId, msg.LastSeqEmitted)
		i.log.Info("session_resume (known)",
			"session_id", msg.SessionId, "last_seq_emitted", msg.LastSeqEmitted)
		i.notifyChange()
		return nil
	}
	// Unknown: create entry. We don't have a SessionStartedJsonMetadata
	// here (resume carries a free-form map), so we synthesize an empty
	// metadata struct. The session is bound to the daemon's machine_id
	// via the connection's registered state.
	var emptyMeta daemon_coordinator.SessionStartedJsonMetadata
	i.sessions.Register(msg.SessionId, st.machineID, emptyMeta)
	i.sessions.RestoreLastSeq(msg.SessionId, msg.LastSeqEmitted)
	i.log.Info("session_resume (unknown; coordinator-restart recovery)",
		"session_id", msg.SessionId, "machine_id", st.machineID,
		"last_seq_emitted", msg.LastSeqEmitted)
	i.notifyChange()
	return nil
}

func (i *Ingestor) handleMachineSuspending(st *connState, _ []byte) error {
	i.machines.SetSuspended(st.machineID)
	i.sessions.PauseAllForMachine(st.machineID)
	i.log.Info("machine_suspending", "machine_id", st.machineID)
	i.notifyChange()
	return nil
}

type daemonSpawnResponse struct {
	Type       string  `json:"type"`
	V          int     `json:"v"`
	RequestID  string  `json:"request_id"`
	Success    bool    `json:"success"`
	SessionID  *string `json:"session_id,omitempty"`
	TmuxTarget *string `json:"tmux_target,omitempty"`
	Error      *string `json:"error,omitempty"`
}

func (i *Ingestor) handleSpawnResponse(b []byte) error {
	var msg daemonSpawnResponse
	if err := json.Unmarshal(b, &msg); err != nil {
		return fmt.Errorf("%w: spawn_response: %v", ErrMalformedFrame, err)
	}
	i.mu.Lock()
	onSpawn := i.onSpawnResponse
	i.mu.Unlock()
	if onSpawn != nil {
		onSpawn(msg.RequestID, msg.Success, msg.SessionID, msg.Error)
	}
	return nil
}

func looksLikeJSONObject(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}
