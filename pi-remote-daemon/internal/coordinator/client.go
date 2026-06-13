// SPDX-License-Identifier: MIT
package coordinator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	pb "github.com/TheTechChild/pi-remote-daemon/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// ErrNotConnected is returned by Send when the client has no live
// WebSocket. The multiplex uses this as a signal to advance seq and
// discard the frame (drop-on-disconnect policy).
var ErrNotConnected = errors.New("coordinator: not connected")

// Backoff schedule constants per SPEC § 7.8 and the Batch 2 plan:
// "exponential backoff 1s → 60s on reconnect".
const (
	backoffInitial = 1 * time.Second
	backoffMax     = 60 * time.Second
)

// Spawner defines the interface to trigger a new Pi/tmux session and handle PTY events.
type Spawner interface {
	Spawn(ctx context.Context, cwd string, requestID string)
	WritePty(sessionID string, data []byte) error
	ResizePty(sessionID string, cols, rows int) error
}

// Config bundles the inputs to NewClient. URL, IDFile, SecretFile come
// from config.toml; MachineRegister is built from machine-level config
// at startup; LiveSnapshot is the multiplex's LiveSessions method;
// Logger and Clock are dependencies; the rest are tuning knobs with
// sensible defaults applied in NewClient.
type Config struct {
	URL             string
	IDFile          string
	SecretFile      string
	MachineRegister MachineRegisterInput
	LiveSnapshot    func() []session.LiveSession
	Spawner         Spawner
	Clock           Clock
	Logger          *slog.Logger
}

// Client is the daemon-side WebSocket connection to the coordinator. It
// runs a single goroutine (Run) that dials, sends machine_register +
// one session_resume per live session, then processes frames until the
// connection drops; on disconnect it backs off (1s -> 60s exponential)
// and reconnects. Identity for reconnect is the machine_id in the
// MachineRegister input - per SPEC § 7.8 the coordinator treats a
// duplicate machine_register as a take-over.
//
// The client does NOT layer an application-level ping/pong over the
// daemon-coordinator link. The coder/websocket library handles
// transport-level keepalive. (The ext-daemon link's heartbeat is a
// Pi-process liveness signal, distinct layer.)
type Client struct {
	cfg Config
	log *slog.Logger

	// connMu protects conn and is held during every write so concurrent
	// Send calls do not interleave bytes on the wire. Reads happen on
	// the run goroutine without needing the mutex (only one reader).
	connMu sync.Mutex
	conn   *websocket.Conn

	// resumePending is set by NotifySuspend; the next successful
	// handshake emits machine_resumed and clears it (SPEC § 7.7).
	resumePending atomic.Bool
}

// NotifySuspend implements the daemon side of SPEC § 7.7: best-effort
// send machine_suspending, close the WebSocket gracefully, and arrange
// for machine_resumed to follow the next reconnect handshake. Called
// from the suspend watcher inside the OS pre-sleep grace window.
func (c *Client) NotifySuspend() {
	c.resumePending.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.write(ctx, map[string]any{"type": "machine_suspending", "v": 1}); err != nil {
		c.log.Warn("machine_suspending send failed", slog.String("err", err.Error()))
	}
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "machine suspending")
	}
	c.log.Info("machine_suspending sent; websocket closed for sleep")
}

// NotifyResume logs the wake transition. The reconnect loop re-dials on
// its own (any in-flight backoff timer fires shortly after wake), and
// the handshake appends machine_resumed because resumePending is set.
func (c *Client) NotifyResume() {
	c.log.Info("system resumed; awaiting reconnect")
}

