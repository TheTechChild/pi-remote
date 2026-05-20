// SPDX-License-Identifier: MIT
package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

func dialClient(t *testing.T, ts *testServer, cookie string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if cookie != "" {
		header.Set("Cookie", "CF_Authorization="+cookie)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, ts.wsURL("/v1/client"), &websocket.DialOptions{
		HTTPHeader: header,
	})
}

// C-53: No cookie → 403 with ERR_COORD_AUTH_REQUIRED.
func TestClientWS_NoCookie_403(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.srv.URL + "/v1/client")
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

// C-54: Fixture cookie → 101 upgrade.
func TestClientWS_FixtureCookie_101(t *testing.T) {
	ts := newTestServer(t)
	conn, resp, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
}

// C-55: Wrong cookie value → 403 (table cases).
func TestClientWS_WrongCookieValue_403(t *testing.T) {
	ts := newTestServer(t)
	for _, val := range []string{"test-jwt-expired", "test-jwt-malformed", "garbage"} {
		val := val
		t.Run(val, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, ts.srv.URL+"/v1/client", nil)
			req.Header.Set("Cookie", "CF_Authorization="+val)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("status = %d, want 403", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "ERR_COORD_AUTH_REQUIRED") {
				t.Errorf("body = %q, want substring ERR_COORD_AUTH_REQUIRED", string(body))
			}
		})
	}
}

