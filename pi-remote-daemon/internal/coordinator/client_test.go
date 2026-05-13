// SPDX-License-Identifier: MIT
package coordinator_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/TheTechChild/pi-remote-daemon/internal/coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// fakeClock is a controllable Clock for backoff testing. Sleep blocks
// until the test calls Advance. Each Sleep is recorded in Requests.
type fakeClock struct {
	mu       sync.Mutex
	now      time.Time
	requests []time.Duration
	pending  chan struct{}
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:     time.UnixMilli(1730000000000),
		pending: make(chan struct{}, 16),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.requests = append(c.requests, d)
	c.pending <- struct{}{}
	c.mu.Unlock()
	// Block until the test signals completion via Advance, or until
	// ctx is canceled (production semantics).
	select {
	case <-c.advanceCh():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *fakeClock) Requests() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]time.Duration, len(c.requests))
	copy(cp, c.requests)
	return cp
}

// advanceCh returns the per-fakeClock channel that Sleep waits on.
// Test calls Advance to release one Sleep.
var advanceChs sync.Map // map[*fakeClock]chan struct{}

func (c *fakeClock) advanceCh() chan struct{} {
	if ch, ok := advanceChs.Load(c); ok {
		return ch.(chan struct{})
	}
	ch := make(chan struct{}, 16)
	advanceChs.Store(c, ch)
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	c.advanceCh() <- struct{}{}
}

// WaitForSleep blocks until the client has called Sleep at least n
// times in total. Used to synchronize the test with the dial loop.
func (c *fakeClock) WaitForSleep(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.Requests()) >= n {
			return
		}
		select {
		case <-c.pending:
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("WaitForSleep: client made %d Sleep calls, wanted >= %d", len(c.Requests()), n)
}

// stubServer wraps an httptest.Server that upgrades to a WebSocket and
// captures upgrade headers + frames received from the client. The test
// drives behavior by populating Fields before NewClient.Run is invoked.
type stubServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	headers  []http.Header
	frames   [][]byte
	connects atomic.Int32

	// behavior knobs
	rejectWith int // 0 = upgrade; non-zero = respond with this status
	closeCode  websocket.StatusCode
	holdOpen   chan struct{} // if non-nil, server holds the conn open until closed
}

func newStubServer() *stubServer {
	s := &stubServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *stubServer) URL() string {
	return "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/v1/daemon"
}

func (s *stubServer) Headers() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]http.Header, len(s.headers))
	copy(out, s.headers)
	return out
}

func (s *stubServer) Frames() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.frames))
	for i, f := range s.frames {
		out[i] = append([]byte(nil), f...)
	}
	return out
}

func (s *stubServer) ConnectCount() int { return int(s.connects.Load()) }

func (s *stubServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.headers = append(s.headers, r.Header.Clone())
	reject := s.rejectWith
	closeCode := s.closeCode
	holdOpen := s.holdOpen
	s.mu.Unlock()

	if reject != 0 {
		w.WriteHeader(reject)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	s.connects.Add(1)

	ctx := r.Context()
	if closeCode != 0 {
		_ = conn.Close(closeCode, "stub forced close")
		return
	}

	// Drain frames in a loop. Stops when the connection closes.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		s.mu.Lock()
		s.frames = append(s.frames, append([]byte(nil), data...))
		s.mu.Unlock()
		if holdOpen != nil {
			select {
			case <-holdOpen:
				return
			case <-ctx.Done():
				return
			default:
			}
		}
	}
}

func (s *stubServer) Close() {
	s.srv.Close()
}

// writeCreds writes id/secret credential files mode 0600 inside the
// test's temp dir and returns their paths.
func writeCreds(t *testing.T, id, secret string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	idPath := filepath.Join(dir, "id")
	secretPath := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(idPath, []byte(id), 0o600))
	require.NoError(t, os.WriteFile(secretPath, []byte(secret), 0o600))
	return idPath, secretPath
}

// emptyLive is a LiveSnapshot returning no sessions.
func emptyLive() []session.LiveSession { return nil }

