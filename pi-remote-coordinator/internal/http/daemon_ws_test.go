// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// testServer is a coordinator stood up against an ephemeral port, exposing
// its registries for assertions.
type testServer struct {
	srv      *httptest.Server
	machines *machines.Registry
	sessions *sessions.Registry
	clients  *clients.Registry
	logBuf   *syncBuffer
}

// syncBuffer is a goroutine-safe bytes.Buffer wrapper so slog log capture
// works under -race when handlers log from a read-loop goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *syncBuffer) Contains(sub string) bool {
	return strings.Contains(s.String(), sub)
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	mreg := machines.NewRegistry()
	sreg := sessions.NewRegistry()
	creg := clients.NewRegistry(clients.WithStubFixture())
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mux := NewMux(Deps{
		Auth:     auth.NewStub(),
		Machines: mreg,
		Sessions: sreg,
		Clients:  creg,
		Logger:   logger,
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testServer{srv: srv, machines: mreg, sessions: sreg, clients: creg, logBuf: logBuf}
}

func (ts *testServer) wsURL(path string) string {
	return "ws" + strings.TrimPrefix(ts.srv.URL, "http") + path
}

func dialDaemon(t *testing.T, ts *testServer, clientID, secret string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if clientID != "" {
		header.Set("CF-Access-Client-Id", clientID)
	}
	if secret != "" {
		header.Set("CF-Access-Client-Secret", secret)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, ts.wsURL("/v1/daemon"), &websocket.DialOptions{
		HTTPHeader: header,
	})
}

// C-45: No auth headers → 403, body contains ERR_COORD_AUTH_REQUIRED.
func TestDaemonWS_NoAuth_403(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.srv.URL + "/v1/daemon")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ERR_COORD_AUTH_REQUIRED") {
		t.Errorf("body = %q, want substring ERR_COORD_AUTH_REQUIRED", string(body))
	}
}

// C-46: Valid stub auth → 101 upgrade succeeds.
func TestDaemonWS_ValidAuth_101(t *testing.T) {
	ts := newTestServer(t)
	conn, resp, err := dialDaemon(t, ts, "test-machine", "some-secret")
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
}