// C-56: First frame not client_hello → close 1008.
func TestClientWS_FirstFrameNotHello_Close1008(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	msg, _ := json.Marshal(map[string]any{
		"type": "something_else",
		"v":    1,
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("read returned nil error; expected close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Errorf("close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
}

// C-57: client_hello{client_id:test-client-1} → log "client_hello accepted",
// conn stays open.
func TestClientWS_HelloAccepted_LogsAndStaysOpen(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	msg, _ := json.Marshal(map[string]any{
		"type":        "client_hello",
		"v":           1,
		"client_id":   "test-client-1",
		"app_version": "0.0.1",
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !waitFor(2*time.Second, func() bool {
		return ts.logBuf.Contains("client_hello accepted")
	}) {
		t.Errorf("expected log 'client_hello accepted', got:\n%s", ts.logBuf.String())
	}

	// Conn should still be open — set a short read deadline; expect timeout,
	// not close.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Errorf("read returned no error; expected timeout")
	} else if ctx.Err() == nil && websocket.CloseStatus(err) != -1 {
		t.Errorf("conn closed unexpectedly: %v (close status %d)", err, websocket.CloseStatus(err))
	}
}

// C-58: client_hello{client_id:"not-real"} → close 1008.
func TestClientWS_UnknownClient_Close1008(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	msg, _ := json.Marshal(map[string]any{
		"type":        "client_hello",
		"v":           1,
		"client_id":   "not-real",
		"app_version": "0.0.1",
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("read returned nil error; expected close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Errorf("close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
}

// C-59: client_hello missing required fields → close 1008.
func TestClientWS_HelloMissingFields_Close1008(t *testing.T) {
	ts := newTestServer(t)
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	// missing client_id and app_version
	msg, _ := json.Marshal(map[string]any{
		"type": "client_hello",
		"v":    1,
	})
	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err = conn.Read(context.Background())
	if err == nil {
		t.Fatal("read returned nil error; expected close")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Errorf("close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
}

func TestClientWS_AttachAndReplay(t *testing.T) {
	ts := newTestServer(t)

	// Pre-register a session
	var emptyMeta daemon_coordinator.SessionStartedJsonMetadata
	sess := ts.sessions.Register("sess-test-1", "mach-1", emptyMeta)

	// Append some entries
	entry1 := broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Ts: time.Now().Unix(), Payload: []byte(`{"type":"session_pty","seq":1,"session_id":"sess-test-1","bytes":"aGVsbG8=","ts":0,"v":1}`)}
	entry2 := broker.Entry{Seq: 2, Kind: broker.EntryKindPty, Ts: time.Now().Unix(), Payload: []byte(`{"type":"session_pty","seq":2,"session_id":"sess-test-1","bytes":"d29ybGQ=","ts":0,"v":1}`)}
	ts.sessions.AppendToRing("sess-test-1", entry1)
	ts.sessions.AppendToRing("sess-test-1", entry2)

	// Connect client
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Send client_hello
	helloMsg, _ := json.Marshal(map[string]any{
		"type":        "client_hello",
		"v":           1,
		"client_id":   "test-client-1",
		"app_version": "0.0.1",
	})
	if err := conn.Write(context.Background(), websocket.MessageText, helloMsg); err != nil {
		t.Fatalf("write client_hello: %v", err)
	}

	// Send attach
	attachMsg, _ := json.Marshal(map[string]any{
		"type":       "attach",
		"v":          1,
		"session_id": "sess-test-1",
		"last_seq":   0,
	})
	if err := conn.Write(context.Background(), websocket.MessageText, attachMsg); err != nil {
		t.Fatalf("write attach: %v", err)
	}

	// Read replayed entry 1
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data1, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read replay 1: %v", err)
	}
	if !strings.Contains(string(data1), "aGVsbG8=") {
		t.Errorf("expected aGVsbG8=, got %s", string(data1))
	}

	// Read replayed entry 2
	_, data2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read replay 2: %v", err)
	}
	if !strings.Contains(string(data2), "d29ybGQ=") {
		t.Errorf("expected d29ybGQ=, got %s", string(data2))
	}

	// Send live entry
	entry3 := broker.Entry{Seq: 3, Kind: broker.EntryKindPty, Ts: time.Now().Unix(), Payload: []byte(`{"type":"session_pty","seq":3,"session_id":"sess-test-1","bytes":"bGl2ZQ==","ts":0,"v":1}`)}

	// Simulate what ingestor does:
	advanced := ts.sessions.AdvanceSeq("sess-test-1", 3)
	if !advanced {
		t.Fatal("AdvanceSeq failed")
	}
	ts.sessions.AppendToRing("sess-test-1", entry3)
	clients := sess.GetAttachedClients()
	for _, c := range clients {
		c.Send(entry3.Payload)
	}

	// Read live entry on client
	_, data3, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if !strings.Contains(string(data3), "bGl2ZQ==") {
		t.Errorf("expected bGl2ZQ==, got %s", string(data3))
	}
}

func TestClientWS_AttachReplayUnavailable(t *testing.T) {
	ts := newTestServer(t)

	// Pre-register a session
	var emptyMeta daemon_coordinator.SessionStartedJsonMetadata
	sess := ts.sessions.Register("sess-test-2", "mach-1", emptyMeta)

	// Evict older entries by using a very small maxBytes for testing, or just simulate it directly
	// Let's set maxBytes of Ring to 15, and append large entries
	sess.Ring.MaxBytes = 15

	entry1 := broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Payload: []byte(`{"type":"session_pty","seq":1,"session_id":"sess-test-2","bytes":"aGVsbG8=","ts":0,"v":1}`)}
	entry2 := broker.Entry{Seq: 2, Kind: broker.EntryKindPty, Payload: []byte(`{"type":"session_pty","seq":2,"session_id":"sess-test-2","bytes":"d29ybGQ=","ts":0,"v":1}`)}

	ts.sessions.AppendToRing("sess-test-2", entry1) // will be evicted when 2 is appended
	ts.sessions.AppendToRing("sess-test-2", entry2)

	// Connect client
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Send client_hello
	helloMsg, _ := json.Marshal(map[string]any{
		"type":        "client_hello",
		"v":           1,
		"client_id":   "test-client-1",
		"app_version": "0.0.1",
	})
	if err := conn.Write(context.Background(), websocket.MessageText, helloMsg); err != nil {
		t.Fatalf("write client_hello: %v", err)
	}

	// Send attach requesting last_seq = 1 (which is < earliestSeq = 2)
	attachMsg, _ := json.Marshal(map[string]any{
		"type":       "attach",
		"v":          1,
		"session_id": "sess-test-2",
		"last_seq":   1,
	})
	if err := conn.Write(context.Background(), websocket.MessageText, attachMsg); err != nil {
		t.Fatalf("write attach: %v", err)
	}

	// Should read replay_unavailable control frame
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read replay_unavailable: %v", err)
	}
	if !strings.Contains(string(data), "replay_unavailable") {
		t.Errorf("expected replay_unavailable, got %s", string(data))
	}
}

func TestClientWS_Detach(t *testing.T) {
	ts := newTestServer(t)

	// Pre-register a session
	var emptyMeta daemon_coordinator.SessionStartedJsonMetadata
	sess := ts.sessions.Register("sess-test-3", "mach-1", emptyMeta)

	// Connect client
	conn, _, err := dialClient(t, ts, "test-jwt-clayton")
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Send client_hello
	helloMsg, _ := json.Marshal(map[string]any{
		"type":        "client_hello",
		"v":           1,
		"client_id":   "test-client-1",
		"app_version": "0.0.1",
	})
	if err := conn.Write(context.Background(), websocket.MessageText, helloMsg); err != nil {
		t.Fatalf("write client_hello: %v", err)
	}

	// Send attach
	attachMsg, _ := json.Marshal(map[string]any{
		"type":       "attach",
		"v":          1,
		"session_id": "sess-test-3",
		"last_seq":   0,
	})
	if err := conn.Write(context.Background(), websocket.MessageText, attachMsg); err != nil {
		t.Fatalf("write attach: %v", err)
	}

	// Send detach
	detachMsg, _ := json.Marshal(map[string]any{
		"type":       "detach",
		"v":          1,
		"session_id": "sess-test-3",
	})
	// Let's sleep a tiny bit to make sure attach completes before detach
	time.Sleep(50 * time.Millisecond)
	if err := conn.Write(context.Background(), websocket.MessageText, detachMsg); err != nil {
		t.Fatalf("write detach: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Send live entry
	entry := broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Payload: []byte(`{"type":"session_pty","seq":1,"session_id":"sess-test-3","bytes":"aGVsbG8=","ts":0,"v":1}`)}
	ts.sessions.AppendToRing("sess-test-3", entry)
	clients := sess.GetAttachedClients()
	for _, c := range clients {
		c.Send(entry.Payload)
	}

	// Verify that client does NOT receive the live entry (timeout on read)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Errorf("read returned no error; expected timeout due to detach")
	} else if ctx.Err() == nil {
		t.Errorf("unexpected read error: %v", err)
	}
}
