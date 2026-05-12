// SPDX-License-Identifier: MIT
package session_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/TheTechChild/pi-remote-daemon/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// stubCoord is the in-test implementation of session.Coord. Captured
// frames are stored in order under a mutex; Connected is flipped by the
// test via SetConnected. Send returns an error when !Connected so the
// multiplex's "treat send-error as drop" path is exercised too.
type stubCoord struct {
	mu        sync.Mutex
	connected bool
	frames    []any
	sendErr   error
}

func newStubCoord() *stubCoord {
	return &stubCoord{connected: true}
}

func (s *stubCoord) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *stubCoord) SetConnected(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = v
}

func (s *stubCoord) SetSendError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendErr = err
}

func (s *stubCoord) Send(frame any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected {
		return errStubDisconnected
	}
	if s.sendErr != nil {
		return s.sendErr
	}
	s.frames = append(s.frames, frame)
	return nil
}

func (s *stubCoord) Frames() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]any, len(s.frames))
	copy(cp, s.frames)
	return cp
}

func (s *stubCoord) FrameCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

var errStubDisconnected = stubErr("stub: disconnected")

type stubErr string

func (e stubErr) Error() string { return string(e) }

// asMap encodes any frame to JSON and decodes as map[string]any so
// tests can assert on schema-shape literals without typed compares.
func asMap(t *testing.T, frame any) map[string]any {
	t.Helper()
	b, err := json.Marshal(frame)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

// startMultiplex is a test helper that constructs a Multiplex over a
// fresh registry + stub coord with a fixed clock for deterministic ts.
func startMultiplex(t *testing.T) (*session.Registry, *stubCoord, *session.Multiplex) {
	t.Helper()
	reg := session.NewRegistry()
	coord := newStubCoord()
	clock := func() time.Time { return time.UnixMilli(1730000000000) }
	mux := session.NewMultiplex(reg, coord, "macbook-pro", clock)
	require.NotNil(t, mux)
	return reg, coord, mux
}

// M1: TestMultiplex_RegisterFiresSessionStarted — one session_started
// frame on the coord per accepted Register.
func TestMultiplex_RegisterFiresSessionStarted(t *testing.T) {
	reg, coord, _ := startMultiplex(t)

	require.True(t, mustRegister(t, reg, newSession("sess-1", 1234)))

	require.Equal(t, 1, coord.FrameCount(), "expected exactly one frame")
	frames := coord.Frames()
	require.NotEmpty(t, frames)
	got := asMap(t, frames[0])
	require.Equal(t, "session_started", got["type"])
	require.Equal(t, "sess-1", got["session_id"])
	require.Equal(t, "macbook-pro", got["machine_id"])
}

// M2: TestMultiplex_SessionStarted_MetadataShape — required metadata
// fields lifted from the session. spawn_token omitted when empty.
func TestMultiplex_SessionStarted_MetadataShape(t *testing.T) {
	reg, coord, _ := startMultiplex(t)

	s := newSession("sess-1", 1234)
	s.CWD = "/home/user/proj"
	s.ProjectName = "proj"
	s.Hostname = "host.local"
	s.Model = "anthropic/claude-sonnet-4-20250514"
	s.StartedAt = time.UnixMilli(1730000000000)
	require.True(t, mustRegister(t, reg, s))

	require.Equal(t, 1, coord.FrameCount())
	frames := coord.Frames()
	require.NotEmpty(t, frames)
	got := asMap(t, frames[0])
	md, ok := got["metadata"].(map[string]any)
	require.True(t, ok, "metadata is a JSON object")
	require.Equal(t, "/home/user/proj", md["cwd"])
	require.Equal(t, "proj", md["project_name"])
	require.Equal(t, "host.local", md["hostname"])
	require.Equal(t, "anthropic/claude-sonnet-4-20250514", md["model"])
	require.EqualValues(t, 1730000000000, md["started_at"])
	_, hasSpawn := md["spawn_token"]
	require.False(t, hasSpawn, "empty spawn_token must be omitted")
}

// M3: TestMultiplex_HeartbeatEmitsNothing — heartbeats are ext-side
// only per the plan.
func TestMultiplex_HeartbeatEmitsNothing(t *testing.T) {
	reg, coord, _ := startMultiplex(t)

	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount() // session_started

	require.NoError(t, reg.UpdateHeartbeat("sess-1", time.Now()))

	require.Equal(t, startCount, coord.FrameCount(), "heartbeat must not produce upstream frames")
}

// M4: TestMultiplex_EventEmitsSessionEvent — Event from ext produces
// session_event with seq=1, kind+data preserved.
func TestMultiplex_EventEmitsSessionEvent(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))

	payload := []byte(`{"type":"event","kind":"agent_start","data":{}}`)
	reg.Event("sess-1", payload)

	require.Equal(t, 2, coord.FrameCount(), "session_started + session_event")
	frames := coord.Frames()
	require.GreaterOrEqual(t, len(frames), 2)
	got := asMap(t, frames[1])
	require.Equal(t, "session_event", got["type"])
	require.Equal(t, "sess-1", got["session_id"])
	require.EqualValues(t, 1, got["seq"])
	require.Equal(t, "agent_start", got["kind"])
	require.Equal(t, map[string]any{}, got["data"])
}

