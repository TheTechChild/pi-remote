// SPDX-License-Identifier: MIT
package sessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
)

// collectConn is a ClientConn that records every payload synchronously.
// Unlike the production attachedClient (buffered channel, drop-on-full),
// it never drops, so tests can assert exact delivery sets.
type collectConn struct {
	id string

	mu     sync.Mutex
	frames [][]byte
}

func (c *collectConn) ID() string { return c.id }

func (c *collectConn) Send(msg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	c.frames = append(c.frames, cp)
}

func (c *collectConn) collected() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.frames))
	copy(out, c.frames)
	return out
}

// seqPayload encodes a seq as a payload so tests can recover it.
func seqPayload(seq uint64) []byte {
	return []byte(fmt.Sprintf("seq:%d", seq))
}

func payloadSeq(t *testing.T, payload []byte) uint64 {
	t.Helper()
	var seq uint64
	if _, err := fmt.Sscanf(string(payload), "seq:%d", &seq); err != nil {
		t.Fatalf("unparseable payload %q: %v", payload, err)
	}
	return seq
}

// Regression test for the attach race: a client that attaches while the
// session is actively publishing must receive every frame exactly once
// (replay ∪ live, no gap at the handover, no duplicates). Before
// AttachWithReplay, frames published between Ring.Replay and Attach were
// silently lost.
func TestAttachWithReplayNoGapNoDup(t *testing.T) {
	const (
		numFrames    = 2000
		numAttachers = 8
	)

	r := NewRegistry()
	s := r.Register("sess-1", "machine-a", meta())
	// Plenty of ring budget: nothing should be evicted in this test.
	s.Ring.MaxBytes = 64 * 1024 * 1024

	var wg sync.WaitGroup

	// Producer: contiguous seqs through the full Publish path (ring +
	// fan-out + LRU balance).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for seq := uint64(1); seq <= numFrames; seq++ {
			r.Publish("sess-1", broker.Entry{
				Seq:     seq,
				Kind:    broker.EntryKindPty,
				Payload: seqPayload(seq),
			})
		}
	}()

	// Attachers: join at staggered points mid-stream with lastSeq=0.
	conns := make([]*collectConn, numAttachers)
	for i := 0; i < numAttachers; i++ {
		conns[i] = &collectConn{id: fmt.Sprintf("conn-%d", i)}
		wg.Add(1)
		go func(c *collectConn) {
			defer wg.Done()
			s.AttachWithReplay(c, 0, func(_, _ uint64) []byte {
				t.Error("unexpected replay_unavailable: ring should never evict in this test")
				return []byte("unavailable")
			})
		}(conns[i])
	}

	wg.Wait()

	for _, c := range conns {
		seen := make(map[uint64]bool)
		var minSeq, maxSeq uint64
		for _, f := range c.collected() {
			seq := payloadSeq(t, f)
			if seen[seq] {
				t.Fatalf("%s: seq %d delivered twice", c.id, seq)
			}
			seen[seq] = true
			if minSeq == 0 || seq < minSeq {
				minSeq = seq
			}
			if seq > maxSeq {
				maxSeq = seq
			}
		}
		// Exactly once: the full range from the attach point (here 1,
		// since lastSeq=0 replays everything) to the final frame, with
		// no holes.
		if minSeq != 1 {
			t.Errorf("%s: first delivered seq = %d, want 1", c.id, minSeq)
		}
		if maxSeq != numFrames {
			t.Errorf("%s: last delivered seq = %d, want %d", c.id, maxSeq, numFrames)
		}
		if len(seen) != numFrames {
			t.Errorf("%s: delivered %d distinct seqs, want %d (gap at the replay/live handover?)",
				c.id, len(seen), numFrames)
		}
	}
}

// AttachWithReplay must emit replay_unavailable (and nothing else) when the
// requested resume point has been evicted, then deliver live frames.
func TestAttachWithReplayUnavailable(t *testing.T) {
	r := NewRegistry()
	s := r.Register("sess-1", "machine-a", meta())
	s.Ring.MaxBytes = 8 // every append evicts its predecessor

	for seq := uint64(1); seq <= 3; seq++ {
		r.Publish("sess-1", broker.Entry{Seq: seq, Kind: broker.EntryKindPty, Payload: seqPayload(seq)})
	}

	c := &collectConn{id: "conn-1"}
	_, ok := s.AttachWithReplay(c, 1, func(earliest, latest uint64) []byte {
		return []byte(fmt.Sprintf("unavailable:%d:%d", earliest, latest))
	})
	if ok {
		t.Fatal("ok = true, want false: seq 2 was evicted")
	}

	frames := c.collected()
	if len(frames) != 1 || string(frames[0]) != "unavailable:3:3" {
		t.Fatalf("frames = %q, want exactly [unavailable:3:3]", frames)
	}

	// Live frames flow after the banner.
	r.Publish("sess-1", broker.Entry{Seq: 4, Kind: broker.EntryKindPty, Payload: seqPayload(4)})
	frames = c.collected()
	if len(frames) != 2 || payloadSeq(t, frames[1]) != 4 {
		t.Fatalf("after live publish frames = %q, want banner + seq 4", frames)
	}
}

