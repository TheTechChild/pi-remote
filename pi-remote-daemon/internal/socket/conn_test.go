// SPDX-License-Identifier: MIT
package socket_test

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
	"github.com/TheTechChild/pi-remote-daemon/internal/socket"
)

// discardLogger silences slog output in tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func validRegister(sessionID string, pid int) string {
	m := map[string]any{
		"type":         "register",
		"v":            1,
		"session_id":   sessionID,
		"cwd":          "/tmp/proj",
		"project_name": "proj",
		"tmux_target":  "untmuxed:0.0",
		"pid":          pid,
		"hostname":     "host",
		"model":        "claude",
		"started_at":   1730000000000,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// servePipe wires a fresh Handler against an in-memory pipe and returns the
// client end. The handler goroutine exits when the client closes its end.
func servePipe(t *testing.T, reg *session.Registry) (client net.Conn, done <-chan struct{}) {
	t.Helper()
	c1, c2 := net.Pipe()
	h := socket.NewHandler(reg, nil, discardLogger())
	d := make(chan struct{})
	go func() {
		h.Serve(c2)
		close(d)
	}()
	t.Cleanup(func() { _ = c1.Close() })
	return c1, d
}

// readLine reads one JSONL frame from conn or fatals on timeout.
func readLine(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("readLine: %v", err)
	}
	return strings.TrimRight(line, "\n")
}

func TestServe_HappyRegister_AckedAndStored(t *testing.T) {
	reg := session.NewRegistry()
	client, _ := servePipe(t, reg)

	if _, err := client.Write([]byte(validRegister("sess-ok", 42) + "\n")); err != nil {
		t.Fatalf("write register: %v", err)
	}

	line := readLine(t, client)
	var ack map[string]any
	if err := json.Unmarshal([]byte(line), &ack); err != nil {
		t.Fatalf("unmarshal ack: %v (line=%q)", err, line)
	}
	if ack["type"] != "register_ack" {
		t.Fatalf("ack type = %v, want register_ack", ack["type"])
	}
	if ack["accepted"] != true {
		t.Fatalf("ack accepted = %v, want true", ack["accepted"])
	}
	if ack["session_id"] != "sess-ok" {
		t.Fatalf("ack session_id = %v, want sess-ok", ack["session_id"])
	}

	got, ok := reg.Get("sess-ok")
	if !ok {
		t.Fatal("registry missing session after register")
	}
	if got.PID != 42 {
		t.Fatalf("registry pid = %d, want 42", got.PID)
	}
}

func TestServe_DuplicateSessionDifferentPID_RejectedWithErrCode(t *testing.T) {
	reg := session.NewRegistry()
	reg.Register(&session.Session{
		SessionID: "sess-dup",
		PID:       1,
		State:     session.StateRunning,
	})

	client, _ := servePipe(t, reg)
	_, _ = client.Write([]byte(validRegister("sess-dup", 2) + "\n"))

	line := readLine(t, client)
	var ack map[string]any
	if err := json.Unmarshal([]byte(line), &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack["accepted"] != false {
		t.Fatalf("accepted = %v, want false", ack["accepted"])
	}
	if ack["reason"] != "ERR_DAEMON_DUPLICATE_SESSION_ID" {
		t.Fatalf("reason = %v, want ERR_DAEMON_DUPLICATE_SESSION_ID", ack["reason"])
	}
}

func TestServe_HeartbeatUpdatesLastHeartbeat(t *testing.T) {
	reg := session.NewRegistry()
	client, _ := servePipe(t, reg)
	_, _ = client.Write([]byte(validRegister("sess-hb", 1) + "\n"))
	_ = readLine(t, client) // discard ack

	hb := `{"type":"heartbeat","ts":1730000099000}` + "\n"
	if _, err := client.Write([]byte(hb)); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	// Heartbeat is one-way: poll the registry for the field flip.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s, ok := reg.Get("sess-hb")
		if ok && !s.LastHeartbeat.IsZero() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("LastHeartbeat never updated")
}

func TestServe_DisconnectRemovesSession(t *testing.T) {
	reg := session.NewRegistry()
	client, done := servePipe(t, reg)
	_, _ = client.Write([]byte(validRegister("sess-disc", 1) + "\n"))
	_ = readLine(t, client)

	_, _ = client.Write([]byte(`{"type":"disconnect","reason":"session_shutdown"}` + "\n"))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after disconnect")
	}
	if _, ok := reg.Get("sess-disc"); ok {
		t.Fatal("session still in registry after disconnect")
	}
}

func TestServe_ConnCloseWithoutDisconnect_MarksEnded(t *testing.T) {
	reg := session.NewRegistry()
	client, done := servePipe(t, reg)
	_, _ = client.Write([]byte(validRegister("sess-drop", 1) + "\n"))
	_ = readLine(t, client)

	_ = client.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return on conn close")
	}
	got, ok := reg.Get("sess-drop")
	if !ok {
		t.Fatal("entry should be retained after abrupt close")
	}
	if got.State != session.StateEnded {
		t.Fatalf("state = %q, want %q", got.State, session.StateEnded)
	}
}

func TestServe_MalformedJSONOnFirstFrame_ClosesConnRegistryUntouched(t *testing.T) {
	reg := session.NewRegistry()
	client, done := servePipe(t, reg)

	_, _ = client.Write([]byte("{not json\n"))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after malformed frame")
	}
	// Registry must be empty.
	if _, ok := reg.Get("anything"); ok {
		t.Fatal("registry contains entry after malformed-input rejection")
	}
}

func TestServe_OversizeFrame_ClosesConn(t *testing.T) {
	reg := session.NewRegistry()
	client, done := servePipe(t, reg)

	// 2MB blob — exceeds the 1MB scanner cap; scanner returns ErrTooLong.
	huge := strings.Repeat("a", 2*1024*1024)
	_, _ = client.Write([]byte(huge))
	_ = client.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after oversize frame")
	}
	if _, ok := reg.Get("anything"); ok {
		t.Fatal("registry mutated by oversize input")
	}
}

func TestServe_NonRegisterFirstFrame_Rejected(t *testing.T) {
	reg := session.NewRegistry()
	client, done := servePipe(t, reg)

	_, _ = client.Write([]byte(`{"type":"heartbeat","ts":1}` + "\n"))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after pre-register heartbeat")
	}
	if _, ok := reg.Get("anything"); ok {
		t.Fatal("registry should be untouched")
	}
}