// M5: TestMultiplex_TwoEvents_SeqAdvances — seq 1 then 2.
func TestMultiplex_TwoEvents_SeqAdvances(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))

	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_end","data":{}}`))

	frames := coord.Frames()
	require.Len(t, frames, 3) // started + 2 events
	require.GreaterOrEqual(t, len(frames), 3)
	require.EqualValues(t, 1, asMap(t, frames[1])["seq"])
	require.EqualValues(t, 2, asMap(t, frames[2])["seq"])
}

// M6: TestMultiplex_PerSessionSeq_StartsAtOneEach — independent
// counters per session.
func TestMultiplex_PerSessionSeq_StartsAtOneEach(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-A", 1)))
	require.True(t, mustRegister(t, reg, newSession("sess-B", 2)))

	reg.Event("sess-A", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	reg.Event("sess-B", []byte(`{"type":"event","kind":"agent_start","data":{}}`))

	frames := coord.Frames()
	require.Len(t, frames, 4) // 2 started + 2 events
	// Filter by session_id and check seq.
	for _, f := range frames {
		m := asMap(t, f)
		if m["type"] != "session_event" {
			continue
		}
		require.EqualValues(t, 1, m["seq"], "each session's first event must be seq=1")
	}
}

// M7: TestMultiplex_DisconnectFiresSessionEnded_ExtensionDisconnect —
// RemoveWithReason maps to upstream reason "extension_disconnect" per
// the acked correction.
func TestMultiplex_DisconnectFiresSessionEnded_ExtensionDisconnect(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))

	reg.RemoveWithReason("sess-1", "session_shutdown")

	require.Equal(t, 2, coord.FrameCount(), "session_started + session_ended")
	frames := coord.Frames()
	require.GreaterOrEqual(t, len(frames), 2)
	got := asMap(t, frames[1])
	require.Equal(t, "session_ended", got["type"])
	require.Equal(t, "sess-1", got["session_id"])
	require.Equal(t, "extension_disconnect", got["reason"])
}

// M8: TestMultiplex_MarkEndedFiresSessionEnded_ProcessExit —
// MarkEnded (socket close without disconnect) maps to "process_exit".
func TestMultiplex_MarkEndedFiresSessionEnded_ProcessExit(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))

	reg.MarkEnded("sess-1")

	require.Equal(t, 2, coord.FrameCount())
	frames := coord.Frames()
	require.GreaterOrEqual(t, len(frames), 2)
	got := asMap(t, frames[1])
	require.Equal(t, "session_ended", got["type"])
	require.Equal(t, "process_exit", got["reason"])
}

// M9: TestMultiplex_SessionEnded_SeqAllocated — the session_ended frame
// gets a seq from the same per-session allocator. Schema requires
// seq >= 1.
func TestMultiplex_SessionEnded_SeqAllocated(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))

	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_end","data":{}}`))
	reg.Event("sess-1", []byte(`{"type":"event","kind":"queue_update","data":{"pending":1}}`))

	reg.RemoveWithReason("sess-1", "session_shutdown")

	frames := coord.Frames()
	require.Len(t, frames, 5) // started + 3 events + ended
	require.GreaterOrEqual(t, len(frames), 5)
	endedFrame := asMap(t, frames[4])
	require.Equal(t, "session_ended", endedFrame["type"])
	require.EqualValues(t, 4, endedFrame["seq"], "session_ended seq advances past the events")
}