// NewClient constructs but does not start the client. Call Run on a
// dedicated goroutine; cancel its context to stop. Run owns its own
// reconnect loop; the caller does not poke at internals.
func NewClient(cfg Config) *Client {
	if cfg.Clock == nil {
		cfg.Clock = RealClock()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.LiveSnapshot == nil {
		cfg.LiveSnapshot = func() []session.LiveSession { return nil }
	}
	return &Client{cfg: cfg, log: cfg.Logger}
}

// Run blocks until ctx is canceled, owning the dial/reconnect loop. On
// each successful connect it emits machine_register, then one
// session_resume per live session from cfg.LiveSnapshot. On disconnect
// it backs off per the exponential schedule and reconnects. A clean
// shutdown (ctx canceled) closes the WebSocket with status 1000.
func (c *Client) Run(ctx context.Context) error {
	backoff := backoffInitial
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		// Attempt one connect cycle (dial + register + read loop).
		// runOnce returns when the connection drops, returning the
		// reason for diagnostic logs.
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			c.log.Warn("coordinator connection ended",
				slog.String("err", err.Error()),
				slog.Duration("next_backoff", backoff))
		}

		// Backoff before reconnect. The Clock abstraction lets tests
		// drive the schedule deterministically and watch ctx for cancel.
		if err := c.cfg.Clock.Sleep(ctx, backoff); err != nil {
			return nil
		}
		backoff = nextBackoff(backoff)
	}
}

// nextBackoff doubles backoff up to the cap.
func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > backoffMax {
		return backoffMax
	}
	return next
}

// runOnce performs a single dial + register + read-loop cycle. Returns
// when the conn drops (or ctx is canceled). The returned error
// describes the failure mode for the calling backoff loop's log; nil
// return is only possible on ctx cancel during dial.
func (c *Client) runOnce(ctx context.Context) error {
	creds, err := LoadCredentials(c.cfg.IDFile, c.cfg.SecretFile)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	for _, w := range creds.Warnings {
		c.log.Warn(w)
	}

	header := http.Header{}
	header.Set("CF-Access-Client-Id", creds.ID)
	header.Set("CF-Access-Client-Secret", creds.Secret)

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, c.cfg.URL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	cancel()
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return fmt.Errorf("dial: %w (status=%d)", err, status)
	}
	// coder/websocket reads from the body to detect close on the
	// server side; the docs note we should not close resp.Body
	// manually for WS connections.

	c.setConn(conn)
	defer c.clearConn()

	// Keepalive: proxies in front of the coordinator (Cloudflare's tunnel
	// edge in particular) drop WebSockets after ~100s of idle, and per
	// SPEC § 7.8 events emitted during the dead window are lost. Ping
	// every 30s so a quiet daemon link stays alive; a failed ping closes
	// the conn so the backoff loop re-dials promptly instead of writing
	// into a zombie connection.
	pingCtx, pingCancel := context.WithCancel(ctx)
	defer pingCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				pctx, pcancel := context.WithTimeout(pingCtx, 10*time.Second)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					if pingCtx.Err() == nil {
						c.log.Warn("keepalive ping failed; closing connection",
							slog.String("err", err.Error()))
						_ = conn.Close(websocket.StatusAbnormalClosure, "keepalive failed")
					}
					return
				}
			}
		}
	}()

	if err := c.handshake(ctx); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "handshake failed")
		return fmt.Errorf("handshake: %w", err)
	}

	// Read loop. Unknown frame types are logged and dropped; valid
	// frames the daemon doesn't yet act on are also logged and dropped
	// (M2/M5/M6 will implement spawn/pty_input/abort).
	return c.readLoop(ctx, conn)
}

// handshake writes machine_register followed by one session_resume per
// live session. Per SPEC § 7.8 the resume frames carry last_seq_emitted
// so the broker can compute backfill.
func (c *Client) handshake(ctx context.Context) error {
	reg := NewMachineRegister(c.cfg.MachineRegister)
	if err := c.write(ctx, reg); err != nil {
		return fmt.Errorf("machine_register: %w", err)
	}
	for _, ls := range c.cfg.LiveSnapshot() {
		resume := NewSessionResume(ls.Session, c.cfg.MachineRegister.MachineID, "", ls.LastSeq)
		if err := c.write(ctx, resume); err != nil {
			return fmt.Errorf("session_resume %s: %w", ls.Session.SessionID, err)
		}
	}
	if c.resumePending.Swap(false) {
		if err := c.write(ctx, map[string]any{"type": "machine_resumed", "v": 1}); err != nil {
			return fmt.Errorf("machine_resumed: %w", err)
		}
	}
	return nil
}

