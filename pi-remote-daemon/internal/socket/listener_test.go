// SPDX-License-Identifier: MIT
package socket_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheTechChild/pi-remote-daemon/internal/socket"
)

// shortTempDir returns a short-path temp directory. macOS's t.TempDir()
// returns paths under /var/folders/... that easily exceed AF_UNIX's
// 104-byte sun_path limit. /tmp is short and works on darwin and linux.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi-remote-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestListen_BindsAtPathWith0600(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "sub", "daemon.sock")

	ln, err := socket.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", mode)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if mode := parent.Mode().Perm(); mode != 0o700 {
		t.Fatalf("parent dir mode = %o, want 0700", mode)
	}
}

func TestListen_AcceptsConnection(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "daemon.sock")
	ln, err := socket.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()

	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if got := <-accepted; got == nil {
		t.Fatal("accept returned nil conn")
	}
}

func TestListen_StaleSocketFileRebinds(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "daemon.sock")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	ln, err := socket.Listen(path)
	if err != nil {
		t.Fatalf("Listen with stale socket should succeed, got: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
}

func TestListen_RefusesIfLiveListener(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "daemon.sock")
	first, err := socket.Listen(path)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	_, err = socket.Listen(path)
	if err == nil {
		t.Fatal("second Listen should fail when first is alive")
	}
	if !errors.Is(err, socket.ErrAlreadyRunning) {
		t.Fatalf("err = %v, want %v", err, socket.ErrAlreadyRunning)
	}
}

func TestListen_CloseUnlinksSocket(t *testing.T) {
	dir := shortTempDir(t)
	path := filepath.Join(dir, "daemon.sock")
	ln, err := socket.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket file still exists after Close: stat err = %v", err)
	}
}
