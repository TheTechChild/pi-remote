// SPDX-License-Identifier: MIT
package session_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// S1: TestSeqAllocator_StartsAtOne — Next() on a fresh allocator returns 1.
// Per SPEC § 18.2 ("seq counter starts at 1") and the plan's "Per-session
// SeqAllocator, starts at 1".
func TestSeqAllocator_StartsAtOne(t *testing.T) {
	var a session.SeqAllocator
	require.Equal(t, uint64(1), a.Next(), "first Next() must return 1")
}

// S2: TestSeqAllocator_Monotonic — 10 sequential calls return 1..10.
func TestSeqAllocator_Monotonic(t *testing.T) {
	var a session.SeqAllocator
	for i := uint64(1); i <= 10; i++ {
		require.Equal(t, i, a.Next(), "call %d", i)
	}
}

// S3: TestSeqAllocator_PeekDoesNotAdvance — Peek returns last allocated;
// does not consume.
func TestSeqAllocator_PeekDoesNotAdvance(t *testing.T) {
	var a session.SeqAllocator

	require.Equal(t, uint64(0), a.Peek(), "Peek before any Next() must return 0")

	for i := 0; i < 3; i++ {
		a.Next()
	}
	require.Equal(t, uint64(3), a.Peek(), "Peek after 3 Next() must return 3")
	require.Equal(t, uint64(3), a.Peek(), "Peek must not advance on repeated call")
	require.Equal(t, uint64(4), a.Next(), "Next() after Peek returns 4")
	require.Equal(t, uint64(4), a.Peek(), "Peek after Next() reflects new value")
}

// S4: TestSeqAllocator_ConcurrentNext_MonotonicAndUnique — 100 goroutines x
// 100 Next() calls produce exactly {1..10000} with no dupes or gaps. Must
// pass under -race. Per #11 acceptance "Concurrent-safe under -race".
func TestSeqAllocator_ConcurrentNext_MonotonicAndUnique(t *testing.T) {
	const goroutines = 100
	const perGoroutine = 100
	const total = goroutines * perGoroutine

	var a session.SeqAllocator
	results := make(chan uint64, total)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				results <- a.Next()
			}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[uint64]bool, total)
	for v := range results {
		require.False(t, seen[v], "duplicate seq %d", v)
		seen[v] = true
	}
	require.Len(t, seen, total, "expected %d unique seqs", total)
	for i := uint64(1); i <= total; i++ {
		require.True(t, seen[i], "missing seq %d", i)
	}
}

// S5: TestSeqAllocator_ZeroValueUsable — `var a SeqAllocator` works
// without a constructor. Allows embedding in Session without a NewSession
// ceremony.
func TestSeqAllocator_ZeroValueUsable(t *testing.T) {
	var a session.SeqAllocator
	require.NotPanics(t, func() {
		_ = a.Next()
		_ = a.Peek()
	})
}
