// SPDX-License-Identifier: MIT
package broker

import (
	"sync"
	"time"
)

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
	Payload []byte // serialized JSON for events; raw bytes for pty
}

// RingBuffer is the per-session circular byte-bounded buffer described in
// SPEC.md § 18.2.
type RingBuffer struct {
	mu          sync.RWMutex
	Entries     []Entry
	Head        int
	Tail        int
	Bytes       int64
	MaxBytes    int64
	EarliestSeq uint64
	LatestSeq   uint64
	LastTouched time.Time
}

// NewRingBuffer creates a RingBuffer with a dynamic starting maxBytes limit.
func NewRingBuffer(maxBytes int64) *RingBuffer {
	return &RingBuffer{
		Entries:     make([]Entry, 128),
		MaxBytes:    maxBytes,
		LastTouched: time.Now(),
	}
}

// Append adds a new entry to the RingBuffer. Oldest entries are evicted if
// adding this entry would overflow MaxBytes.
func (r *RingBuffer) Append(entry Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entrySize := int64(len(entry.Payload))

	// Evict oldest until it fits or buffer is empty
	for r.Tail != r.Head && r.Bytes+entrySize > r.MaxBytes {
		oldest := r.Entries[r.Tail]
		r.Bytes -= int64(len(oldest.Payload))
		r.Tail = (r.Tail + 1) % len(r.Entries)
	}

	if r.Tail == r.Head {
		r.Bytes = 0
		r.EarliestSeq = 0
		r.LatestSeq = 0
	} else {
		r.EarliestSeq = r.Entries[r.Tail].Seq
	}

	// Append the new entry
	if len(r.Entries) == 0 {
		r.Entries = make([]Entry, 128)
	} else if (r.Head+1)%len(r.Entries) == r.Tail {
		// Grow the slice
		newEntries := make([]Entry, len(r.Entries)*2)
		n := 0
		t := r.Tail
		for {
			newEntries[n] = r.Entries[t]
			n++
			t = (t + 1) % len(r.Entries)
			if t == r.Head {
				break
			}
		}
		r.Entries = newEntries
		r.Tail = 0
		r.Head = n
	}

	r.Entries[r.Head] = entry
	r.LatestSeq = entry.Seq
	r.Bytes += entrySize
	if r.EarliestSeq == 0 {
		r.EarliestSeq = entry.Seq
	}
	r.Head = (r.Head + 1) % len(r.Entries)
	r.LastTouched = time.Now()
}

// evictLocked evicts oldest entries until Bytes <= MaxBytes.
func (r *RingBuffer) evictLocked() {
	for r.Tail != r.Head && r.Bytes > r.MaxBytes {
		oldest := r.Entries[r.Tail]
		r.Bytes -= int64(len(oldest.Payload))
		r.Tail = (r.Tail + 1) % len(r.Entries)
	}
	if r.Tail == r.Head {
		r.Bytes = 0
		r.EarliestSeq = 0
		r.LatestSeq = 0
	} else {
		r.EarliestSeq = r.Entries[r.Tail].Seq
	}
}

// AllEntries returns a copy of all entries in chronological order.
func (r *RingBuffer) AllEntries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.AllEntriesLocked()
}

// AllEntriesLocked returns a copy of all entries in chronological order (caller must hold lock).
func (r *RingBuffer) AllEntriesLocked() []Entry {
	if r.Bytes == 0 || len(r.Entries) == 0 {
		return nil
	}
	res := make([]Entry, 0)
	t := r.Tail
	for {
		res = append(res, r.Entries[t])
		t = (t + 1) % len(r.Entries)
		if t == r.Head {
			break
		}
	}
	return res
}

// Replay gets entries with seq > lastSeq.
// Returns:
//   - entries: matching entries to replay
//   - ok: true if the replay range is valid, false if lastSeq < earliestSeq (indicating replay_unavailable)
//   - earliestSeq: the oldest sequence in the ring
//   - latestSeq: the newest sequence in the ring
func (r *RingBuffer) Replay(lastSeq uint64) (entries []Entry, ok bool, earliestSeq uint64, latestSeq uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	earliestSeq = r.EarliestSeq
	latestSeq = r.LatestSeq

	if lastSeq == 0 {
		return r.AllEntriesLocked(), true, earliestSeq, latestSeq
	}

	if lastSeq < earliestSeq {
		return nil, false, earliestSeq, latestSeq
	}

	var res []Entry
	all := r.AllEntriesLocked()
	for _, entry := range all {
		if entry.Seq > lastSeq {
			res = append(res, entry)
		}
	}
	return res, true, earliestSeq, latestSeq
}

// LRUItem represents one session's metadata needed by BalanceGlobalLRU.
type LRUItem struct {
	SessionID   string
	Ended       bool
	LastTouched time.Time
	Ring        *RingBuffer
}

// BalanceGlobalLRU performs the Global LRU shrinking/growing policy across all active sessions.
func BalanceGlobalLRU(items []LRUItem, activeSessionID string, totalCap int64, floorCap int64) {
	var totalBytes int64
	for _, item := range items {
		item.Ring.mu.Lock()
		totalBytes += item.Ring.Bytes
		item.Ring.mu.Unlock()
	}

	// Grow active session if total cache is under 80% capacity (40MB)
	if totalBytes < int64(float64(totalCap)*0.8) {
		for _, item := range items {
			if item.SessionID == activeSessionID {
				item.Ring.mu.Lock()
				item.Ring.MaxBytes = int64(float64(item.Ring.MaxBytes) * 1.1)
				item.Ring.mu.Unlock()
				break
			}
		}
	}

	// Shrink candidate sessions by 10% step if over totalCap until totalBytes <= totalCap
	for {
		totalBytes = 0
		for _, item := range items {
			item.Ring.mu.Lock()
			totalBytes += item.Ring.Bytes
			item.Ring.mu.Unlock()
		}

		if totalBytes <= totalCap {
			break
		}

		// Find candidate session
		var bestItem *LRUItem
		for i := range items {
			item := &items[i]
			if item.Ended {
				continue
			}
			item.Ring.mu.Lock()
			hasBytesAboveFloor := item.Ring.Bytes > floorCap
			item.Ring.mu.Unlock()

			if !hasBytesAboveFloor {
				continue
			}

			if bestItem == nil || item.LastTouched.Before(bestItem.LastTouched) {
				bestItem = item
			}
		}

		if bestItem == nil {
			// No more candidates to shrink (all at or below floor)
			break
		}

		// Shrink best candidate by 10%
		bestItem.Ring.mu.Lock()
		newMax := int64(float64(bestItem.Ring.MaxBytes) * 0.9)
		if newMax < floorCap {
			newMax = floorCap
		}
		bestItem.Ring.MaxBytes = newMax

		// Evict immediately to reflect in totalBytes
		bestItem.Ring.evictLocked()
		bestItem.Ring.mu.Unlock()
	}
}
