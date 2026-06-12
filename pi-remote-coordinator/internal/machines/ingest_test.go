// SPDX-License-Identifier: MIT
package machines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// testHarness wires an Ingestor with fresh registries and a captured slog
// handler so tests can assert on log output where needed.
type testHarness struct {
	ing      *Ingestor
	machines *Registry
	sessions *sessions.Registry
	logBuf   *bytes.Buffer
	conn     *fakeConn
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mreg := NewRegistry()
	sreg := sessions.NewRegistry()
	return &testHarness{
		ing:      NewIngestor(mreg, sreg, logger),
		machines: mreg,
		sessions: sreg,
		logBuf:   buf,
		conn:     newFakeConn("daemon-1"),
	}
}

// frame builds a JSON frame from the given top-level fields.
func frame(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func registerFrame() map[string]any {
	return map[string]any{
		"type":                 "machine_register",
		"v":                    1,
		"machine_id":           "macbook-pro",
		"machine_display_name": "MacBook Pro",
		"daemon_version":       "0.0.1",
		"capabilities":         []string{"spawn", "mirror"},
	}
}

func sessionStartedFrame(sessionID string) map[string]any {
	return map[string]any{
		"type":       "session_started",
		"v":          1,
		"session_id": sessionID,
		"machine_id": "macbook-pro",
		"metadata": map[string]any{
			"hostname":     "macbook-pro",
			"cwd":          "/home/clayton/code",
			"model":        "claude-3-opus",
			"project_name": "pi-remote",
			"started_at":   1700000000,
		},
	}
}

func sessionEventFrame(sessionID string, seq int) map[string]any {
	return map[string]any{
		"type":       "session_event",
		"v":          1,
		"session_id": sessionID,
		"seq":        seq,
		"ts":         1700000000,
		"kind":       "agent_start",
		"data":       map[string]any{},
	}
}

func sessionPtyFrame(sessionID string, seq int) map[string]any {
	return map[string]any{
		"type":       "session_pty",
		"v":          1,
		"session_id": sessionID,
		"seq":        seq,
		"ts":         1700000000,
		"bytes":      "AAAA",
	}
}

// register helper: registers macbook-pro through the Ingestor as the first
// frame, then asserts the registry was updated. Tests building on this use
// it to skip the first-frame plumbing.
func (h *testHarness) register(t *testing.T) {
	t.Helper()
	if err := h.ing.Handle(context.Background(), h.conn, frame(t, registerFrame())); err != nil {
		t.Fatalf("Handle(machine_register): %v", err)
	}
}

// C-28: First frame machine_register accepted; entry in machines registry.
func TestIngest_RegisterFirst(t *testing.T) {
	h := newHarness(t)
	if err := h.ing.Handle(context.Background(), h.conn, frame(t, registerFrame())); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	m, ok := h.machines.Get("macbook-pro")
	if !ok {
		t.Fatal("entry not present")
	}
	if m.Conn != h.conn {
		t.Errorf("Conn mismatch")
	}
}

// C-29: First frame other than register → ErrFirstFrameNotRegister.
func TestIngest_FirstFrameNotRegister(t *testing.T) {
	h := newHarness(t)
	err := h.ing.Handle(context.Background(), h.conn, frame(t, sessionEventFrame("sess-1", 1)))
	if !errors.Is(err, ErrFirstFrameNotRegister) {
		t.Fatalf("err = %v, want ErrFirstFrameNotRegister", err)
	}
}

// C-30: session_started after register → entry in sessions registry, LastSeq=0.
func TestIngest_SessionStarted(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	if err := h.ing.Handle(context.Background(), h.conn, frame(t, sessionStartedFrame("sess-1"))); err != nil {
		t.Fatalf("Handle(session_started): %v", err)
	}
	s, ok := h.sessions.Get("sess-1")
	if !ok {
		t.Fatal("session not registered")
	}
	if s.LastSeq != 0 {
		t.Errorf("LastSeq = %d", s.LastSeq)
	}
	if s.MachineID != "macbook-pro" {
		t.Errorf("MachineID = %q", s.MachineID)
	}
}

// C-31: session_event advances LastSeq.
func TestIngest_SessionEvent_Advances(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 1)))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 5)))
	s, _ := h.sessions.Get("sess-1")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", s.LastSeq)
	}
}