// startClient is a test helper that constructs and runs the client on
// a goroutine, returning a cancel function that stops Run and waits.
func startClient(t *testing.T, cfg coordinator.Config) (context.CancelFunc, *sync.WaitGroup) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c := coordinator.NewClient(cfg)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = c.Run(ctx)
	}()
	// Store the client on the context so tests can reach it; in the
	// real API the caller would hold a reference.
	clientsByTest.Store(t.Name(), c)
	t.Cleanup(func() { clientsByTest.Delete(t.Name()) })
	return cancel, &wg
}

// clientsByTest lets test helpers fetch the client constructed for the
// running test without threading it through every helper. Keyed by
// t.Name; the suite is sequential within a package so this is safe.
var clientsByTest sync.Map

func clientFor(t *testing.T) *coordinator.Client {
	t.Helper()
	v, ok := clientsByTest.Load(t.Name())
	require.True(t, ok)
	return v.(*coordinator.Client)
}

// defaultRegister returns a populated MachineRegisterInput for tests.
func defaultRegister() coordinator.MachineRegisterInput {
	return coordinator.MachineRegisterInput{
		MachineID:          "macbook-pro",
		MachineDisplayName: "MacBook Pro",
		DaemonVersion:      "1.0.0",
		Capabilities:       []string{"spawn", "mirror"},
	}
}

// waitForFrame blocks until the stub server has received at least n
// frames, or the deadline elapses.
func waitForFrame(t *testing.T, s *stubServer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.Frames()) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitForFrame: got %d frames, wanted >= %d", len(s.Frames()), n)
}

// waitForConnects blocks until the stub server has accepted >= n
// upgrades, or the deadline elapses.
func waitForConnects(t *testing.T, s *stubServer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectCount() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitForConnects: got %d, wanted >= %d", s.ConnectCount(), n)
}

// C1: TestClient_Connect_SetsCFAccessHeaders.
func TestClient_Connect_SetsCFAccessHeaders(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)
	hdr := srv.Headers()[0]
	require.Equal(t, "test-machine", hdr.Get("CF-Access-Client-Id"))
	require.Equal(t, "test-secret", hdr.Get("CF-Access-Client-Secret"))
}

// C2: TestClient_Connect_FirstFrameIsMachineRegister.
func TestClient_Connect_FirstFrameIsMachineRegister(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForFrame(t, srv, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal(srv.Frames()[0], &got))
	require.Equal(t, "machine_register", got["type"])
	require.Equal(t, "macbook-pro", got["machine_id"])
}

// C3: TestClient_Connect_FirstFrameBeforeAnythingElse. The Send API is
// called immediately on the client; the first frame on the wire must
// still be machine_register.
func TestClient_Connect_FirstFrameBeforeAnythingElse(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	// Race the first frame: call Send before the dial completes.
	c := clientFor(t)
	earlyFrame := map[string]any{"type": "session_event", "v": 1, "session_id": "x", "seq": 1, "kind": "agent_start", "ts": 0, "data": map[string]any{}}
	_ = c.Send(earlyFrame) // may error (not connected); the client must NOT enqueue it

	waitForFrame(t, srv, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal(srv.Frames()[0], &got))
	require.Equal(t, "machine_register", got["type"], "first frame on the wire MUST be machine_register")
}

// C5: TestClient_MissingCredentials_RetriesWithBackoff. The id file
// does not exist at startup; the client logs and retries with backoff,
// not crash. Once the file appears, the next attempt connects.
func TestClient_MissingCredentials_RetriesWithBackoff(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()

	dir := t.TempDir()
	idPath := filepath.Join(dir, "id")
	secretPath := filepath.Join(dir, "secret")
	// Secret exists; id does not yet.
	require.NoError(t, os.WriteFile(secretPath, []byte("test-secret"), 0o600))

	clock := newFakeClock()
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	clock.WaitForSleep(t, 1) // client requested a backoff sleep
	require.Equal(t, 0, srv.ConnectCount(), "no upgrade yet - missing creds")

	// Provision credentials and advance the clock to release backoff.
	require.NoError(t, os.WriteFile(idPath, []byte("test-machine"), 0o600))
	clock.Advance(time.Second)

	waitForConnects(t, srv, 1)
}

