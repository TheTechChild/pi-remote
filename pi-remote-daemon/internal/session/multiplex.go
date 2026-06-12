// SPDX-License-Identifier: MIT
package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"syscall"
	"time"

	"github.com/TheTechChild/pi-remote-daemon/internal/ptymux"
)

// Coord is the contract the multiplex requires from the coordinator
// client. Defined in this package (the consumer) per Go's
// accept-interfaces-return-structs convention - keeps the interface
// small, breaks the import cycle, and lets multiplex_test.go ship a
// tiny in-test stub without pulling the real coordinator package.
//
// Connected reports whether the client currently has a live WebSocket
// to the coordinator. The multiplex uses this for the drop-on-disconnect
// policy: events fired while !Connected allocate a seq (so the
// coordinator's gap detection sees the right numbers post-reconnect)
// but the frame is discarded. No buffering. Per SPEC § 7.8 and the
// Batch 2 plan.
//
// Send writes a single JSON frame to the coordinator. The argument is
// one of the typed frames returned by internal/coordinator/frames.go.
// Returns a non-nil error when the underlying conn is unavailable or
// the write fails; the multiplex treats any error the same as
// !Connected (seq advanced, frame discarded).
type Coord interface {
	Connected() bool
	Send(frame any) error
}

// LiveSession is the snapshot the multiplex returns from LiveSessions()
// for the coordinator client to drive session_resume emission on
// reconnect. Session is a value copy; LastSeq is the allocator's
// current Peek value at snapshot time.
type LiveSession struct {
	Session Session
	LastSeq uint64
}

// FrameBuilder is the seam between the multiplex and the typed-frame
// helpers in internal/coordinator. Defined as an interface in the
// session package per the same accept-interfaces idiom that justifies
// Coord here: each frame method is what the multiplex needs to emit a
// specific upstream frame, and the coordinator package implements them
// in frames.go. The seam keeps session from importing coordinator.
type FrameBuilder interface {
	SessionStarted(s Session, machineID, hostnameFallback string) any
	SessionEvent(sessionID string, seq uint64, kind string, ts time.Time, data map[string]any) any
	SessionEnded(sessionID string, seq uint64, kind EndedKind, reason string) any
}

// extEventFrame is the shape of the extension's `event` JSONL frame.
// Used only to parse what the registry handed us; we don't need the
// generated extension-daemon proto here because the multiplex consumes
// only kind + data and re-emits via FrameBuilder.SessionEvent.
type extEventFrame struct {
	Type string         `json:"type"`
	Kind string         `json:"kind"`
	Ts   int64          `json:"ts"`
	Data map[string]any `json:"data"`
}

// Multiplex bridges the per-ext-connection registry events into the
// upstream coordinator WebSocket. One Multiplex per daemon process; it
// owns the per-session SeqAllocators and reads Coord.Connected() to
// decide between write and drop.
//
// The Multiplex wires itself to a Registry's callbacks at construction
// time. It does not own the registry; the registry's lifetime is the
// daemon process and outlives any individual coordinator reconnect.
type Multiplex struct {
	reg       *Registry
	coord     Coord
	frames    FrameBuilder
	machineID string
	now       func() time.Time

	mu         sync.Mutex
	seqs       map[string]*SeqAllocator
	sanitizers map[string]*ptymux.Sanitizer

	// pidAlive probes process existence for LiveSessions (issue #16);
	// defaultPidAlive in production, injectable in tests.
	pidAlive func(int) bool
}

// NewMultiplex wires the registry's hooks to the multiplex and binds
// the multiplex to the supplied Coord and FrameBuilder. The machineID
// is included in session_started frames per the schema. The now
// function stamps outgoing frame timestamps; pass time.Now in
// production.
func NewMultiplex(reg *Registry, coord Coord, frames FrameBuilder, machineID string, now func() time.Time) *Multiplex {
	if now == nil {
		now = time.Now
	}
	m := &Multiplex{
		reg:        reg,
		coord:      coord,
		frames:     frames,
		machineID:  machineID,
		now:        now,
		seqs:       make(map[string]*SeqAllocator),
		sanitizers: make(map[string]*ptymux.Sanitizer),
		pidAlive:   defaultPidAlive,
	}
	reg.OnRegister(m.onRegister)
	reg.OnEvent(m.onEvent)
	reg.OnEnded(m.onEnded)
	reg.OnStateChange(m.onStateChange)
	// OnHeartbeat intentionally NOT wired: heartbeats are ext-side only,
	// the coordinator infers liveness from frame cadence + WebSocket
	// ping. M3 in plan and #11 acceptance.
	return m
}

