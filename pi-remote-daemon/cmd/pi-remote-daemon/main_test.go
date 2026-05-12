// SPDX-License-Identifier: MIT
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheTechChild/pi-remote-daemon/internal/config"
)

// shortTempDir returns a /tmp-rooted temp dir; macOS t.TempDir paths exceed
// AF_UNIX's 104-byte sun_path limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi-remote-main-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// TestRun_AcceptsRegisterAndShutsDownOnContextCancel exercises the full
// daemon wireup: listener up, ext-side dial, register handshake, then
// graceful shutdown via context cancel.
func TestRun_AcceptsRegisterAndShutsDownOnContextCancel(t *testing.T) {
	dir := shortTempDir(t)
	cfg := &config.Config{
		Socket: config.SocketConfig{Path: filepath.Join(dir, "daemon.sock")},
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- run(ctx, cfg, discardLogger()) }()

	// Wait for the socket to appear, then dial.
	deadline := time.Now().Add(2 * time.Second)
	var c net.Conn
	for time.Now().Before(deadline) {
		var derr error
		c, derr = net.Dial("unix", cfg.Socket.Path)
		if derr == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("daemon never started accepting on its socket")
	}
	t.Cleanup(func() { _ = c.Close() })

	reg := map[string]any{
		"type":         "register",
		"v":            1,
		"session_id":   "smoke-1",
		"cwd":          "/tmp",
		"project_name": "smoke",
		"tmux_target":  "untmuxed:0.0",
		"pid":          1,
		"hostname":     "host",
		"model":        "claude",
		"started_at":   1730000000000,
	}
	b, _ := json.Marshal(reg)
	if _, err := c.Write(append(b, '\n')); err != nil {
		t.Fatalf("write register: %v", err)
	}

	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack["accepted"] != true {
		t.Fatalf("ack accepted = %v, want true", ack["accepted"])
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after context cancel")
	}

	// Socket file should be unlinked.
	if _, err := os.Stat(cfg.Socket.Path); !os.IsNotExist(err) {
		t.Fatalf("socket still present after shutdown: stat err=%v", err)
	}
}

func TestExpandPath_TildeReplacedWithHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	got, err := expandPath("~/.pi-remote/daemon.sock")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	want := filepath.Join(home, ".pi-remote/daemon.sock")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandPath_AbsolutePathPassthrough(t *testing.T) {
	got, err := expandPath("/var/run/x.sock")
	if err != nil {
		t.Fatalf("expandPath: %v", err)
	}
	if got != "/var/run/x.sock" {
		t.Fatalf("got %q, want passthrough", got)
	}
}

func TestExpandPath_EmptyErrors(t *testing.T) {
	if _, err := expandPath(""); err == nil {
		t.Fatal("expandPath(\"\") should error")
	}
}
