// SPDX-License-Identifier: MIT
package session

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrUnknownSession is returned by registry mutators when the named session
// is not registered.
var ErrUnknownSession = errors.New("session: unknown session_id")

// ErrCodeDuplicateSessionID is the rejection reason returned in register_ack
// when a session_id is already known to the daemon for a different pid.
// See pi-remote-spec/errors/codes.md.
const ErrCodeDuplicateSessionID = "ERR_DAEMON_DUPLICATE_SESSION_ID"

// EndedKind tags the OnEnded callback so consumers (the multiplex) can
// pick the upstream reason without inspecting state.
type EndedKind int

const (
	// EndedExplicit means the extension sent a `disconnect` frame; the
	// extension-side reason (session_shutdown / client_request / error)
	// is forwarded as the `reason` argument to the callback.
	EndedExplicit EndedKind = iota
	// EndedImplicit means the extension's socket closed without a
	// disconnect frame. The callback `reason` is empty; the multiplex
	// emits upstream `session_ended { reason: "process_exit" }`.
	EndedImplicit
)

// nowFn is the clock the registry uses to stamp EndedAt. Variable so
// tests could replace it; production stays on time.Now.
var nowFn = time.Now

// Registry is the daemon's in-memory map of active and recently-ended Pi
// sessions. Concurrent access is guarded by a single RWMutex; per SPEC § 7.5
// the workload is a small map with low contention, so channels are deferred
// until M3 (coordinator multiplex) needs goroutine fan-out.
//
// M3+M4 add callback hooks that fire after the registry's mutex is
// released, so callbacks may safely re-enter the registry (e.g., to read
// other sessions for fan-out). Callbacks are invoked synchronously from
// the mutating goroutine; if a callback panics, the registry recovers and
// logs, then returns to the caller as if the mutation succeeded.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ended    map[string]bool // tracks "OnEnded fired" so the second
	// terminal call is a no-op even after the
	// entry has been Removed.

	hookMu        sync.RWMutex
	onRegister    func(s *Session)
	onHeartbeat   func(id string, ts time.Time)
	onEnded       func(s *Session, kind EndedKind, reason string)
	onEvent       func(id string, eventBytes []byte)
	onStateChange func(id string, from, to SessionState)
}

func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[string]*Session),
		ended:    make(map[string]bool),
	}
}

// OnRegister installs a callback fired after a successful Register that
// adds a new session. Idempotent re-registrations (same id, same pid) do
// NOT fire the callback again; only the initial registration does. The
// multiplex uses this to emit `session_started`.
func (r *Registry) OnRegister(fn func(s *Session)) {
	r.hookMu.Lock()
	r.onRegister = fn
	r.hookMu.Unlock()
}

// OnHeartbeat installs a callback fired after a successful
// UpdateHeartbeat. The multiplex does NOT use this on the upstream wire
// (the coordinator infers liveness from frame cadence + WebSocket ping);
// the hook exists so future heartbeat-timeout work (#42) has a place to
// plug in.
func (r *Registry) OnHeartbeat(fn func(id string, ts time.Time)) {
	r.hookMu.Lock()
	r.onHeartbeat = fn
	r.hookMu.Unlock()
}

// OnEnded installs a callback fired exactly once per session ending,
// from either RemoveWithReason (kind=EndedExplicit) or MarkEnded
// (kind=EndedImplicit). Subsequent calls on the same id are no-ops.
func (r *Registry) OnEnded(fn func(s *Session, kind EndedKind, reason string)) {
	r.hookMu.Lock()
	r.onEnded = fn
	r.hookMu.Unlock()
}

// OnEvent installs a callback fired by Event. The bytes are the raw
// extension-side `event` frame; the multiplex parses them.
func (r *Registry) OnEvent(fn func(id string, eventBytes []byte)) {
	r.hookMu.Lock()
	r.onEvent = fn
	r.hookMu.Unlock()
}

