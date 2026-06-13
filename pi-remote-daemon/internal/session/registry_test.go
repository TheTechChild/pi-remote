// SPDX-License-Identifier: MIT
package session_test

import (
	"sync"
	"testing"
	"time"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

func newSession(id string, pid int) *session.Session {
	return &session.Session{
		SessionID:   id,
		PID:         pid,
		TmuxTarget:  "untmuxed:0.0",
		CWD:         "/tmp",
		ProjectName: "test",
		Hostname:    "host",
		Model:       "claude",
		StartedAt:   time.Unix(1730000000, 0),
		State:       session.StateRunning,
	}
}

func TestRegistry_RegisterNew_Accepted(t *testing.T) {
	r := session.NewRegistry()
	s := newSession("sess-1", 1234)

	accepted, reason := r.Register(s)

	if !accepted {
		t.Fatalf("expected accepted=true, got reason=%q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason on accept, got %q", reason)
	}
	got, ok := r.Get("sess-1")
	if !ok {
		t.Fatal("session not stored after register")
	}
	if got.PID != 1234 {
		t.Fatalf("stored pid = %d, want 1234", got.PID)
	}
}

func TestRegistry_DuplicateSessionDifferentPID_Rejected(t *testing.T) {
	r := session.NewRegistry()
	r.Register(newSession("sess-dup", 1000))

	accepted, reason := r.Register(newSession("sess-dup", 2000))

	if accepted {
		t.Fatal("expected accepted=false for different pid")
	}
	if reason != "ERR_DAEMON_DUPLICATE_SESSION_ID" {
		t.Fatalf("reason = %q, want ERR_DAEMON_DUPLICATE_SESSION_ID", reason)
	}
	got, _ := r.Get("sess-dup")
	if got.PID != 1000 {
		t.Fatalf("registry pid = %d, want unchanged 1000", got.PID)
	}
}

func TestRegistry_SameSessionSamePID_Idempotent(t *testing.T) {
	r := session.NewRegistry()
	first := newSession("sess-re", 1500)
	first.LastHeartbeat = time.Unix(100, 0)
	r.Register(first)

	again := newSession("sess-re", 1500)
	again.LastHeartbeat = time.Unix(200, 0)
	accepted, reason := r.Register(again)

	if !accepted {
		t.Fatalf("same-pid re-register should be accepted, got reason=%q", reason)
	}
	got, _ := r.Get("sess-re")
	if !got.LastHeartbeat.Equal(time.Unix(200, 0)) {
		t.Fatalf("LastHeartbeat = %v, want refreshed to 200s", got.LastHeartbeat.Unix())
	}
}

func TestRegistry_UpdateHeartbeat(t *testing.T) {
	r := session.NewRegistry()
	r.Register(newSession("sess-hb", 1))

	ts := time.Unix(1730000050, 0)
	if err := r.UpdateHeartbeat("sess-hb", ts); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	got, _ := r.Get("sess-hb")
	if !got.LastHeartbeat.Equal(ts) {
		t.Fatalf("LastHeartbeat = %v, want %v", got.LastHeartbeat, ts)
	}
}

func TestRegistry_UpdateHeartbeatUnknown_Errors(t *testing.T) {
	r := session.NewRegistry()
	if err := r.UpdateHeartbeat("missing", time.Now()); err == nil {
		t.Fatal("expected error on unknown session_id, got nil")
	}
}

func TestRegistry_Remove_DeletesEntry(t *testing.T) {
	r := session.NewRegistry()
	r.Register(newSession("sess-rm", 1))

	r.Remove("sess-rm")

	if _, ok := r.Get("sess-rm"); ok {
		t.Fatal("session still present after Remove")
	}
}

func TestRegistry_MarkEnded_KeepsEntryWithEndedState(t *testing.T) {
	r := session.NewRegistry()
	r.Register(newSession("sess-end", 1))

	r.MarkEnded("sess-end")

	got, ok := r.Get("sess-end")
	if !ok {
		t.Fatal("MarkEnded should retain entry, but it was removed")
	}
	if got.State != session.StateEnded {
		t.Fatalf("state = %q, want %q", got.State, session.StateEnded)
	}
}

// Concurrent register + heartbeat under -race exercises the registry's
// internal locking. Run with `go test -race ./...`.
func TestRegistry_Concurrent_RaceSafe(t *testing.T) {
	r := session.NewRegistry()
	const n = 50
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		id := "sess-c-" + string(rune('a'+i%26))
		wg.Add(2)
		go func(id string) {
			defer wg.Done()
			r.Register(newSession(id, 1))
		}(id)
		go func(id string) {
			defer wg.Done()
			_ = r.UpdateHeartbeat(id, time.Now())
		}(id)
	}
	wg.Wait()
}

