// SPDX-License-Identifier: MIT
package sessions

import (
	"sync"
	"sync/atomic"
	"testing"

	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

func newRegistry() *Registry { return NewRegistry() }

func meta() daemon_coordinator.SessionStartedJsonMetadata {
	return daemon_coordinator.SessionStartedJsonMetadata{
		Hostname:    "macbook-pro",
		Cwd:         "/home/clayton/code",
		Model:       "claude-3-opus",
		ProjectName: "pi-remote",
	}
}

// C-13: Register returns LastSeq=0, State="running", Ended=false.
func TestRegister_Defaults(t *testing.T) {
	r := newRegistry()
	r.Register("sess-1", "machine-a", meta())
	s, ok := r.Get("sess-1")
	if !ok {
		t.Fatal("Get(sess-1) ok=false")
	}
	if s.LastSeq != 0 {
		t.Errorf("LastSeq = %d, want 0", s.LastSeq)
	}
	if s.State != "running" {
		t.Errorf("State = %q, want %q", s.State, "running")
	}
	if s.Ended {
		t.Errorf("Ended = true, want false")
	}
	if s.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.MachineID != "machine-a" {
		t.Errorf("MachineID = %q", s.MachineID)
	}
}

// C-14: Get unknown → nil, false.
func TestGet_Unknown(t *testing.T) {
	r := newRegistry()
	s, ok := r.Get("nope")
	if ok {
		t.Errorf("ok = true, want false")
	}
	if s != nil {
		t.Errorf("session = %+v, want nil", s)
	}
}

// C-15: AdvanceSeq monotonic.
func TestAdvanceSeq_Monotonic(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.AdvanceSeq("s", 1)
	r.AdvanceSeq("s", 2)
	r.AdvanceSeq("s", 5)
	s, _ := r.Get("s")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", s.LastSeq)
	}
}

// C-16: AdvanceSeq smaller value does not regress LastSeq.
func TestAdvanceSeq_NoRegressOnSmaller(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.AdvanceSeq("s", 5)
	r.AdvanceSeq("s", 3)
	s, _ := r.Get("s")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", s.LastSeq)
	}
}

// C-17: AdvanceSeq equal value does not regress (and does not double-count).
func TestAdvanceSeq_EqualNoOp(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.AdvanceSeq("s", 5)
	r.AdvanceSeq("s", 5)
	s, _ := r.Get("s")
	if s.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", s.LastSeq)
	}
}

// C-18: SetState updates State.
func TestSetState(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.SetState("s", "idle")
	s, _ := r.Get("s")
	if s.State != "idle" {
		t.Errorf("State = %q, want %q", s.State, "idle")
	}
}

// C-19: MarkEnded sets Ended=true, retained on Get.
func TestMarkEnded_Retained(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.MarkEnded("s")
	s, ok := r.Get("s")
	if !ok {
		t.Fatal("session not retained after MarkEnded")
	}
	if !s.Ended {
		t.Errorf("Ended = false, want true")
	}
}

// C-20: PauseAllForMachine flips only that machine's sessions to "paused".
func TestPauseAllForMachine(t *testing.T) {
	r := newRegistry()
	r.Register("a1", "machine-a", meta())
	r.Register("a2", "machine-a", meta())
	r.Register("b1", "machine-b", meta())
	r.PauseAllForMachine("machine-a")

	a1, _ := r.Get("a1")
	a2, _ := r.Get("a2")
	b1, _ := r.Get("b1")
	if a1.State != "paused" {
		t.Errorf("a1 State = %q, want paused", a1.State)
	}
	if a2.State != "paused" {
		t.Errorf("a2 State = %q, want paused", a2.State)
	}
	if b1.State != "running" {
		t.Errorf("b1 State = %q, want running (untouched)", b1.State)
	}
}

// Ended sessions should NOT be flipped back to paused — they stay ended.
func TestPauseAllForMachine_SkipsEnded(t *testing.T) {
	r := newRegistry()
	r.Register("a1", "machine-a", meta())
	r.MarkEnded("a1")
	r.PauseAllForMachine("machine-a")
	s, _ := r.Get("a1")
	if !s.Ended {
		t.Errorf("Ended = false, want true")
	}
	if s.State == "paused" {
		t.Errorf("State = paused; ended sessions should retain prior state, got %q", s.State)
	}
}

// C-21: RestoreLastSeq = max(LastSeq, n).
func TestRestoreLastSeq_NeverRegresses(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	r.AdvanceSeq("s", 10)
	r.RestoreLastSeq("s", 5)
	s, _ := r.Get("s")
	if s.LastSeq != 10 {
		t.Errorf("LastSeq = %d, want 10", s.LastSeq)
	}
	r.RestoreLastSeq("s", 20)
	s, _ = r.Get("s")
	if s.LastSeq != 20 {
		t.Errorf("LastSeq = %d, want 20", s.LastSeq)
	}
}

// C-22: Concurrent AdvanceSeq under -race; final = max, no race.
func TestAdvanceSeq_ConcurrentMax(t *testing.T) {
	r := newRegistry()
	r.Register("s", "m", meta())
	const N = 200
	var wg sync.WaitGroup
	var maxSeq atomic.Int64
	for i := 1; i <= N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.AdvanceSeq("s", i)
			for {
				cur := maxSeq.Load()
				if int64(i) <= cur {
					break
				}
				if maxSeq.CompareAndSwap(cur, int64(i)) {
					break
				}
			}
		}()
	}
	wg.Wait()
	s, _ := r.Get("s")
	if s.LastSeq != int(maxSeq.Load()) {
		t.Errorf("LastSeq = %d, want %d", s.LastSeq, maxSeq.Load())
	}
	if s.LastSeq != N {
		t.Errorf("LastSeq = %d, want %d", s.LastSeq, N)
	}
}