// OnStateChange is reserved for #42 (heartbeat-timeout detection) and
// later state-transition work. Currently no codepath in M3+M4 fires it;
// the hook is plumbed so the future work can subscribe without surgery.
func (r *Registry) OnStateChange(fn func(id string, from, to SessionState)) {
	r.hookMu.Lock()
	r.onStateChange = fn
	r.hookMu.Unlock()
}

// hookSnapshot returns the currently-installed callbacks under the hook
// mutex. Returning by value lets the caller release the lock before
// invoking, so callbacks can re-enter the registry safely.
func (r *Registry) hookSnapshot() (
	onRegister func(s *Session),
	onHeartbeat func(id string, ts time.Time),
	onEnded func(s *Session, kind EndedKind, reason string),
	onEvent func(id string, eventBytes []byte),
) {
	r.hookMu.RLock()
	defer r.hookMu.RUnlock()
	return r.onRegister, r.onHeartbeat, r.onEnded, r.onEvent
}

// safeInvoke runs fn under a deferred recover so a panicking callback
// does not propagate out of the registry. Logs the recovered value at
// WARN with the callback's name for diagnostic context.
func safeInvoke(name string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Warn("registry callback panicked",
				slog.String("callback", name),
				slog.Any("panic", rec))
		}
	}()
	fn()
}

// Register stores s if no entry exists for s.SessionID, or refreshes the
// existing entry if it has a matching PID (same Pi process re-registering
// after an ext-side socket reconnect). Returns (false,
// ErrCodeDuplicateSessionID) when the session_id is taken by a different
// pid; the caller forwards this verbatim to register_ack.reason.
//
// Fires OnRegister exactly once per initially-accepted session;
// idempotent re-registrations do NOT re-fire.
func (r *Registry) Register(s *Session) (accepted bool, reason string) {
	r.mu.Lock()
	existing, present := r.sessions[s.SessionID]
	isNew := !present
	if present && existing.PID != s.PID {
		r.mu.Unlock()
		return false, ErrCodeDuplicateSessionID
	}
	r.sessions[s.SessionID] = s
	r.mu.Unlock()

	if isNew {
		onRegister, _, _, _ := r.hookSnapshot()
		if onRegister != nil {
			safeInvoke("OnRegister", func() { onRegister(s) })
		}
	}
	return true, ""
}

func (r *Registry) UpdateHeartbeat(id string, ts time.Time) error {
	r.mu.Lock()
	s, ok := r.sessions[id]
	if !ok {
		r.mu.Unlock()
		return ErrUnknownSession
	}
	s.LastHeartbeat = ts
	// Recovery (SPEC § 12.1): a heartbeat from an unresponsive session
	// proves the channel is alive again.
	var recovered bool
	if s.State == StateUnresponsive {
		s.State = StateRunning
		recovered = true
	}
	onStateChange := r.onStateChange
	r.mu.Unlock()

	if recovered && onStateChange != nil {
		safeInvoke("OnStateChange", func() { onStateChange(id, StateUnresponsive, StateRunning) })
	}

	_, onHeartbeat, _, _ := r.hookSnapshot()
	if onHeartbeat != nil {
		safeInvoke("OnHeartbeat", func() { onHeartbeat(id, ts) })
	}
	return nil
}

// Event fans out an extension-side event frame to the OnEvent callback.
// Bytes are passed through verbatim; the registry does no parsing.
func (r *Registry) Event(id string, eventBytes []byte) {
	_, _, _, onEvent := r.hookSnapshot()
	if onEvent != nil {
		safeInvoke("OnEvent", func() { onEvent(id, eventBytes) })
	}
}

// Remove deletes the entry. Thin wrapper around RemoveWithReason(id, "")
// for backward compatibility with the M1 socket handler.
func (r *Registry) Remove(id string) {
	r.RemoveWithReason(id, "")
}