// M10: TestMultiplex_DropOnDisconnect_AllocatesSeq — seq advances even
// when coord is disconnected; no frame on the wire.
func TestMultiplex_DropOnDisconnect_AllocatesSeq(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount()

	coord.SetConnected(false)
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))

	require.Equal(t, startCount, coord.FrameCount(), "no new frame while disconnected")

	// Reconnect and fire one more event - its seq must be 2, proving
	// the dropped event consumed seq=1.
	coord.SetConnected(true)
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_end","data":{}}`))

	frames := coord.Frames()
	require.Greater(t, len(frames), startCount, "event after reconnect produces a frame")
	got := asMap(t, frames[startCount])
	require.EqualValues(t, 2, got["seq"], "dropped frame consumed seq=1; this frame must be seq=2")
}

// M11: TestMultiplex_DropOnDisconnect_NoBuffering — reconnect does NOT
// flush dropped frames. Buffer-nothing policy per the plan.
func TestMultiplex_DropOnDisconnect_NoBuffering(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount()

	coord.SetConnected(false)
	for i := 0; i < 10; i++ {
		reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	}
	require.Equal(t, startCount, coord.FrameCount(), "no frames while disconnected")

	coord.SetConnected(true)
	// Idle - no further events. Multiplex must NOT now flush the 10.
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, startCount, coord.FrameCount(), "reconnect must not flush dropped events")
}

// M12: TestMultiplex_LastSeqPreservedAcrossDisconnect — connected 3,
// disconnected 5 (lost), reconnected 1 = seq 9.
func TestMultiplex_LastSeqPreservedAcrossDisconnect(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount()

	for i := 0; i < 3; i++ {
		reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	}

	coord.SetConnected(false)
	for i := 0; i < 5; i++ {
		reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	}

	coord.SetConnected(true)
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))

	// Frames sent: 3 + 1 (the 5 in the middle were dropped).
	require.Equal(t, startCount+4, coord.FrameCount())

	// Last frame's seq is 9 (1..8 used + 9 = current).
	frames := coord.Frames()
	require.GreaterOrEqual(t, len(frames), startCount+4)
	last := asMap(t, frames[startCount+3])
	require.EqualValues(t, 9, last["seq"], "LastSeq preserved across the dropped window")
}

// M13: TestMultiplex_ConcurrentEvents_MonotonicPerSession_Race — 4
// sessions x 50 goroutines x 100 events under -race.
func TestMultiplex_ConcurrentEvents_MonotonicPerSession_Race(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	const sessions = 4
	const goroutines = 50
	const perGoroutine = 100

	sessIDs := make([]string, sessions)
	for i := 0; i < sessions; i++ {
		id := "sess-c-" + string(rune('a'+i))
		sessIDs[i] = id
		require.True(t, mustRegister(t, reg, newSession(id, 1+i)))
	}

	payload := []byte(`{"type":"event","kind":"agent_start","data":{}}`)

	var wg sync.WaitGroup
	for s := 0; s < sessions; s++ {
		for g := 0; g < goroutines; g++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				for i := 0; i < perGoroutine; i++ {
					reg.Event(id, payload)
				}
			}(sessIDs[s])
		}
	}
	wg.Wait()

	// Per session: extract seq values from session_event frames, assert
	// they are exactly {1..goroutines*perGoroutine}.
	const wantPerSession = goroutines * perGoroutine
	seqsPerSession := make(map[string]map[uint64]bool, sessions)
	for _, id := range sessIDs {
		seqsPerSession[id] = make(map[uint64]bool, wantPerSession)
	}
	for _, f := range coord.Frames() {
		m := asMap(t, f)
		if m["type"] != "session_event" {
			continue
		}
		id := m["session_id"].(string)
		seq := uint64(m["seq"].(float64))
		require.False(t, seqsPerSession[id][seq], "duplicate seq %d on %s", seq, id)
		seqsPerSession[id][seq] = true
	}
	for _, id := range sessIDs {
		require.Len(t, seqsPerSession[id], wantPerSession, "session %s missing seqs", id)
		for i := uint64(1); i <= wantPerSession; i++ {
			require.True(t, seqsPerSession[id][i], "session %s missing seq %d", id, i)
		}
	}
}