// C-32: session_event seq=5 then seq=3 → LastSeq stays 5, seq=3 logged.
func TestIngest_SessionEvent_StaleLogged(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 5)))
	h.logBuf.Reset()
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 3)))
	s, _ := h.sessions.Get("sess-1")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5 (stale must not regress)", s.LastSeq)
	}
	if !bytes.Contains(h.logBuf.Bytes(), []byte("stale")) {
		t.Errorf("expected log containing 'stale', got: %s", h.logBuf.String())
	}
}

// C-33: session_event seq=5 then seq=5 → LastSeq stays 5.
func TestIngest_SessionEvent_EqualNoOp(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 5)))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 5)))
	s, _ := h.sessions.Get("sess-1")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", s.LastSeq)
	}
}

// C-34: session_pty has the same LastSeq semantics as session_event.
func TestIngest_SessionPty_AdvancesAndNoRegress(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionPtyFrame("sess-1", 7)))
	s, _ := h.sessions.Get("sess-1")
	if s.LastSeq != 7 {
		t.Errorf("LastSeq = %d, want 7", s.LastSeq)
	}
	mustHandle(t, h.ing, h.conn, frame(t, sessionPtyFrame("sess-1", 4)))
	s, _ = h.sessions.Get("sess-1")
	if s.LastSeq != 7 {
		t.Errorf("LastSeq = %d, want 7 (no regress)", s.LastSeq)
	}
}

// C-35: session_state_change updates State.
func TestIngest_SessionStateChange(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":       "session_state_change",
		"v":          1,
		"session_id": "sess-1",
		"seq":        2,
		"ts":         1700000000,
		"from":       "running",
		"to":         "idle",
	}))
	s, _ := h.sessions.Get("sess-1")
	if s.State != "idle" {
		t.Errorf("State = %q, want idle", s.State)
	}
}

// C-36: session_ended → Ended=true, retained.
func TestIngest_SessionEnded(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":       "session_ended",
		"v":          1,
		"session_id": "sess-1",
		"seq":        3,
		"reason":     "process_exit",
	}))
	s, ok := h.sessions.Get("sess-1")
	if !ok {
		t.Fatal("session not retained")
	}
	if !s.Ended {
		t.Errorf("Ended = false, want true")
	}
}

// C-37: machine_suspending → machine "suspended", all sessions "paused".
func TestIngest_MachineSuspending(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-2")))
	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type": "machine_suspending",
		"v":    1,
		"ts":   1700000000,
	}))
	m, _ := h.machines.Get("macbook-pro")
	if m.State != "suspended" {
		t.Errorf("machine State = %q, want suspended", m.State)
	}
	for _, id := range []string{"sess-1", "sess-2"} {
		s, _ := h.sessions.Get(id)
		if s.State != "paused" {
			t.Errorf("%s State = %q, want paused", id, s.State)
		}
	}
}

// C-38: session_resume on known session → re-register (state→"running"),
// LastSeq stays at max.
func TestIngest_SessionResume_Known(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 10)))
	// Mimic a pause from a suspending machine.
	h.sessions.SetState("sess-1", "paused")
	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":             "session_resume",
		"v":                1,
		"session_id":       "sess-1",
		"last_seq_emitted": 5,
		"metadata":         map[string]any{"hostname": "macbook-pro"},
	}))
	s, _ := h.sessions.Get("sess-1")
	if s.State != "running" {
		t.Errorf("State = %q, want running", s.State)
	}
	if s.LastSeq != 10 {
		t.Errorf("LastSeq = %d, want 10 (max(10,5))", s.LastSeq)
	}
}

