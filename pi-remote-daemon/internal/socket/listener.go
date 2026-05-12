// SPDX-License-Identifier: MIT
package socket

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ErrAlreadyRunning is returned by Listen when an existing socket at the
// requested path has a live listener (probed by attempting a Unix dial).
// The caller should report this to the user and exit non-zero (SPEC § 7.4).
var ErrAlreadyRunning = errors.New("socket: another daemon instance is already running")

// dialProbeTimeout is how long Listen waits when dial-probing a stale-vs-live
// socket file. Connection-refused returns immediately; this only bounds the
// case where the kernel never replies (rare for AF_UNIX, defensive only).
const dialProbeTimeout = 250 * time.Millisecond

// Listener wraps a net.UnixListener with the daemon's
// path-management contract: the parent directory is created with 0700, the
// socket file with 0600, and the file is unlinked on Close.
type Listener struct {
	ln   *net.UnixListener
	path string
}

// Listen creates the parent directory (0700), enforces single-instance via
// a dial probe, unlinks any stale socket file, binds, and chmods the socket
// to 0600. The caller owns Close.
func Listen(socketPath string) (*Listener, error) {
	if socketPath == "" {
		return nil, errors.New("socket: empty path")
	}
	parent := filepath.Dir(socketPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("socket: mkdir parent %s: %w", parent, err)
	}

	if _, err := os.Stat(socketPath); err == nil {
		// File exists. Probe for a live listener.
		c, derr := net.DialTimeout("unix", socketPath, dialProbeTimeout)
		if derr == nil {
			_ = c.Close()
			return nil, fmt.Errorf("%w (socket: %s)", ErrAlreadyRunning, socketPath)
		}
		// Dial failed; treat as stale and unlink so Bind can succeed.
		if rerr := os.Remove(socketPath); rerr != nil {
			return nil, fmt.Errorf("socket: remove stale %s: %w", socketPath, rerr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("socket: stat %s: %w", socketPath, err)
	}

	addr := &net.UnixAddr{Net: "unix", Name: socketPath}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("socket: listen %s: %w", socketPath, err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("socket: chmod %s: %w", socketPath, err)
	}
	return &Listener{ln: ln, path: socketPath}, nil
}

// Accept proxies to the underlying UnixListener.
func (l *Listener) Accept() (net.Conn, error) {
	return l.ln.Accept()
}

// Close stops accepting new connections and unlinks the socket file.
func (l *Listener) Close() error {
	err := l.ln.Close()
	// SetUnlinkOnClose may or may not fire depending on platform / state;
	// remove unconditionally so Listen-after-Close cleans up cleanly.
	if rerr := os.Remove(l.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = fmt.Errorf("socket: remove %s: %w", l.path, rerr)
	}
	return err
}

// Path returns the bound socket path. Useful for logging.
func (l *Listener) Path() string { return l.path }
