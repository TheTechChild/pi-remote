// SPDX-License-Identifier: MIT
package session

import (
	"errors"
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

// Registry is the daemon's in-memory map of active and recently-ended Pi
// sessions. Concurrent access is guarded by a single RWMutex; per SPEC § 7.5
// the workload is a small map with low contention, so channels are deferred
// until M3 (coordinator multiplex) needs goroutine fan-out.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*Session)}
}

// Register stores s if no entry exists for s.SessionID, or refreshes the
// existing entry if it has a matching PID (same Pi process re-registering
// after an ext-side socket reconnect). Returns (false,
// ErrCodeDuplicateSessionID) when the session_id is taken by a different
// pid; the caller forwards this verbatim to register_ack.reason.
func (r *Registry) Register(s *Session) (accepted bool, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.sessions[s.SessionID]
	if ok && existing.PID != s.PID {
		return false, ErrCodeDuplicateSessionID
	}
	r.sessions[s.SessionID] = s
	return true, ""
}

func (r *Registry) UpdateHeartbeat(id string, ts time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return ErrUnknownSession
	}
	s.LastHeartbeat = ts
	return nil
}

// Remove deletes the entry. Used for explicit `disconnect` frames per the
// batch plan.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// MarkEnded keeps the entry but flips State to `ended`. Used when the
// extension's socket closes without a `disconnect` frame; later milestones
// add a reaper.
func (r *Registry) MarkEnded(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok {
		s.State = StateEnded
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