// Issue #42: heartbeat-timeout sweep flips silent sessions to
// unresponsive exactly once; a fresh heartbeat recovers them.
func TestRegistry_SweepHeartbeats(t *testing.T) {
	reg := session.NewRegistry()
	var transitions []string
	reg.OnStateChange(func(id string, from, to session.SessionState) {
		transitions = append(transitions, id+":"+string(from)+"->"+string(to))
	})

	if !mustRegister(t, reg, newSession("sess-1", 1)) {
		t.Fatal("register failed")
	}
	base := time.Now()
	if err := reg.UpdateHeartbeat("sess-1", base); err != nil {
		t.Fatal(err)
	}

	// Within the window: no flip.
	if got := reg.SweepHeartbeats(base.Add(20*time.Second), 30*time.Second); len(got) != 0 {
		t.Fatalf("early sweep flipped %v", got)
	}

	// Past the window: flips once, idempotent on the second sweep.
	if got := reg.SweepHeartbeats(base.Add(31*time.Second), 30*time.Second); len(got) != 1 || got[0] != "sess-1" {
		t.Fatalf("sweep = %v, want [sess-1]", got)
	}
	if got := reg.SweepHeartbeats(base.Add(40*time.Second), 30*time.Second); len(got) != 0 {
		t.Fatalf("second sweep not idempotent: %v", got)
	}
	if len(transitions) != 1 || transitions[0] != "sess-1:running->unresponsive" {
		t.Fatalf("transitions = %v", transitions)
	}

	// Recovery: a heartbeat flips it back and announces it.
	if err := reg.UpdateHeartbeat("sess-1", base.Add(45*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 || transitions[1] != "sess-1:unresponsive->running" {
		t.Fatalf("recovery transitions = %v", transitions)
	}
}

// Issue #43: the reaper drops only sufficiently-old ended entries.
func TestRegistry_ReapEnded(t *testing.T) {
	reg := session.NewRegistry()
	for _, id := range []string{"sess-old", "sess-new", "sess-live"} {
		if !mustRegister(t, reg, newSession(id, 1)) {
			t.Fatal("register failed")
		}
	}
	reg.MarkEnded("sess-old")
	reg.MarkEnded("sess-new")

	if n := reg.ReapEnded(time.Now().Add(30*time.Minute), time.Hour); n != 0 {
		t.Fatalf("early reap removed %d", n)
	}
	if n := reg.ReapEnded(time.Now().Add(2*time.Hour), time.Hour); n != 2 {
		t.Fatalf("reap removed %d, want 2", n)
	}
	if _, ok := reg.Get("sess-live"); !ok {
		t.Fatal("live session reaped")
	}
	if _, ok := reg.Get("sess-old"); ok {
		t.Fatal("ended session should be reaped")
	}
}

// #42 follow-up: a brand-new registration must get a heartbeat grace
// period — the sweep should not flip a session that registered moments
// ago and simply hasn't heartbeated yet.
func TestRegistry_SweepGivesNewSessionsGracePeriod(t *testing.T) {
	reg := session.NewRegistry()
	if !mustRegister(t, reg, newSession("sess-fresh", 1)) {
		t.Fatal("register failed")
	}
	if got := reg.SweepHeartbeats(time.Now(), 30*time.Second); len(got) != 0 {
		t.Fatalf("sweep flipped a just-registered session: %v", got)
	}
	// But it is still swept once genuinely silent past the timeout.
	if got := reg.SweepHeartbeats(time.Now().Add(31*time.Second), 30*time.Second); len(got) != 1 {
		t.Fatalf("sweep = %v, want [sess-fresh] after real silence", got)
	}
}
