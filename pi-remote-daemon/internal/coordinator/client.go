// SPDX-License-Identifier: MIT
package coordinator

import (
	"context"
	"errors"
	"log/slog"

	"github.com/coder/websocket"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// ErrNotConnected is returned by Send when the client has no live
// WebSocket. The multiplex uses this as a signal to advance seq and
// discard the frame (drop-on-disconnect policy).
var ErrNotConnected = errors.New("coordinator: not connected")

// Config bundles the inputs to NewClient. URL, IDFile, SecretFile come
// from config.toml; MachineRegister is built from machine-level config
// at startup; LiveSnapshot is the multiplex's LiveSessions method;
// Logger and Clock are dependencies; the rest are tuning knobs with
// sensible defaults applied in NewClient.
type Config struct {
	URL            string
	IDFile         string
	SecretFile     string
	MachineRegister MachineRegisterInput
	LiveSnapshot   func() []session.LiveSession
	Clock          Clock
	Logger         *slog.Logger
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
	// fields deliberately omitted in RED phase.
}

// NewClient constructs but does not start the client. Call Run on a
// dedicated goroutine; cancel its context to stop. Run owns its own
// reconnect loop; the caller does not poke at internals.
func NewClient(cfg Config) *Client {
	_ = cfg
	// RED-phase stub.
	return &Client{}
}

// Run blocks until ctx is canceled, owning the dial/reconnect loop. On
// each successful connect it emits machine_register, then one
// session_resume per live session from cfg.LiveSnapshot. On disconnect
// it backs off per the exponential schedule and reconnects. A clean
// shutdown (ctx canceled) closes the WebSocket with status 1000.
func (c *Client) Run(ctx context.Context) error {
	_ = ctx
	// RED-phase stub.
	return errors.New("not implemented")
}

// Connected reports whether the client currently has a live WebSocket
// to the coordinator. Used by the multiplex for the drop-on-disconnect
// gate.
func (c *Client) Connected() bool {
	// RED-phase stub.
	return false
}

// Send writes a single JSON frame to the coordinator. Returns
// ErrNotConnected when no live WebSocket exists. Returns the underlying
// write error when the WebSocket is live but the write fails.
func (c *Client) Send(frame any) error {
	_ = frame
	// RED-phase stub.
	return ErrNotConnected
}

// Close performs a clean shutdown: sends a WebSocket close frame with
// status 1000 ("normal closure") and stops the run loop. Safe to call
// from any goroutine. Idempotent.
func (c *Client) Close() error {
	// RED-phase stub.
	return nil
}

// Compile-time guard: Client satisfies the session.Coord interface.
// The multiplex consumes it through that interface; this assertion
// keeps the two contracts in sync.
var _ session.Coord = (*Client)(nil)

// Unused-import guard for websocket package during RED phase. The
// stub does not yet call into the library, but the dep is reserved
// here so the GREEN-phase implementation can reach for it directly.
var _ = websocket.MessageText
