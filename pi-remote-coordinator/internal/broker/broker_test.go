// SPDX-License-Identifier: MIT
package broker_test

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
)

func TestRingBufferBasicAppendAndGet(t *testing.T) {
	// RingBuffer with 1KB limit
	ring := broker.NewRingBuffer(1024)

	e1 := broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Ts: time.Now().Unix(), Payload: []byte("hello")}
	e2 := broker.Entry{Seq: 2, Kind: broker.EntryKindPty, Ts: time.Now().Unix(), Payload: []byte("world")}

	ring.Append(e1)
	ring.Append(e2)

	entries := ring.AllEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !bytes.Equal(entries[0].Payload, e1.Payload) {
		t.Errorf("expected %s, got %s", e1.Payload, entries[0].Payload)
	}
	if !bytes.Equal(entries[1].Payload, e2.Payload) {
		t.Errorf("expected %s, got %s", e2.Payload, entries[1].Payload)
	}
}

func TestRingBufferEviction(t *testing.T) {
	// RingBuffer with 15 bytes limit (only fits one of these at a time if overhead is 0)
	ring := broker.NewRingBuffer(15)

	e1 := broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Payload: []byte("1234567890")} // 10 bytes
	ring.Append(e1)

	if ring.Bytes != 10 {
		t.Errorf("expected 10 bytes, got %d", ring.Bytes)
	}

	e2 := broker.Entry{Seq: 2, Kind: broker.EntryKindPty, Payload: []byte("abcdef")} // 6 bytes
	ring.Append(e2)                                                                  // 10 + 6 = 16 > 15, so e1 should be evicted

	entries := ring.AllEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !bytes.Equal(entries[0].Payload, e2.Payload) {
		t.Errorf("expected %s, got %s", e2.Payload, entries[0].Payload)
	}
	if ring.Bytes != 6 {
		t.Errorf("expected 6 bytes, got %d", ring.Bytes)
	}
	if ring.EarliestSeq != 2 {
		t.Errorf("expected earliest seq 2, got %d", ring.EarliestSeq)
	}
}

func TestRingBufferGrowSlice(t *testing.T) {
	// Small ring buffer to trigger growing multiple times
	ring := broker.NewRingBuffer(1000)
	// We force the initial slice capacity to be very small
	ring.Entries = make([]broker.Entry, 4)

	for i := 1; i <= 10; i++ {
		ring.Append(broker.Entry{
			Seq:     uint64(i),
			Kind:    broker.EntryKindPty,
			Payload: []byte{byte(i)},
		})
	}

	entries := ring.AllEntries()
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}
	for i, entry := range entries {
		if entry.Seq != uint64(i+1) {
			t.Errorf("expected seq %d, got %d", i+1, entry.Seq)
		}
	}
}