// readLoop drains incoming frames until the connection closes. Any
// read error returns; the run loop wraps with backoff. Unknown frame
// types are logged at DEBUG and dropped to satisfy C21 and future-
// proof against schema additions the daemon doesn't yet handle.
func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var frame map[string]any
		err := wsjson.Read(ctx, conn, &frame)
		if err != nil {
			return err
		}
		ftype, _ := frame["type"].(string)
		c.log.Debug("coordinator frame received",
			slog.String("type", ftype))

		if ftype == "spawn_request" {
			payloadBytes, err := json.Marshal(frame)
			if err != nil {
				c.log.Error("failed to marshal spawn_request frame", slog.String("err", err.Error()))
				continue
			}
			var req pb.SpawnRequestJson
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				c.log.Error("failed to unmarshal spawn_request", slog.String("err", err.Error()))
				continue
			}
			if c.cfg.Spawner != nil {
				go c.cfg.Spawner.Spawn(ctx, req.Cwd, req.RequestId)
			} else {
				c.log.Warn("spawner not configured, ignoring spawn_request")
			}
		} else if ftype == "pty_input" {
			payloadBytes, err := json.Marshal(frame)
			if err != nil {
				c.log.Error("failed to marshal pty_input frame", slog.String("err", err.Error()))
				continue
			}
			var req pb.PtyInputJson
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				c.log.Error("failed to unmarshal pty_input", slog.String("err", err.Error()))
				continue
			}
			if c.cfg.Spawner != nil {
				decoded, err := base64.StdEncoding.DecodeString(req.Bytes)
				if err != nil {
					c.log.Error("failed to decode base64 pty input bytes", slog.String("err", err.Error()))
					continue
				}
				if err := c.cfg.Spawner.WritePty(req.SessionId, decoded); err != nil {
					c.log.Error("failed to write pty input", slog.String("session_id", req.SessionId), slog.String("err", err.Error()))
				}
			} else {
				c.log.Warn("spawner not configured, ignoring pty_input")
			}
		} else if ftype == "pty_resize" {
			payloadBytes, err := json.Marshal(frame)
			if err != nil {
				c.log.Error("failed to marshal pty_resize frame", slog.String("err", err.Error()))
				continue
			}
			var req pb.PtyResizeJson
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				c.log.Error("failed to unmarshal pty_resize", slog.String("err", err.Error()))
				continue
			}
			if c.cfg.Spawner != nil {
				if err := c.cfg.Spawner.ResizePty(req.SessionId, req.Cols, req.Rows); err != nil {
					c.log.Error("failed to resize pty", slog.String("session_id", req.SessionId), slog.String("err", err.Error()))
				}
			} else {
				c.log.Warn("spawner not configured, ignoring pty_resize")
			}
		}
	}
}

// write serializes a single frame to JSON and writes it as a text
// WebSocket frame. Holds connMu so concurrent Send calls do not
// interleave bytes.
func (c *Client) write(ctx context.Context, frame any) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return ErrNotConnected
	}
	return wsjson.Write(ctx, c.conn, frame)
}

// setConn / clearConn / getConn manage the conn pointer under connMu.
func (c *Client) setConn(conn *websocket.Conn) {
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
}

func (c *Client) clearConn() {
	c.connMu.Lock()
	if c.conn != nil {
		// Close with normal closure when the run loop is exiting
		// cleanly; on error paths the caller has already closed with
		// a more specific code. Errors here are ignored - the
		// connection is dying.
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
	}
	c.conn = nil
	c.connMu.Unlock()
}

// Connected reports whether the client currently has a live WebSocket
// to the coordinator. Used by the multiplex for the drop-on-disconnect
// gate.
func (c *Client) Connected() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn != nil
}

// Send writes a single JSON frame to the coordinator. Returns
// ErrNotConnected when no live WebSocket exists. Returns the underlying
// write error when the WebSocket is live but the write fails.
//
// Uses a fresh background context so a Send call from a goroutine
// other than Run does not inherit Run's context cancellation.
func (c *Client) Send(frame any) error {
	return c.write(context.Background(), frame)
}

// Close performs a clean shutdown: closes the WebSocket if one is
// live. Safe to call from any goroutine. Idempotent.
func (c *Client) Close() error {
	c.clearConn()
	return nil
}

// Compile-time guard: Client satisfies the session.Coord interface.
// The multiplex consumes it through that interface; this assertion
// keeps the two contracts in sync.
var _ session.Coord = (*Client)(nil)
