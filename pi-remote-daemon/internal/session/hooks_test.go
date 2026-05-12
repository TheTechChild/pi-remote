// SPDX-License-Identifier: MIT
package session_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// R1: TestRegistry_OnRegister_FiresOnAccept — callback invoked exactly
// once with the session that was registered.
func TestRegistry_OnRegister_FiresOnAccept(t *testing.T) {
	r := session.NewRegistry()

	var got *session.Session
	var calls atomic.Int32
	r.OnRegister(func(s *session.Session) {
		calls.Add(1)
		got = s
	})

	s := newSession("sess-1", 1234)
	accepted, _ := r.Register(s)
	require.True(t, accepted)

	require.EqualValues(t, 1, calls.Load(), "OnRegister must fire exactly once on accept")
	require.NotNil(t, got)
	require.Equal(t, "sess-1", got.SessionID)
}

// R2: TestRegistry_OnRegister_DoesNotFireOnReject — duplicate-pid
// rejection short-circuits before the callback.
func TestRegistry_OnRegister_DoesNotFireOnReject(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1000)))

	var calls atomic.Int32
	r.OnRegister(func(s *session.Session) { calls.Add(1) })

	accepted, _ := r.Register(newSession("sess-1", 2000)) // different pid -> reject
	require.False(t, accepted)
	require.EqualValues(t, 0, calls.Load(), "OnRegister must not fire on reject")
}

// R3: TestRegistry_OnRegister_DoesNotFireOnReRegister — idempotent
// re-register (same id+pid) is a refresh, not a new session.
func TestRegistry_OnRegister_DoesNotFireOnReRegister(t *testing.T) {
	r := session.NewRegistry()
	var calls atomic.Int32
	r.OnRegister(func(s *session.Session) { calls.Add(1) })

	first := newSession("sess-1", 1000)
	require.True(t, mustRegister(t, r, first))
	require.EqualValues(t, 1, calls.Load(), "initial register fires")

	again := newSession("sess-1", 1000) // same id, same pid
	require.True(t, mustRegister(t, r, again))
	require.EqualValues(t, 1, calls.Load(), "re-register must NOT fire OnRegister again")
}

// R4: TestRegistry_OnHeartbeat_Fires — UpdateHeartbeat invokes the hook.
func TestRegistry_OnHeartbeat_Fires(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var (
		gotID atomic.Pointer[string]
		gotTS atomic.Pointer[time.Time]
	)
	r.OnHeartbeat(func(id string, ts time.Time) {
		gotID.Store(&id)
		gotTS.Store(&ts)
	})

	ts := time.Unix(1730000050, 0)
	require.NoError(t, r.UpdateHeartbeat("sess-1", ts))

	gotIDVal := gotID.Load()
	gotTSVal := gotTS.Load()
	require.NotNil(t, gotIDVal, "OnHeartbeat must fire")
	require.Equal(t, "sess-1", *gotIDVal)
	require.True(t, gotTSVal.Equal(ts))
}

// R5: TestRegistry_OnEnded_FiresOnRemoveWithReason — kind=EndedExplicit,
// reason propagated from RemoveWithReason.
func TestRegistry_OnEnded_FiresOnRemoveWithReason(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var (
		gotSess   atomic.Pointer[session.Session]
		gotKind   atomic.Int32
		gotReason atomic.Pointer[string]
		calls     atomic.Int32
	)
	r.OnEnded(func(s *session.Session, k session.EndedKind, reason string) {
		calls.Add(1)
		gotSess.Store(s)
		gotKind.Store(int32(k))
		gotReason.Store(&reason)
	})

	r.RemoveWithReason("sess-1", "session_shutdown")
	require.EqualValues(t, 1, calls.Load(), "OnEnded must fire once")
	require.NotNil(t, gotSess.Load())
	require.Equal(t, "sess-1", gotSess.Load().SessionID)
	require.EqualValues(t, session.EndedExplicit, gotKind.Load(), "kind must be EndedExplicit")
	require.Equal(t, "session_shutdown", *gotReason.Load(), "reason propagates verbatim")
}