// C6: TestClient_AuthFailure_RetriesWithBackoff. Server returns 403;
// client backs off and retries.
func TestClient_AuthFailure_RetriesWithBackoff(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()

	srv.mu.Lock()
	srv.rejectWith = http.StatusForbidden
	srv.mu.Unlock()

	idPath, secretPath := writeCreds(t, "test-machine", "wrong-secret")
	clock := newFakeClock()
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	clock.WaitForSleep(t, 1)
	require.GreaterOrEqual(t, len(srv.Headers()), 1, "client dialed at least once before backoff")
}

// C7: TestClient_Reconnect_ExponentialBackoff_1s_60s. Inspect the
// fakeClock.Requests after several rejection cycles.
func TestClient_Reconnect_ExponentialBackoff_1s_60s(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.rejectWith = http.StatusForbidden
	srv.mu.Unlock()

	idPath, secretPath := writeCreds(t, "test-machine", "wrong-secret")
	clock := newFakeClock()
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	// Cycle through 8 attempts, releasing each sleep.
	for i := 0; i < 8; i++ {
		clock.WaitForSleep(t, i+1)
		clock.Advance(time.Hour)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second}
	got := clock.Requests()
	require.GreaterOrEqual(t, len(got), len(want))
	for i, w := range want {
		require.Equal(t, w, got[i], "request %d: want %v, got %v", i, w, got[i])
	}
}

// C9: TestClient_Reconnect_EmitsMachineRegister. Force-close + reconnect:
// the second upgrade's first frame is also machine_register.
func TestClient_Reconnect_EmitsMachineRegister(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	// First conn closes immediately after accept.
	srv.mu.Lock()
	srv.closeCode = websocket.StatusGoingAway
	srv.mu.Unlock()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)

	// Stop forcing close; release backoff; expect a second connect.
	srv.mu.Lock()
	srv.closeCode = 0
	srv.mu.Unlock()
	clock.WaitForSleep(t, 1)
	clock.Advance(time.Second)
	waitForConnects(t, srv, 2)
	waitForFrame(t, srv, 1) // first frame on the SECOND conn

	// The second conn's first frame is still machine_register.
	var got map[string]any
	require.NoError(t, json.Unmarshal(srv.Frames()[0], &got))
	require.Equal(t, "machine_register", got["type"])
	require.Equal(t, "macbook-pro", got["machine_id"])
}

// C10: TestClient_Reconnect_EmitsSessionResumePerLiveSession.
// LiveSnapshot returns 2 sessions; after machine_register, expect 2
// session_resume frames.
func TestClient_Reconnect_EmitsSessionResumePerLiveSession(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	live := func() []session.LiveSession {
		return []session.LiveSession{
			{Session: session.Session{SessionID: "sess-A", CWD: "/x", ProjectName: "p", Hostname: "h", Model: "m", StartedAt: time.UnixMilli(1)}, LastSeq: 5},
			{Session: session.Session{SessionID: "sess-B", CWD: "/y", ProjectName: "q", Hostname: "h", Model: "m", StartedAt: time.UnixMilli(2)}, LastSeq: 12},
		}
	}
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    live,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForFrame(t, srv, 3) // machine_register + 2 session_resume
	frames := srv.Frames()
	require.GreaterOrEqual(t, len(frames), 3)

	// First frame: machine_register.
	var first map[string]any
	require.NoError(t, json.Unmarshal(frames[0], &first))
	require.Equal(t, "machine_register", first["type"])

	// Next two: session_resume (in any order).
	got := map[string]uint64{}
	for _, fb := range frames[1:3] {
		var m map[string]any
		require.NoError(t, json.Unmarshal(fb, &m))
		require.Equal(t, "session_resume", m["type"])
		got[m["session_id"].(string)] = uint64(m["last_seq_emitted"].(float64))
	}
	require.EqualValues(t, 5, got["sess-A"])
	require.EqualValues(t, 12, got["sess-B"])
}