// M14: TestMultiplex_SendError_DoesNotRollbackSeq — coord.Send returning
// an error advances seq same as !Connected.
func TestMultiplex_SendError_DoesNotRollbackSeq(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount()

	coord.SetSendError(stubErr("transient write failure"))
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	require.Equal(t, startCount, coord.FrameCount(), "errored frame not stored")

	coord.SetSendError(nil)
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_end","data":{}}`))

	frames := coord.Frames()
	require.Greater(t, len(frames), startCount)
	got := asMap(t, frames[startCount])
	require.EqualValues(t, 2, got["seq"], "errored frame consumed seq=1")
}

// M15: TestMultiplex_LiveSessions_ReturnsSnapshotForResume — snapshot
// drives session_resume emission on coordinator reconnect.
func TestMultiplex_LiveSessions_ReturnsSnapshotForResume(t *testing.T) {
	reg, coord, mux := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-A", 1)))
	require.True(t, mustRegister(t, reg, newSession("sess-B", 2)))

	reg.Event("sess-A", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	reg.Event("sess-A", []byte(`{"type":"event","kind":"agent_end","data":{}}`))
	reg.Event("sess-B", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	_ = coord // exists to keep the multiplex's coord-side state consistent

	live := mux.LiveSessions()
	require.Len(t, live, 2)
	bySID := map[string]uint64{}
	for _, ls := range live {
		bySID[ls.Session.SessionID] = ls.LastSeq
	}
	require.EqualValues(t, 3, bySID["sess-A"], "A: started(1) + 2 events = LastSeq 3")
	require.EqualValues(t, 2, bySID["sess-B"], "B: started(1) + 1 event = LastSeq 2")
}

// M16: TestMultiplex_EndedSession_DropsFromLive — Resume must not
// re-announce dead sessions.
func TestMultiplex_EndedSession_DropsFromLive(t *testing.T) {
	reg, _, mux := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-A", 1)))
	require.True(t, mustRegister(t, reg, newSession("sess-B", 2)))

	reg.RemoveWithReason("sess-A", "session_shutdown")

	live := mux.LiveSessions()
	require.Len(t, live, 1)
	require.Equal(t, "sess-B", live[0].Session.SessionID)
}

// M17: TestMultiplex_MalformedEventBytes_DoesNotPanic_DropsFrame —
// bad bytes from the ext side cannot wedge the multiplex.
func TestMultiplex_MalformedEventBytes_DoesNotPanic_DropsFrame(t *testing.T) {
	reg, coord, _ := startMultiplex(t)
	require.True(t, mustRegister(t, reg, newSession("sess-1", 1)))
	startCount := coord.FrameCount()

	require.NotPanics(t, func() {
		reg.Event("sess-1", []byte(`{not valid json`))
		reg.Event("sess-1", []byte(``))
		reg.Event("sess-1", []byte(`{"type":"event","kind":"NOT_IN_ENUM","data":{}}`))
	})

	require.Equal(t, startCount, coord.FrameCount(), "malformed events do not produce frames")

	// Allocator should NOT have advanced (we never built a valid frame).
	// Next valid event should be seq=1.
	reg.Event("sess-1", []byte(`{"type":"event","kind":"agent_start","data":{}}`))
	frames := coord.Frames()
	require.Greater(t, len(frames), startCount)
	got := asMap(t, frames[startCount])
	require.EqualValues(t, 1, got["seq"], "malformed events did not consume a seq slot")
}

// Suppress unused-import linter complaints for the proto package; it's
// referenced indirectly through the frame helpers but not by symbol
// here. (Remove this if a direct reference appears.)
var _ = pb.SessionEventJson{}