// C-47: First frame not machine_register → close 1008.
func TestDaemonWS_FirstFrameNotRegister_Close1008(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

	// Send a non-register frame as the very first frame.
	msg, _ := json.Marshal(map[string]any{
		"type":       "session_event",
		"v":          1,
		"session_id": "x",
		"seq":        1,
		"ts":         1,
		"kind":       "agent_start",
		"data":       map[string]any{},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Server should close 1008.
	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("read returned nil error; expected close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Errorf("close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
}

// C-48: First frame machine_register → entry appears in machines registry,
// conn stays open.
func TestDaemonWS_FirstFrameRegister_Accepted(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

	msg, _ := json.Marshal(map[string]any{
		"type":                 "machine_register",
		"v":                    1,
		"machine_id":           "macbook-pro",
		"machine_display_name": "MacBook Pro",
		"daemon_version":       "0.0.1",
		"capabilities":         []string{"spawn", "mirror"},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Poll up to 2s for the registry to populate. The handler reads in a
	// goroutine and dispatches synchronously into the registry, but the
	// scheduler may not have run yet by the time Write returns.
	if !waitFor(2*time.Second, func() bool {
		_, ok := ts.machines.Get("macbook-pro")
		return ok
	}) {
		t.Fatalf("machine not registered within timeout")
	}

	// Conn should still be open — try a ping by writing another frame.
	pingMsg, _ := json.Marshal(map[string]any{
		"type":       "session_started",
		"v":          1,
		"session_id": "sess-1",
		"machine_id": "macbook-pro",
		"metadata": map[string]any{
			"hostname": "macbook-pro", "cwd": "/", "model": "m", "project_name": "p",
			"started_at": 1,
		},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, pingMsg); err != nil {
		t.Errorf("conn should still be open: write failed: %v", err)
	}
}

// C-49: Two parallel dials same machine_id → first closed when second registers.
func TestDaemonWS_TakeOver(t *testing.T) {
	ts := newTestServer(t)
	first, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	regFrame, _ := json.Marshal(map[string]any{
		"type":                 "machine_register",
		"v":                    1,
		"machine_id":           "macbook-pro",
		"machine_display_name": "MacBook Pro",
		"daemon_version":       "0.0.1",
		"capabilities":         []string{"spawn"},
	})
	if err := first.Write(context.Background(), websocket.MessageText, regFrame); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !waitFor(2*time.Second, func() bool {
		_, ok := ts.machines.Get("macbook-pro")
		return ok
	}) {
		t.Fatal("first register did not land")
	}

	second, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer func() { _ = second.Close(websocket.StatusNormalClosure, "test done") }()
	if err := second.Write(context.Background(), websocket.MessageText, regFrame); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// First conn should be closed by the take-over.
	_, _, err = first.Read(context.Background())
	if err == nil {
		t.Fatal("first conn did not close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Logf("first close status = %d (any non-zero close is acceptable)", got)
	}
}

// C-50: Socket close → machine retained, sessions paused.
func TestDaemonWS_SocketClose_PausesSessions(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	regFrame, _ := json.Marshal(map[string]any{
		"type":                 "machine_register",
		"v":                    1,
		"machine_id":           "macbook-pro",
		"machine_display_name": "MacBook Pro",
		"daemon_version":       "0.0.1",
		"capabilities":         []string{"spawn"},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, regFrame); err != nil {
		t.Fatalf("write reg: %v", err)
	}
	startFrame, _ := json.Marshal(map[string]any{
		"type":       "session_started",
		"v":          1,
		"session_id": "sess-1",
		"machine_id": "macbook-pro",
		"metadata": map[string]any{
			"hostname": "macbook-pro", "cwd": "/", "model": "m", "project_name": "p",
			"started_at": 1,
		},
	})
	if err := conn.Write(context.Background(), websocket.MessageText, startFrame); err != nil {
		t.Fatalf("write start: %v", err)
	}
	if !waitFor(2*time.Second, func() bool {
		s, ok := ts.sessions.Get("sess-1")
		return ok && s.GetState() == "running"
	}) {
		t.Fatal("session_started did not land")
	}

	// Close the socket from the client side.
	_ = conn.Close(websocket.StatusNormalClosure, "client done")

	// The handler's defer should pause sessions; machine entry is retained.
	if !waitFor(2*time.Second, func() bool {
		s, ok := ts.sessions.Get("sess-1")
		return ok && s.GetState() == "paused"
	}) {
		t.Fatalf("session not paused after socket close")
	}
	if _, ok := ts.machines.Get("macbook-pro"); !ok {
		t.Errorf("machine entry should be retained after socket close")
	}
}

// C-51: Malformed JSON post-register → close 1008.
func TestDaemonWS_MalformedJSONPostRegister_Close1008(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialDaemon(t, ts, "test-machine", "secret")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()
	regFrame, _ := json.Marshal(map[string]any{
		"type":                 "machine_register",
		"v":                    1,
		"machine_id":           "macbook-pro",
		"machine_display_name": "MacBook Pro",
		"daemon_version":       "0.0.1",
		"capabilities":         []string{"spawn"},
	})
	_ = conn.Write(context.Background(), websocket.MessageText, regFrame)
	if !waitFor(2*time.Second, func() bool {
		_, ok := ts.machines.Get("macbook-pro")
		return ok
	}) {
		t.Fatal("register did not land")
	}
	if err := conn.Write(context.Background(), websocket.MessageText, []byte("not-json")); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("read returned nil error; expected close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Errorf("close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
}

// C-52: Race: 5 daemons × 20 events concurrently, all LastSeqs land correctly.
func TestDaemonWS_RaceMultipleDaemons(t *testing.T) {
	ts := newTestServer(t)
	const Daemons = 5
	const EventsPerSession = 20

	var wg sync.WaitGroup
	var errCount atomic.Int32
	for i := 0; i < Daemons; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine uses its own /v1/daemon WS, but the auth stub
			// only accepts "test-machine"; the registry distinguishes by
			// machine_register payload.
			conn, _, err := dialDaemon(t, ts, "test-machine", "secret")
			if err != nil {
				errCount.Add(1)
				t.Errorf("dial %d: %v", i, err)
				return
			}
			defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

			machineID := fmt.Sprintf("machine-%d", i)
			sessionID := fmt.Sprintf("sess-%d", i)
			regFrame, _ := json.Marshal(map[string]any{
				"type":                 "machine_register",
				"v":                    1,
				"machine_id":           machineID,
				"machine_display_name": machineID,
				"daemon_version":       "0.0.1",
				"capabilities":         []string{"spawn"},
			})
			if err := conn.Write(context.Background(), websocket.MessageText, regFrame); err != nil {
				errCount.Add(1)
				return
			}
			startFrame, _ := json.Marshal(map[string]any{
				"type":       "session_started",
				"v":          1,
				"session_id": sessionID,
				"machine_id": machineID,
				"metadata": map[string]any{
					"hostname": machineID, "cwd": "/", "model": "m", "project_name": "p",
					"started_at": 1,
				},
			})
			if err := conn.Write(context.Background(), websocket.MessageText, startFrame); err != nil {
				errCount.Add(1)
				return
			}
			for seq := 1; seq <= EventsPerSession; seq++ {
				ev, _ := json.Marshal(map[string]any{
					"type":       "session_event",
					"v":          1,
					"session_id": sessionID,
					"seq":        seq,
					"ts":         1700000000,
					"kind":       "agent_start",
					"data":       map[string]any{},
				})
				if err := conn.Write(context.Background(), websocket.MessageText, ev); err != nil {
					errCount.Add(1)
					return
				}
			}
		}()
	}
	wg.Wait()
	if errCount.Load() > 0 {
		t.Fatalf("had %d errors during race", errCount.Load())
	}

	// Wait for all sessions to settle at LastSeq=EventsPerSession.
	if !waitFor(5*time.Second, func() bool {
		for i := 0; i < Daemons; i++ {
			s, ok := ts.sessions.Get(fmt.Sprintf("sess-%d", i))
			if !ok || s.LastSeq != EventsPerSession {
				return false
			}
		}
		return true
	}) {
		for i := 0; i < Daemons; i++ {
			s, ok := ts.sessions.Get(fmt.Sprintf("sess-%d", i))
			if !ok {
				t.Errorf("sess-%d not registered", i)
			} else {
				t.Errorf("sess-%d LastSeq=%d want %d", i, s.LastSeq, EventsPerSession)
			}
		}
		t.FailNow()
	}
}

// waitFor polls fn until it returns true or the timeout fires.
func waitFor(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fn()
}