// R6: TestRegistry_OnEnded_FiresOnMarkEnded — kind=EndedImplicit, reason
// empty (multiplex picks the upstream reason from the kind).
func TestRegistry_OnEnded_FiresOnMarkEnded(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var (
		gotKind   atomic.Int32
		gotReason atomic.Pointer[string]
		calls     atomic.Int32
	)
	r.OnEnded(func(s *session.Session, k session.EndedKind, reason string) {
		calls.Add(1)
		gotKind.Store(int32(k))
		gotReason.Store(&reason)
	})

	r.MarkEnded("sess-1")
	require.EqualValues(t, 1, calls.Load(), "OnEnded must fire once")
	require.EqualValues(t, session.EndedImplicit, gotKind.Load(), "kind must be EndedImplicit")
	require.Equal(t, "", *gotReason.Load(), "reason empty for implicit end")
}

// R7: TestRegistry_OnEnded_FiresOnce — Remove then MarkEnded (or vice
// versa) on the same id fires OnEnded exactly once.
func TestRegistry_OnEnded_FiresOnce(t *testing.T) {
	t.Run("RemoveWithReason then MarkEnded", func(t *testing.T) {
		r := session.NewRegistry()
		require.True(t, mustRegister(t, r, newSession("sess-1", 1)))
		var calls atomic.Int32
		r.OnEnded(func(_ *session.Session, _ session.EndedKind, _ string) { calls.Add(1) })
		r.RemoveWithReason("sess-1", "session_shutdown")
		r.MarkEnded("sess-1")
		require.EqualValues(t, 1, calls.Load(), "second end-call must be a no-op")
	})

	t.Run("MarkEnded then RemoveWithReason", func(t *testing.T) {
		r := session.NewRegistry()
		require.True(t, mustRegister(t, r, newSession("sess-2", 1)))
		var calls atomic.Int32
		r.OnEnded(func(_ *session.Session, _ session.EndedKind, _ string) { calls.Add(1) })
		r.MarkEnded("sess-2")
		r.RemoveWithReason("sess-2", "session_shutdown")
		require.EqualValues(t, 1, calls.Load(), "second end-call must be a no-op")
	})
}

// R8: TestRegistry_OnEvent_FiresWithBytes — Event method fans bytes out
// verbatim. No parsing in the registry.
func TestRegistry_OnEvent_FiresWithBytes(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var (
		gotID    atomic.Pointer[string]
		gotBytes atomic.Pointer[[]byte]
	)
	r.OnEvent(func(id string, b []byte) {
		gotID.Store(&id)
		bcopy := append([]byte(nil), b...)
		gotBytes.Store(&bcopy)
	})

	payload := []byte(`{"type":"event","kind":"agent_start","data":{}}`)
	r.Event("sess-1", payload)

	require.NotNil(t, gotID.Load(), "OnEvent must fire")
	require.Equal(t, "sess-1", *gotID.Load())
	require.Equal(t, payload, *gotBytes.Load(), "bytes pass through verbatim")
}

// R9: TestRegistry_OnStateChange_HookExistsButUnfired — the hook is
// plumbed for #42 but no M3+M4 codepath fires it.
func TestRegistry_OnStateChange_HookExistsButUnfired(t *testing.T) {
	r := session.NewRegistry()
	var calls atomic.Int32
	r.OnStateChange(func(_ string, _, _ session.SessionState) { calls.Add(1) })

	s := newSession("sess-1", 1)
	require.True(t, mustRegister(t, r, s))
	require.NoError(t, r.UpdateHeartbeat("sess-1", time.Now()))
	r.Event("sess-1", []byte(`{}`))
	r.MarkEnded("sess-1")     // flips State to ended, but the hook is
	r.RemoveWithReason("sess-1", "x") // for explicit transitions tracked
	                                  // by #42, which doesn't exist yet.

	require.EqualValues(t, 0, calls.Load(), "no M3+M4 codepath should fire OnStateChange")
}