// PauseAllForMachine must broadcast a session_state_change (to=paused) to
// clients attached to that machine's sessions — and only those.
func TestPauseAllForMachineBroadcasts(t *testing.T) {
	r := NewRegistry()
	sa := r.Register("sess-a", "machine-a", meta())
	sb := r.Register("sess-b", "machine-b", meta())

	ca := &collectConn{id: "conn-a"}
	cb := &collectConn{id: "conn-b"}
	sa.Attach(ca)
	sb.Attach(cb)

	r.PauseAllForMachine("machine-a")

	aFrames := ca.collected()
	if len(aFrames) != 1 {
		t.Fatalf("machine-a client got %d frames, want 1", len(aFrames))
	}
	var frame struct {
		Type      string `json:"type"`
		V         int    `json:"v"`
		SessionID string `json:"session_id"`
		MachineID string `json:"machine_id"`
		From      string `json:"from"`
		To        string `json:"to"`
	}
	if err := json.Unmarshal(aFrames[0], &frame); err != nil {
		t.Fatalf("unmarshal broadcast frame: %v", err)
	}
	if frame.Type != "session_state_change" || frame.To != "paused" ||
		frame.From != "running" || frame.MachineID != "machine-a" ||
		frame.SessionID != "sess-a" || frame.V != 1 {
		t.Errorf("unexpected frame: %s", aFrames[0])
	}
	if strings.Contains(string(aFrames[0]), `"seq"`) {
		t.Errorf("coordinator-app session_state_change must not carry seq: %s", aFrames[0])
	}

	if got := len(cb.collected()); got != 0 {
		t.Errorf("machine-b client got %d frames, want 0", got)
	}

	// Idempotent: a second pause must not re-broadcast.
	r.PauseAllForMachine("machine-a")
	if got := len(ca.collected()); got != 1 {
		t.Errorf("after second pause machine-a client has %d frames, want still 1", got)
	}
}

// M3 acceptance: registry operations are concurrent-safe under -race.
// No assertions beyond invariant sanity — the value is the race detector
// sweeping every registry method while sessions publish and clients attach.
func TestRegistryConcurrentOps(t *testing.T) {
	const (
		numSessions  = 16
		opsPerWorker = 300
	)

	r := NewRegistry()
	for i := 0; i < numSessions; i++ {
		r.Register(fmt.Sprintf("sess-%d", i), "machine-a", meta())
	}

	var wg sync.WaitGroup
	worker := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				fn(i)
			}
		}()
	}

	worker(func(i int) {
		r.Register(fmt.Sprintf("sess-%d", i%numSessions), "machine-b", meta())
	})
	worker(func(i int) {
		r.Publish(fmt.Sprintf("sess-%d", i%numSessions), broker.Entry{
			Seq: uint64(i + 1), Kind: broker.EntryKindPty, Payload: seqPayload(uint64(i + 1)),
		})
	})
	worker(func(i int) {
		r.AdvanceSeq(fmt.Sprintf("sess-%d", i%numSessions), i)
	})
	worker(func(i int) {
		r.SetState(fmt.Sprintf("sess-%d", i%numSessions), "idle")
	})
	worker(func(i int) {
		if s, ok := r.Get(fmt.Sprintf("sess-%d", i%numSessions)); ok {
			c := &collectConn{id: fmt.Sprintf("conn-%d", i)}
			s.AttachWithReplay(c, 0, func(_, _ uint64) []byte { return []byte("unavailable") })
			s.Detach(c.id)
		}
	})
	worker(func(i int) {
		_ = r.List()
		_ = r.LRUItems()
	})
	worker(func(i int) {
		if i%50 == 0 {
			r.PauseAllForMachine("machine-a")
		}
		r.MarkEnded(fmt.Sprintf("sess-%d", numSessions+i)) // mostly unknown ids: exercises the miss path
	})

	wg.Wait()

	if got := len(r.List()); got != numSessions {
		t.Errorf("List() len = %d, want %d", got, numSessions)
	}
}
