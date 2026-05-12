// SPDX-License-Identifier: MIT
package session

// SeqAllocator is a concurrent-safe monotonic per-session counter. The
// daemon's multiplex calls Next() to allocate a `seq` for each outgoing
// session_event / session_pty / session_state_change / session_ended
// frame, per SPEC § 18.2. Allocation increments BEFORE write (drop-on-
// disconnect policy preserves seq across coordinator outages).
//
// Zero value is usable; no constructor required. This lets Session embed
// SeqAllocator without a ceremony in Register.
type SeqAllocator struct {
	// implementation deliberately omitted in RED phase.
}

// Next returns the next sequence number for this allocator. The first call
// returns 1; subsequent calls return monotonically increasing values. Safe
// for concurrent use.
func (a *SeqAllocator) Next() uint64 {
	// RED-phase stub. Tests should fail meaningfully on this.
	return 0
}

// Peek returns the last value allocated, or 0 if no Next() call has been
// made yet. Does not advance the counter. Used by the multiplex to read
// LastSeq for session_resume frames on reconnect.
func (a *SeqAllocator) Peek() uint64 {
	// RED-phase stub.
	return 0
}
