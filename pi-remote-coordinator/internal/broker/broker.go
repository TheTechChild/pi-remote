// SPDX-License-Identifier: MIT
package broker

import "time"

// Phase-0 type stubs only (per § 23.4 Task 4 brief). The full ring buffer +
// global LRU is implemented in milestone M4.

// EntryKind discriminates between structured events and pty bytes in a
// session's ring buffer.
type EntryKind uint8

const (
	EntryKindEvent EntryKind = iota
	EntryKindPty
)

// Entry is one record in a session ring buffer.
type Entry struct {
	Seq     uint64
	Kind    EntryKind
	Ts      int64
	Payload []byte
}

// RingBuffer is the per-session circular byte-bounded buffer described in
// SPEC.md § 18.2.
type RingBuffer struct {
	Entries     []Entry
	Head        int
	Tail        int
	Bytes       int64
	MaxBytes    int64
	EarliestSeq uint64
	LatestSeq   uint64
	LastTouched time.Time
}