// RemoveWithReason deletes the entry and fires OnEnded with
// kind=EndedExplicit and the supplied reason. The reason is the
// extension-side disconnect reason (session_shutdown / client_request /
// error) and is informational; the multiplex collapses all of them to
// the single upstream reason value `extension_disconnect` per the
// schema's enum.
//
// If MarkEnded already fired OnEnded for this session, this call is a
// no-op (one OnEnded per session lifetime).
func (r *Registry) RemoveWithReason(id, reason string) {
	r.mu.Lock()
	s, present := r.sessions[id]
	alreadyEnded := r.ended[id]
	if present && !alreadyEnded {
		s.EndedAt = nowFn()
		r.ended[id] = true
	}
	delete(r.sessions, id)
	r.mu.Unlock()

	if present && !alreadyEnded {
		_, _, onEnded, _ := r.hookSnapshot()
		if onEnded != nil {
			safeInvoke("OnEnded", func() { onEnded(s, EndedExplicit, reason) })
		}
	}
}

// SweepHeartbeats flips every running/idle session whose extension has
// been silent for longer than timeout to `unresponsive` (SPEC §§ 6.6,
// 12.2: three missed 10s heartbeats ≥ 30s). Idempotent: already-
// unresponsive, paused, and ended sessions are untouched. Returns the
// ids transitioned this sweep. Issue #42.
func (r *Registry) SweepHeartbeats(now time.Time, timeout time.Duration) []string {
	r.mu.Lock()
	var flipped []string
	for id, s := range r.sessions {
		if s.State != StateRunning && s.State != StateIdle {
			continue
		}
		if now.Sub(s.LastHeartbeat) <= timeout {
			continue
		}
		from := s.State
		s.State = StateUnresponsive
		flipped = append(flipped, id)
		if r.onStateChange != nil {
			fn := r.onStateChange
			safeInvoke("OnStateChange", func() { fn(id, from, StateUnresponsive) })
		}
	}
	r.mu.Unlock()
	return flipped
}

// ReapEnded deletes entries that ended longer than ttl ago: attached
// clients have long since seen the final state, and without a reaper the
// registry grows unbounded over the daemon's lifetime. Returns the
// number reaped. Issue #43.
func (r *Registry) ReapEnded(now time.Time, ttl time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	reaped := 0
	for id, s := range r.sessions {
		if s.State == StateEnded && !s.EndedAt.IsZero() && now.Sub(s.EndedAt) > ttl {
			delete(r.sessions, id)
			reaped++
		}
	}
	return reaped
}

// MarkEnded keeps the entry but flips State to `ended` and stamps
// EndedAt. Used when the extension's socket closes without a
// `disconnect` frame; the reaper (#43) will sweep ended entries.
//
// Fires OnEnded with kind=EndedImplicit. If RemoveWithReason already
// fired OnEnded for this session, this call is a no-op (one OnEnded per
// session lifetime).
func (r *Registry) MarkEnded(id string) {
	r.mu.Lock()
	s, present := r.sessions[id]
	alreadyEnded := r.ended[id]
	if present && !alreadyEnded {
		s.State = StateEnded
		s.EndedAt = nowFn()
		r.ended[id] = true
	}
	r.mu.Unlock()

	if present && !alreadyEnded {
		_, _, onEnded, _ := r.hookSnapshot()
		if onEnded != nil {
			safeInvoke("OnEnded", func() { onEnded(s, EndedImplicit, "") })
		}
	}
}

// Get returns a snapshot copy of the named session. Returning by value
// keeps the registry's mutex the single point of synchronization on the
// underlying *Session — exposing the live pointer would race against
// concurrent UpdateHeartbeat / MarkEnded calls.
func (r *Registry) Get(id string) (Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *s, true
}

// Snapshot returns a copy of every currently-registered session whose
// State is not ended. Used by the multiplex's LiveSessions for
// session_resume emission on coordinator reconnect.
//
// The returned slice is fresh; mutating it does not affect the registry.
// The underlying *Session pointers are snapshotted to value-types so
// callers do not race with future mutation.
func (r *Registry) Snapshot() []Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		if s.State == StateEnded {
			continue
		}
		out = append(out, *s)
	}
	return out
}