// C-39: session_resume on unknown session → create with
// LastSeq=last_seq_emitted, State="running".
func TestIngest_SessionResume_Unknown(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":             "session_resume",
		"v":                1,
		"session_id":       "sess-new",
		"last_seq_emitted": 42,
		"metadata":         map[string]any{"hostname": "macbook-pro"},
	}))
	s, ok := h.sessions.Get("sess-new")
	if !ok {
		t.Fatal("session not created")
	}
	if s.State != "running" {
		t.Errorf("State = %q, want running", s.State)
	}
	if s.LastSeq != 42 {
		t.Errorf("LastSeq = %d, want 42", s.LastSeq)
	}
	if s.MachineID != "macbook-pro" {
		t.Errorf("MachineID = %q", s.MachineID)
	}
}

// C-40: Unknown frame type → nil error, logged. Forward-compat: the daemon
// may emit machine_resumed/spawn_response which we don't handle yet.
func TestIngest_UnknownFrameType(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	err := h.ing.Handle(context.Background(), h.conn, frame(t, map[string]any{
		"type": "some_future_frame",
		"v":    1,
	}))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !bytes.Contains(h.logBuf.Bytes(), []byte("some_future_frame")) {
		t.Errorf("expected log mentioning unknown type, got: %s", h.logBuf.String())
	}
}

// C-41: Malformed JSON → ErrMalformedFrame.
func TestIngest_MalformedJSON(t *testing.T) {
	h := newHarness(t)
	err := h.ing.Handle(context.Background(), h.conn, []byte("not-json"))
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("err = %v, want ErrMalformedFrame", err)
	}
}

// Non-object JSON (e.g. array, number, null) is also malformed.
func TestIngest_MalformedJSON_NonObject(t *testing.T) {
	h := newHarness(t)
	err := h.ing.Handle(context.Background(), h.conn, []byte(`[1,2,3]`))
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("err = %v, want ErrMalformedFrame", err)
	}
}

// C-42: Two machine_registers for same machine_id → take-over path.
// (The Ingestor stamps its sourceConn into the registry; if the SAME conn
// re-registers, we just refresh; if a DIFFERENT conn re-registers, the
// previous one is closed by the registry. We exercise the latter here.)
func TestIngest_DoubleRegister_TakeOver(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mreg := NewRegistry()
	sreg := sessions.NewRegistry()
	ing := NewIngestor(mreg, sreg, logger)

	first := newFakeConn("first")
	second := newFakeConn("second")
	if err := ing.Handle(context.Background(), first, frame(t, registerFrame())); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := ing.Handle(context.Background(), second, frame(t, registerFrame())); err != nil {
		t.Fatalf("second register: %v", err)
	}
	if !first.Closed() {
		t.Errorf("first conn was not closed by take-over")
	}
	if second.Closed() {
		t.Errorf("second conn unexpectedly closed")
	}
	m, _ := mreg.Get("macbook-pro")
	if m.Conn != second {
		t.Errorf("Conn = %v, want second", m.Conn)
	}
}

// --- helpers ---

func mustHandle(t *testing.T, ing *Ingestor, conn Conn, b []byte) {
	t.Helper()
	if err := ing.Handle(context.Background(), conn, b); err != nil {
		t.Fatalf("Handle(%s): %v", peekType(b), err)
	}
}

func peekType(b []byte) string {
	var m struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(io.LimitReader(bytes.NewReader(b), 1024)).Decode(&m)
	return m.Type
}

// captureClient is a sessions.ClientConn recording every forwarded frame.
type captureClient struct {
	id string

	mu     sync.Mutex
	frames [][]byte
}

func (c *captureClient) ID() string { return c.id }

func (c *captureClient) Send(msg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	c.frames = append(c.frames, cp)
}

func (c *captureClient) collected() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.frames))
	copy(out, c.frames)
	return out
}

// attachClient attaches a capture client to a registered session.
func (h *testHarness) attachClient(t *testing.T, sessionID, connID string) *captureClient {
	t.Helper()
	s, ok := h.sessions.Get(sessionID)
	if !ok {
		t.Fatalf("session %q not registered", sessionID)
	}
	c := &captureClient{id: connID}
	s.Attach(c)
	return c
}