// R10: TestRegistry_NilCallbacks_AreSafe — methods don't panic when no
// callback is registered.
func TestRegistry_NilCallbacks_AreSafe(t *testing.T) {
	r := session.NewRegistry()
	require.NotPanics(t, func() {
		require.True(t, mustRegister(t, r, newSession("sess-1", 1)))
		_ = r.UpdateHeartbeat("sess-1", time.Now())
		r.Event("sess-1", []byte(`{}`))
		r.MarkEnded("sess-1")
		r.RemoveWithReason("sess-1", "")
	})
}

// R11: TestRegistry_CallbackPanic_DoesNotPropagate — one buggy hook must
// not wedge the daemon. The registry recovers, logs, and proceeds.
func TestRegistry_CallbackPanic_DoesNotPropagate(t *testing.T) {
	r := session.NewRegistry()
	r.OnRegister(func(_ *session.Session) { panic("boom from OnRegister") })
	r.OnHeartbeat(func(_ string, _ time.Time) { panic("boom from OnHeartbeat") })
	r.OnEvent(func(_ string, _ []byte) { panic("boom from OnEvent") })
	r.OnEnded(func(_ *session.Session, _ session.EndedKind, _ string) { panic("boom from OnEnded") })

	require.NotPanics(t, func() {
		require.True(t, mustRegister(t, r, newSession("sess-1", 1)))
		_ = r.UpdateHeartbeat("sess-1", time.Now())
		r.Event("sess-1", []byte(`{}`))
		r.MarkEnded("sess-1")
	})

	// After all those panics, the registry should still be functional.
	require.True(t, mustRegister(t, r, newSession("sess-2", 1)))
}

// R12: TestSession_EndedAt_SetByRemoveWithReason — EndedAt is stamped
// before OnEnded fires, so callbacks can observe it.
func TestSession_EndedAt_SetByRemoveWithReason(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var gotEndedAt atomic.Pointer[time.Time]
	r.OnEnded(func(s *session.Session, _ session.EndedKind, _ string) {
		t := s.EndedAt
		gotEndedAt.Store(&t)
	})

	before := time.Now()
	r.RemoveWithReason("sess-1", "session_shutdown")
	after := time.Now()

	got := gotEndedAt.Load()
	require.NotNil(t, got)
	require.False(t, got.IsZero(), "EndedAt must be set")
	require.False(t, got.Before(before), "EndedAt must be >= test start time")
	require.False(t, got.After(after), "EndedAt must be <= test end time")
}

// R13: TestSession_EndedAt_SetByMarkEnded — same for the implicit path.
func TestSession_EndedAt_SetByMarkEnded(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	var gotEndedAt atomic.Pointer[time.Time]
	r.OnEnded(func(s *session.Session, _ session.EndedKind, _ string) {
		t := s.EndedAt
		gotEndedAt.Store(&t)
	})

	before := time.Now()
	r.MarkEnded("sess-1")
	after := time.Now()

	got := gotEndedAt.Load()
	require.NotNil(t, got)
	require.False(t, got.IsZero(), "EndedAt must be set")
	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

// R14: TestSession_EndedAt_VisibleAfterMarkEnded — operators / future
// reaper can Get a still-present (ended) entry and observe its EndedAt.
func TestSession_EndedAt_VisibleAfterMarkEnded(t *testing.T) {
	r := session.NewRegistry()
	require.True(t, mustRegister(t, r, newSession("sess-1", 1)))

	r.MarkEnded("sess-1")
	got, ok := r.Get("sess-1")
	require.True(t, ok, "MarkEnded retains the entry")
	require.False(t, got.EndedAt.IsZero(), "Get-returned session has EndedAt set")
}

// mustRegister is a test helper that asserts Register returns accepted=true.
// Defined here (not in registry_test.go) so the hooks tests are
// self-contained.
func mustRegister(t *testing.T, r *session.Registry, s *session.Session) bool {
	t.Helper()
	accepted, reason := r.Register(s)
	if !accepted {
		t.Fatalf("Register(%q) rejected: %s", s.SessionID, reason)
	}
	return accepted
}