func TestRingBufferReplay(t *testing.T) {
	ring := broker.NewRingBuffer(100)

	for i := 1; i <= 5; i++ {
		ring.Append(broker.Entry{
			Seq:     uint64(i),
			Kind:    broker.EntryKindPty,
			Payload: []byte{byte(i)},
		})
	}

	// 1. lastSeq = 0: stream all
	entries, ok, earliest, latest := ring.Replay(0)
	if !ok {
		t.Error("expected replay to be ok for lastSeq=0")
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
	if earliest != 1 || latest != 5 {
		t.Errorf("expected earliest=1 latest=5, got earliest=%d latest=%d", earliest, latest)
	}

	// 2. lastSeq = 2: stream 3, 4, 5
	entries, ok, _, _ = ring.Replay(2)
	if !ok {
		t.Error("expected replay to be ok for lastSeq=2")
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Seq != 3 || entries[2].Seq != 5 {
		t.Errorf("unexpected replay entry seqs: %d to %d", entries[0].Seq, entries[len(entries)-1].Seq)
	}

	// Evict some entries by appending a large one
	ring.Append(broker.Entry{
		Seq:     6,
		Kind:    broker.EntryKindPty,
		Payload: make([]byte, 100), // will evict everything else
	})

	// 3. lastSeq = 2 (which is < earliestSeq = 6): should return ok=false (replay_unavailable)
	entries, ok, earliest, latest = ring.Replay(2)
	if ok {
		t.Error("expected replay to be NOT ok for lastSeq < earliestSeq")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries on unavailable replay, got %d", len(entries))
	}
	if earliest != 6 || latest != 6 {
		t.Errorf("expected earliest=6 latest=6, got earliest=%d latest=%d", earliest, latest)
	}
}

func TestBalanceGlobalLRU(t *testing.T) {
	// Setup 3 sessions
	ring1 := broker.NewRingBuffer(100)
	ring2 := broker.NewRingBuffer(100)
	ring3 := broker.NewRingBuffer(100)

	// Add entries to session 1 and 2
	ring1.Append(broker.Entry{Seq: 1, Payload: make([]byte, 40)})
	ring2.Append(broker.Entry{Seq: 1, Payload: make([]byte, 50)})

	t1 := time.Now().Add(-10 * time.Second)
	t2 := time.Now().Add(-5 * time.Second)
	t3 := time.Now()

	items := []broker.LRUItem{
		{SessionID: "s1", Ended: false, LastTouched: t1, Ring: ring1},
		{SessionID: "s2", Ended: false, LastTouched: t2, Ring: ring2},
		{SessionID: "s3", Ended: false, LastTouched: t3, Ring: ring3},
	}

	// Total cap: 80 bytes, floor cap: 10 bytes.
	// Current totalBytes: 40 + 50 = 90 > 80.
	// Candidates above floor (10):
	// s1 (bytes=40, LastTouched=t1), s2 (bytes=50, LastTouched=t2).
	// s1 has oldest LastTouched, so s1 should be shrunk by 10%.
	// Initial maxBytes for ring1 is 100. Shrunk to 90.
	// Wait, does 90 bytes total cap cause another iteration?
	// 40 + 50 = 90 > 80, so yes!
	// Next iteration: totalBytes is still 90.
	// Candidate with oldest LastTouched above floor is still s1 (since t1 is oldest).
	// Wait! Let's check how long the loop runs.
	// If ring1 gets shrunk multiple times, eventually ring1's maxBytes shrinks enough that it evicts its entry!
	// Once ring1 evicts its 40-byte entry, its Bytes drops to 0.
	// Then totalBytes becomes 50. 50 <= 80, so the loop finishes!
	// Let's verify this behavior.
	broker.BalanceGlobalLRU(items, "s3", 80, 10)

	if ring1.Bytes != 0 {
		t.Errorf("expected ring1 to be fully evicted, got %d bytes", ring1.Bytes)
	}
	if ring2.Bytes != 50 {
		t.Errorf("expected ring2 to keep its bytes, got %d bytes", ring2.Bytes)
	}

	// Now let's test growth:
	// Total bytes is now 50. 50 is under 80% of total cap 80 (80% of 80 = 64).
	// When we balance again with active session s3, ring3's maxBytes grows
	// by 10% (100 -> 110) but is clamped at the global cap (80): a single
	// ring can never out-budget the whole broker.
	broker.BalanceGlobalLRU(items, "s3", 80, 10)
	if ring3.MaxBytes != 80 {
		t.Errorf("expected active session ring3 maxBytes to grow to the 80 cap, got %d", ring3.MaxBytes)
	}

	// With a cap that leaves headroom, growth is the plain 10% step.
	broker.BalanceGlobalLRU(items, "s3", 1000, 10)
	if ring3.MaxBytes != 88 {
		t.Errorf("expected active session ring3 maxBytes to grow to 88, got %d", ring3.MaxBytes)
	}
}

// A ring whose entries have all been evicted must preserve its seq window
// (earliest = latest+1) so stale attaches produce replay_unavailable
// instead of a silent gap.
func TestRingBufferEvictionToEmptyPreservesSeqWindow(t *testing.T) {
	ring := broker.NewRingBuffer(100)
	for i := 1; i <= 5; i++ {
		ring.Append(broker.Entry{Seq: uint64(i), Kind: broker.EntryKindPty, Payload: make([]byte, 10)})
	}

	// Shrink to zero via the global LRU (floor 0 permits full eviction).
	items := []broker.LRUItem{{SessionID: "s1", LastTouched: time.Now(), Ring: ring}}
	broker.BalanceGlobalLRU(items, "", 0, 0)

	if ring.Bytes != 0 {
		t.Fatalf("expected fully evicted ring, got %d bytes", ring.Bytes)
	}

	// Client current at the latest seq: nothing missed, clean (empty) replay.
	entries, ok, earliest, latest := ring.Replay(5)
	if !ok {
		t.Errorf("Replay(5) ok=false, want true (no gap at the window edge)")
	}
	if len(entries) != 0 {
		t.Errorf("Replay(5) returned %d entries, want 0", len(entries))
	}
	if earliest != 6 || latest != 5 {
		t.Errorf("seq window = (earliest=%d, latest=%d), want (6, 5)", earliest, latest)
	}

	// Client behind the evicted window: genuine gap.
	if _, ok, _, _ := ring.Replay(3); ok {
		t.Errorf("Replay(3) ok=true, want false (entries 4-5 were dropped)")
	}
}

// last_seq exactly one below the ring's earliest entry is NOT a gap: the
// first frame the client needs is still present. See SPEC.md § 18.4.
func TestRingBufferReplayBoundaryNoGap(t *testing.T) {
	ring := broker.NewRingBuffer(15)
	ring.Append(broker.Entry{Seq: 1, Kind: broker.EntryKindPty, Payload: make([]byte, 10)})
	ring.Append(broker.Entry{Seq: 2, Kind: broker.EntryKindPty, Payload: make([]byte, 10)}) // evicts seq 1

	entries, ok, earliest, _ := ring.Replay(1)
	if earliest != 2 {
		t.Fatalf("earliest = %d, want 2", earliest)
	}
	if !ok {
		t.Fatalf("Replay(1) ok=false, want true: seq 2 is the next needed frame and it is present")
	}
	if len(entries) != 1 || entries[0].Seq != 2 {
		t.Fatalf("Replay(1) = %v, want exactly seq 2", entries)
	}
}

// Zero-length payloads are legal entries and must not confuse the
// empty-ring bookkeeping (which is positional, not byte-based).
func TestRingBufferZeroLengthPayload(t *testing.T) {
	ring := broker.NewRingBuffer(100)
	ring.Append(broker.Entry{Seq: 1, Kind: broker.EntryKindEvent, Payload: nil})
	ring.Append(broker.Entry{Seq: 2, Kind: broker.EntryKindEvent, Payload: []byte{}})

	if got := len(ring.AllEntries()); got != 2 {
		t.Fatalf("AllEntries len = %d, want 2", got)
	}
	entries, ok, _, _ := ring.Replay(1)
	if !ok || len(entries) != 1 || entries[0].Seq != 2 {
		t.Fatalf("Replay(1) = (%v, %v), want seq 2 only", entries, ok)
	}
}

// M4 acceptance: stress with concurrent producers and consumers. One
// producer per ring (matching the one-daemon-conn-per-session model),
// concurrent Replay readers, and a concurrent LRU balancer. Every replay
// snapshot must be internally consistent: strictly ascending, contiguous,
// and bounded by the advertised window. Run under -race.
func TestRingBufferConcurrentStress(t *testing.T) {
	const (
		numRings   = 8
		numAppends = 2000
		numReaders = 4
	)

	rings := make([]*broker.RingBuffer, numRings)
	items := make([]broker.LRUItem, numRings)
	for i := range rings {
		rings[i] = broker.NewRingBuffer(4096)
		items[i] = broker.LRUItem{
			SessionID:   string(rune('a' + i)),
			LastTouched: time.Now(),
			Ring:        rings[i],
		}
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Producers: one per ring, contiguous seqs.
	for _, ring := range rings {
		wg.Add(1)
		go func(r *broker.RingBuffer) {
			defer wg.Done()
			for seq := 1; seq <= numAppends; seq++ {
				r.Append(broker.Entry{
					Seq:     uint64(seq),
					Kind:    broker.EntryKindPty,
					Payload: make([]byte, 64),
				})
			}
		}(ring)
	}

	// Balancer: races against producers, like Registry.Publish does. Not
	// part of wg — it runs until everything else is done, then is stopped.
	balancerDone := make(chan struct{})
	go func() {
		defer close(balancerDone)
		for {
			select {
			case <-stop:
				return
			default:
				broker.BalanceGlobalLRU(items, "a", 16*1024, 1024)
			}
		}
	}()

	// Readers: replay from random offsets, validating snapshot invariants.
	errCh := make(chan error, numReaders)
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				ring := rings[i%numRings]
				entries, ok, earliest, latest := ring.Replay(uint64(i % numAppends))
				if !ok {
					continue
				}
				prev := uint64(0)
				for _, e := range entries {
					if prev != 0 && e.Seq != prev+1 {
						errCh <- fmt.Errorf("non-contiguous replay: %d after %d", e.Seq, prev)
						return
					}
					if e.Seq < earliest || e.Seq > latest {
						errCh <- fmt.Errorf("seq %d outside window [%d, %d]", e.Seq, earliest, latest)
						return
					}
					prev = e.Seq
				}
			}
		}()
	}

	// Wait for producers+readers, then stop the balancer.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	var stressErr error
	select {
	case stressErr = <-errCh:
	case <-done:
	}
	close(stop)
	<-balancerDone
	if stressErr != nil {
		t.Fatal(stressErr)
	}

	// Post-conditions: every ring's window is sane and within budget slack.
	for i, ring := range rings {
		entries, ok, earliest, latest := ring.Replay(0)
		if !ok {
			t.Fatalf("ring %d: Replay(0) not ok", i)
		}
		if latest != numAppends {
			t.Errorf("ring %d: latest = %d, want %d", i, latest, numAppends)
		}
		if len(entries) > 0 {
			if entries[0].Seq != earliest || entries[len(entries)-1].Seq != latest {
				t.Errorf("ring %d: entries [%d..%d] disagree with window [%d..%d]",
					i, entries[0].Seq, entries[len(entries)-1].Seq, earliest, latest)
			}
		}
	}
}