// allocFor returns the SeqAllocator for sessionID, creating it on
// first use. Concurrent-safe.
func (m *Multiplex) allocFor(sessionID string) *SeqAllocator {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.seqs[sessionID]
	if !ok {
		a = &SeqAllocator{}
		m.seqs[sessionID] = a
	}
	return a
}

// dropAlloc removes a session's allocator. Called from onEnded after
// the final session_ended frame; LiveSessions excludes ended sessions,
// so the allocator is no longer needed.
func (m *Multiplex) dropAlloc(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.seqs, sessionID)
}

// onRegister handles a new session's initial registration: emit
// session_started upstream. session_started does NOT carry a seq per
// the schema (it's a session-level event, not a per-session frame).
func (m *Multiplex) onRegister(s *Session) {
	// Force allocator creation now so LiveSessions reports this
	// session even before its first event.
	_ = m.allocFor(s.SessionID)
	frame := m.frames.SessionStarted(*s, m.machineID, "")
	if !m.coord.Connected() {
		return
	}
	if err := m.coord.Send(frame); err != nil {
		slog.Debug("multiplex: session_started send failed",
			slog.String("session_id", s.SessionID),
			slog.String("err", err.Error()))
	}
}

// onEvent handles an extension-side event frame: parse it, allocate
// the next seq for this session (which advances LastSeq regardless of
// connection state), and if connected, send the upstream
// session_event. If the input is malformed, log and return WITHOUT
// allocating a seq - the multiplex never built a frame for that input
// (M17 design call).
func (m *Multiplex) onEvent(sessionID string, eventBytes []byte) {
	var raw extEventFrame
	if err := json.Unmarshal(eventBytes, &raw); err != nil || raw.Kind == "" {
		slog.Warn("multiplex: malformed event from ext, dropping",
			slog.String("session_id", sessionID),
			slog.String("err", errString(err)))
		return
	}
	// Validate kind against the daemon-coordinator enum by attempting
	// a round-trip through the FrameBuilder; an unknown kind here
	// would still pass the frame on, deferring schema enforcement to
	// the coordinator. M17 specifies that unknown kinds are dropped at
	// the multiplex.
	if !isKnownEventKind(raw.Kind) {
		slog.Warn("multiplex: unknown event kind, dropping",
			slog.String("session_id", sessionID),
			slog.String("kind", raw.Kind))
		return
	}

	seq := m.allocFor(sessionID).Next()
	ts := time.UnixMilli(raw.Ts)
	if raw.Ts == 0 {
		ts = m.now()
	}
	frame := m.frames.SessionEvent(sessionID, seq, raw.Kind, ts, raw.Data)
	if !m.coord.Connected() {
		return
	}
	if err := m.coord.Send(frame); err != nil {
		slog.Debug("multiplex: session_event send failed",
			slog.String("session_id", sessionID),
			slog.Uint64("seq", seq),
			slog.String("err", err.Error()))
	}
}

// onEnded handles either terminal path: emit session_ended upstream
// with a seq slot from the allocator. After emission, drop the
// allocator. Per M9 + plan: session_ended consumes a seq even though
// the schema doesn't strictly require monotonic seq across
// session-level frames.
func (m *Multiplex) onEnded(s *Session, kind EndedKind, reason string) {
	seq := m.allocFor(s.SessionID).Next()
	defer m.dropAlloc(s.SessionID)

	frame := m.frames.SessionEnded(s.SessionID, seq, kind, reason)
	if !m.coord.Connected() {
		return
	}
	if err := m.coord.Send(frame); err != nil {
		slog.Debug("multiplex: session_ended send failed",
			slog.String("session_id", s.SessionID),
			slog.Uint64("seq", seq),
			slog.String("err", err.Error()))
	}
}

