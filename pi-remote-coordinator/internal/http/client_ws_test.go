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
