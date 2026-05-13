// SPDX-License-Identifier: MIT
package session

import "sync/atomic"

// SeqAllocator is a concurrent-safe monotonic per-session counter. The
// daemon's multiplex calls Next() to allocate a `seq` for each outgoing
// session_event / session_pty / session_state_change / session_ended
// frame, per SPEC § 18.2. Allocation increments BEFORE write (drop-on-
// disconnect policy preserves seq across coordinator outages).
//
// Zero value is usable; no constructor required. This lets Session embed
// SeqAllocator without a ceremony in Register.
//
// Implementation: a single atomic uint64. Next is an atomic Add(1); Peek
// is an atomic Load. The wraparound at 2^64-1 is effectively impossible
// for our session lifetimes (≈ 5.8e11 years at 1ns/Next).
type SeqAllocator struct {
	n atomic.Uint64
}

// Next returns the next sequence number for this allocator. The first call
// returns 1; subsequent calls return monotonically increasing values. Safe
// for concurrent use.
func (a *SeqAllocator) Next() uint64 {
	return a.n.Add(1)
}

// Peek returns the last value allocated, or 0 if no Next() call has been
// made yet. Does not advance the counter. Used by the multiplex to read
// LastSeq for session_resume frames on reconnect.
func (a *SeqAllocator) Peek() uint64 {
	return a.n.Load()
}