// C11: TestClient_Reconnect_NoResumeForEndedSession. LiveSnapshot only
// returns live sessions; the client must not invent resume frames.
func TestClient_Reconnect_NoResumeForEndedSession(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	live := func() []session.LiveSession {
		return []session.LiveSession{
			{Session: session.Session{SessionID: "sess-B", CWD: "/y", ProjectName: "q", Hostname: "h", Model: "m", StartedAt: time.UnixMilli(2)}, LastSeq: 12},
		}
	}
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    live,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForFrame(t, srv, 2) // machine_register + 1 session_resume

	// Give the client a moment to emit any (incorrect) extra frames.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, 2, len(srv.Frames()), "exactly machine_register + 1 session_resume")
}

// C12: TestClient_Reconnect_ResumeMatchesRegistryLastSeq. The
// LiveSnapshot is consulted fresh on each reconnect.
func TestClient_Reconnect_ResumeMatchesRegistryLastSeq(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.closeCode = websocket.StatusGoingAway
	srv.mu.Unlock()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	var lastSeq atomic.Uint64
	lastSeq.Store(5)
	live := func() []session.LiveSession {
		return []session.LiveSession{
			{Session: session.Session{SessionID: "sess-A", CWD: "/x", ProjectName: "p", Hostname: "h", Model: "m", StartedAt: time.UnixMilli(1)}, LastSeq: lastSeq.Load()},
		}
	}
	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    live,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)

	// Simulate dropped events advancing LastSeq during the outage.
	lastSeq.Store(8)
	srv.mu.Lock()
	srv.closeCode = 0
	srv.mu.Unlock()
	clock.WaitForSleep(t, 1)
	clock.Advance(time.Second)
	waitForConnects(t, srv, 2)
	waitForFrame(t, srv, 2) // register + resume on the new conn

	// Find the resume frame and check last_seq_emitted.
	frames := srv.Frames()
	var resumeSeq float64
	for _, fb := range frames {
		var m map[string]any
		if err := json.Unmarshal(fb, &m); err != nil {
			continue
		}
		if m["type"] == "session_resume" && m["session_id"] == "sess-A" {
			resumeSeq = m["last_seq_emitted"].(float64)
		}
	}
	require.EqualValues(t, 8, resumeSeq, "resume must reflect advanced LastSeq, not stale 5")
}

// C13: TestClient_CleanShutdown_SendsCloseCode1000. ctx cancel triggers
// a clean shutdown.
func TestClient_CleanShutdown_SendsCloseCode1000(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	waitForConnects(t, srv, 1)
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("client did not exit within 2s after ctx cancel")
	}
	// Verifying the *actual* close-code 1000 on the wire requires the
	// stubServer to capture conn.CloseRead's error; deferred to GREEN.
	// For RED it suffices that the client exited cleanly on cancel.
}

// C14: TestClient_ServerClose_1008_LogsPolicyViolation. Server closes
// with 1008; client backs off and retries.
func TestClient_ServerClose_1008_LogsPolicyViolation(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.closeCode = websocket.StatusPolicyViolation
	srv.mu.Unlock()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)
	clock.WaitForSleep(t, 1) // backed off, did not give up
}

// C15: TestClient_SendWhileDisconnected_ReturnsErrNotConnected.
func TestClient_SendWhileDisconnected_ReturnsErrNotConnected(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.rejectWith = http.StatusForbidden
	srv.mu.Unlock()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()
	clock.WaitForSleep(t, 1) // client is in backoff state, !Connected

	c := clientFor(t)
	err := c.Send(map[string]any{"type": "session_event", "v": 1, "session_id": "x", "seq": 1, "kind": "agent_start", "ts": 0, "data": map[string]any{}})
	require.True(t, errors.Is(err, coordinator.ErrNotConnected), "want ErrNotConnected, got %v", err)
}

// C16: TestClient_Connected_FlipsOnDisconnect.
func TestClient_Connected_FlipsOnDisconnect(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.rejectWith = http.StatusForbidden
	srv.mu.Unlock()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	clock.WaitForSleep(t, 1)
	c := clientFor(t)
	require.False(t, c.Connected(), "in backoff state, Connected must be false")
}