// session_state_change must be forwarded to attached clients in the
// coordinator-app shape: machine_id added, seq/ts dropped (SPEC.md § 10.3).
func TestIngest_SessionStateChange_ForwardedToAttached(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	c := h.attachClient(t, "sess-1", "conn-1")

	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":       "session_state_change",
		"v":          1,
		"session_id": "sess-1",
		"seq":        2,
		"ts":         1700000000,
		"from":       "running",
		"to":         "idle",
	}))

	frames := c.collected()
	if len(frames) != 1 {
		t.Fatalf("attached client got %d frames, want 1", len(frames))
	}
	// Strict decode through the generated coordinator-app type: enforces
	// required machine_id and valid enum values.
	var fwd coordinator_app.SessionStateChangeJson
	if err := json.Unmarshal(frames[0], &fwd); err != nil {
		t.Fatalf("forwarded frame is not coordinator-app session_state_change: %v\n%s", err, frames[0])
	}
	if fwd.MachineId != "macbook-pro" || fwd.SessionId != "sess-1" ||
		string(fwd.From) != "running" || string(fwd.To) != "idle" {
		t.Errorf("unexpected forwarded frame: %s", frames[0])
	}
	var loose map[string]any
	_ = json.Unmarshal(frames[0], &loose)
	if _, hasSeq := loose["seq"]; hasSeq {
		t.Errorf("coordinator-app session_state_change must not carry seq: %s", frames[0])
	}
}

// session_ended must be forwarded to attached clients with machine_id
// added (SPEC.md § 10.3).
func TestIngest_SessionEnded_ForwardedToAttached(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	c := h.attachClient(t, "sess-1", "conn-1")

	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type":       "session_ended",
		"v":          1,
		"session_id": "sess-1",
		"seq":        3,
		"reason":     "process_exit",
	}))

	frames := c.collected()
	if len(frames) != 1 {
		t.Fatalf("attached client got %d frames, want 1", len(frames))
	}
	var fwd coordinator_app.SessionEndedJson
	if err := json.Unmarshal(frames[0], &fwd); err != nil {
		t.Fatalf("forwarded frame is not coordinator-app session_ended: %v\n%s", err, frames[0])
	}
	if fwd.MachineId != "macbook-pro" || fwd.SessionId != "sess-1" ||
		string(fwd.Reason) != "process_exit" {
		t.Errorf("unexpected forwarded frame: %s", frames[0])
	}
}

// machine_suspending must broadcast paused state changes to clients
// attached to that machine's sessions (via PauseAllForMachine).
func TestIngest_MachineSuspending_ForwardedToAttached(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	c := h.attachClient(t, "sess-1", "conn-1")

	mustHandle(t, h.ing, h.conn, frame(t, map[string]any{
		"type": "machine_suspending",
		"v":    1,
	}))

	frames := c.collected()
	if len(frames) != 1 {
		t.Fatalf("attached client got %d frames, want 1", len(frames))
	}
	var fwd coordinator_app.SessionStateChangeJson
	if err := json.Unmarshal(frames[0], &fwd); err != nil {
		t.Fatalf("broadcast frame is not coordinator-app session_state_change: %v\n%s", err, frames[0])
	}
	if string(fwd.To) != "paused" || fwd.MachineId != "macbook-pro" {
		t.Errorf("unexpected broadcast frame: %s", frames[0])
	}
}

// session_event publishes atomically to ring + attached clients.
func TestIngest_SessionEvent_FannedOutToAttached(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	mustHandle(t, h.ing, h.conn, frame(t, sessionStartedFrame("sess-1")))
	c := h.attachClient(t, "sess-1", "conn-1")

	raw := frame(t, sessionEventFrame("sess-1", 1))
	mustHandle(t, h.ing, h.conn, raw)

	frames := c.collected()
	if len(frames) != 1 || !bytes.Equal(frames[0], raw) {
		t.Fatalf("attached client frames = %q, want the raw session_event passed through", frames)
	}

	// Stale seq: neither ring nor clients see it again.
	mustHandle(t, h.ing, h.conn, frame(t, sessionEventFrame("sess-1", 1)))
	if got := len(c.collected()); got != 1 {
		t.Errorf("stale seq was fanned out: %d frames, want 1", got)
	}
}