// onStateChange forwards daemon-detected state transitions (today:
// unresponsive flips + recovery, issue #42) as session_state_change
// frames so the coordinator can update clients and fire pushes.
func (m *Multiplex) onStateChange(id string, from, to SessionState) {
	seq := m.allocFor(id).Next()
	frame := map[string]any{
		"type":       "session_state_change",
		"v":          1,
		"session_id": id,
		"seq":        seq,
		"ts":         m.now().UnixMilli(),
		"from":       string(from),
		"to":         string(to),
	}
	if !m.coord.Connected() {
		return
	}
	if err := m.coord.Send(frame); err != nil {
		slog.Debug("multiplex: session_state_change send failed",
			slog.String("session_id", id), slog.String("err", err.Error()))
	}
}

// SendPty assigns a sequence number, base64 encodes the terminal data, and sends the session_pty frame.
// Outbound bytes pass the D8 title-spoof sanitizer first (issue #17).
func (m *Multiplex) SendPty(sessionID string, rawBytes []byte) error {
	m.mu.Lock()
	san, ok := m.sanitizers[sessionID]
	if !ok {
		san = &ptymux.Sanitizer{}
		m.sanitizers[sessionID] = san
	}
	m.mu.Unlock()
	rawBytes = san.Sanitize(rawBytes)

	seq := m.allocFor(sessionID).Next()
	frame := map[string]any{
		"type":       "session_pty",
		"v":          1,
		"session_id": sessionID,
		"seq":        int(seq),
		"bytes":      base64.StdEncoding.EncodeToString(rawBytes),
		"ts":         int(m.now().UnixMilli()),
	}
	if !m.coord.Connected() {
		return nil
	}
	return m.coord.Send(frame)
}

// SendPtyOrFrame sends any arbitrary coordinator frame upstream.
func (m *Multiplex) SendPtyOrFrame(frame any) error {
	if !m.coord.Connected() {
		return nil
	}
	return m.coord.Send(frame)
}

// LiveSessions returns a snapshot of every session in non-ended state
// with its current LastSeq. The coordinator client calls this on
// (re)connect to emit one session_resume per live session per SPEC
// § 7.8.
//
// The snapshot reads the registry's non-ended set and the multiplex's
// allocator map under their respective mutexes; LastSeq for each
// session is read via Peek (does not advance).
func (m *Multiplex) LiveSessions() []LiveSession {
	sessions := m.reg.Snapshot()
	out := make([]LiveSession, 0, len(sessions))
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range sessions {
		// Issue #16: never announce a session whose Pi process died while
		// we were disconnected (e.g. across a suspend) — the coordinator
		// would resurrect a zombie entry.
		if sessions[i].PID > 0 && !m.pidAlive(sessions[i].PID) {
			continue
		}
		var last uint64
		if a, ok := m.seqs[sessions[i].SessionID]; ok {
			last = a.Peek()
		}
		out = append(out, LiveSession{Session: sessions[i], LastSeq: last})
	}
	return out
}

// defaultPidAlive reports whether pid exists (signal 0 probe; EPERM
// still means "exists").
func defaultPidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// errString is a nil-safe error.Error wrapper for log fields.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// isKnownEventKind enforces M17's "unknown kind, drop without
// advancing seq" rule. The enum mirrors the daemon-coordinator
// session_event.kind enum from pi-remote-spec; updating that schema
// requires regen + a sync of this list (caught at GREEN time by F8's
// constant-match sentinel covering the analogous SessionEnded enum,
// and by the per-kind multiplex tests if a new kind ships).
//
// We intentionally do NOT import the generated SessionEventJsonKind
// constants here to avoid a session->proto dependency direction
// (frames.go is the only seam allowed to know about the proto). The
// trade-off: schema drift in the kind enum is detected by the kind
// round-trip tests in frames_test.go and by integration smoke.
func isKnownEventKind(k string) bool {
	switch k {
	case "agent_start", "agent_end",
		"attention_dialog",
		"compaction_start", "compaction_end",
		"extension_error",
		"model_select",
		"queue_update",
		"tool_failure":
		return true
	}
	return false
}