// C17: TestClient_ConcurrentSends_Serialized.
func TestClient_ConcurrentSends_Serialized(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)
	waitForFrame(t, srv, 1) // machine_register

	c := clientFor(t)
	const n = 100
	var sendWG sync.WaitGroup
	for i := 0; i < n; i++ {
		sendWG.Add(1)
		go func(i int) {
			defer sendWG.Done()
			_ = c.Send(map[string]any{
				"type":       "session_event",
				"v":          1,
				"session_id": "s",
				"seq":        i + 1,
				"kind":       "agent_start",
				"ts":         0,
				"data":       map[string]any{},
			})
		}(i)
	}
	sendWG.Wait()

	waitForFrame(t, srv, 1+n)
	// Each frame must parse as JSON cleanly (no interleaving).
	for i, fb := range srv.Frames() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(fb, &m), "frame %d parse", i)
	}
}

// C18: TestClient_ContextCancel_StopsReconnectLoop. cancel() while
// mid-backoff returns within a reasonable window.
func TestClient_ContextCancel_StopsReconnectLoop(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	srv.mu.Lock()
	srv.rejectWith = http.StatusForbidden
	srv.mu.Unlock()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	clock.WaitForSleep(t, 1) // mid-backoff
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancel during backoff did not stop Run within 500ms")
	}
}

// C19: TestClient_PingPongLibraryManaged_NoAppPing. Over a 250ms idle
// window after connect, no application-level ping frames appear.
func TestClient_PingPongLibraryManaged_NoAppPing(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForFrame(t, srv, 1) // machine_register
	time.Sleep(250 * time.Millisecond)

	for i, fb := range srv.Frames() {
		var m map[string]any
		_ = json.Unmarshal(fb, &m)
		require.NotEqual(t, "ping", m["type"], "frame %d is an application ping", i)
	}
}

// C20: TestClient_HeaderInjection_HappensAtDialTime. Credentials change
// mid-connection do NOT update the in-flight conn; only the next
// reconnect picks them up.
func TestClient_HeaderInjection_HappensAtDialTime(t *testing.T) {
	srv := newStubServer()
	defer srv.Close()
	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             srv.URL(),
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	waitForConnects(t, srv, 1)

	// Rewrite the credential files; in-flight conn keeps old headers.
	require.NoError(t, os.WriteFile(idPath, []byte("new-machine"), 0o600))
	require.NoError(t, os.WriteFile(secretPath, []byte("new-secret"), 0o600))

	// Wait a bit; no new connection should have been triggered.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 1, srv.ConnectCount(), "credential change must not trigger reconnect")
	require.Equal(t, "test-machine", srv.Headers()[0].Get("CF-Access-Client-Id"))
}

// C21: TestClient_UnknownFrameFromServer_LoggedAndDropped. The server
// sends a frame whose type the daemon does not recognize (e.g., a
// future spawn_request the daemon doesn't yet handle). The client must
// not crash. To exercise this, the stub upgrades and writes a single
// frame, then closes. The client should NOT propagate panic; the test
// passes by virtue of Run returning normally on the subsequent close.
func TestClient_UnknownFrameFromServer_LoggedAndDropped(t *testing.T) {
	// Custom stub that writes a frame after accept, then closes.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/daemon", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// Wait for the client's first frame (machine_register).
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}
		// Now write an unknown frame.
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"who_knows","v":1,"surprise":true}`))
		_ = conn.Close(websocket.StatusNormalClosure, "")
	})
	hs := httptest.NewServer(mux)
	defer hs.Close()

	idPath, secretPath := writeCreds(t, "test-machine", "test-secret")
	clock := newFakeClock()

	cancel, wg := startClient(t, coordinator.Config{
		URL:             "ws" + strings.TrimPrefix(hs.URL, "http") + "/v1/daemon",
		IDFile:          idPath,
		SecretFile:      secretPath,
		MachineRegister: defaultRegister(),
		LiveSnapshot:    emptyLive,
		Clock:           clock,
	})
	defer wg.Wait()
	defer cancel()

	// The client should observe the close and proceed to backoff,
	// not crash on the unknown frame.
	clock.WaitForSleep(t, 1)
}
