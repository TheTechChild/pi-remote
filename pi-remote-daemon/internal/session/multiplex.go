// SPDX-License-Identifier: MIT
package session

import (
	"time"
)

// Coord is the contract the multiplex requires from the coordinator
// client. Defined in this package (the consumer) per Go's
// accept-interfaces-return-structs convention - keeps the interface
// small, breaks the import cycle, and lets multiplex_test.go ship a
// tiny in-test stub without pulling the real coordinator package.
//
// Connected reports whether the client currently has a live WebSocket
// to the coordinator. The multiplex uses this for the drop-on-disconnect
// policy: events fired while !Connected allocate a seq (so the
// coordinator's gap detection sees the right numbers post-reconnect)
// but the frame is discarded. No buffering. Per SPEC § 7.8 and the
// Batch 2 plan.
//
// Send writes a single JSON frame to the coordinator. The argument is
// one of the typed frames returned by internal/coordinator/frames.go.
// Returns a non-nil error when the underlying conn is unavailable or
// the write fails; the multiplex treats any error the same as
// !Connected (seq advanced, frame discarded).
type Coord interface {
	Connected() bool
	Send(frame any) error
}

// LiveSession is the snapshot the multiplex returns from LiveSessions()
// for the coordinator client to drive session_resume emission on
// reconnect. SessionID identifies the session; LastSeq is its current
// allocator Peek value.
type LiveSession struct {
	Session Session
	LastSeq uint64
}

// Multiplex bridges the per-ext-connection registry events into the
// upstream coordinator WebSocket. One Multiplex per daemon process; it
// owns the seq allocators (one per session, embedded on Session) and
// reads Coord.Connected() to decide between write and drop.
//
// The Multiplex wires itself to a Registry's callbacks at construction
// time. It does not own the registry; the registry's lifetime is the
// daemon process and outlives any individual coordinator reconnect.
type Multiplex struct {
	// fields deliberately omitted in RED phase.
}

// NewMultiplex wires the registry's hooks to the multiplex and binds
// the multiplex to the supplied Coord. The machineID is included in
// session_started frames per the schema. The clock is used for
// timestamps on outgoing frames; pass time.Now in production.
func NewMultiplex(reg *Registry, coord Coord, machineID string, now func() time.Time) *Multiplex {
	_ = reg
	_ = coord
	_ = machineID
	_ = now
	// RED-phase stub.
	return &Multiplex{}
}

// LiveSessions returns a snapshot of every session in non-ended state
// with its current LastSeq. The coordinator client calls this on
// (re)connect to emit one session_resume per live session per SPEC
// § 7.8.
//
// The snapshot is taken under the registry's mutex; LastSeq for each
// session is read via Peek (does not advance).
func (m *Multiplex) LiveSessions() []LiveSession {
	// RED-phase stub.
	return nil
}
