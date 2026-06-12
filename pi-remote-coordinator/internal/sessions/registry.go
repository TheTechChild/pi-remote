// SPDX-License-Identifier: MIT
package sessions

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/broker"
	coordinator_app "github.com/TheTechChild/pi-remote-coordinator/internal/proto/coordinator-app"
	daemon_coordinator "github.com/TheTechChild/pi-remote-coordinator/internal/proto/daemon-coordinator"
)

// Defaults per SPEC.md § 18.1; production wiring should pass the configured
// values via NewRegistryWithLimits (config [broker] section, SPEC.md § 8.3).
const (
	defaultTotalCacheBytes        = 50 * 1024 * 1024
	defaultSessionCacheFloorBytes = 1 * 1024 * 1024
)

// Registry is an in-memory map of sessions, safe for concurrent use.
type Registry struct {
	mu  sync.RWMutex
	m   map[string]*Session
	now func() time.Time // injectable for tests; defaults to time.Now

	totalCacheBytes        int64 // global ring budget (SPEC.md § 18.1)
	sessionCacheFloorBytes int64 // per-session eviction floor
}

// NewRegistry constructs an empty Registry with SPEC.md § 18.1 default
// cache sizing (50MB total, 1MB floor).
func NewRegistry() *Registry {
	return NewRegistryWithLimits(defaultTotalCacheBytes, defaultSessionCacheFloorBytes)
}

// NewRegistryWithLimits constructs an empty Registry with explicit broker
// cache sizing, normally from config [broker] (SPEC.md § 8.3). Non-positive
// values fall back to the SPEC defaults.
func NewRegistryWithLimits(totalCacheBytes, sessionCacheFloorBytes int64) *Registry {
	if totalCacheBytes <= 0 {
		totalCacheBytes = defaultTotalCacheBytes
	}
	if sessionCacheFloorBytes <= 0 {
		sessionCacheFloorBytes = defaultSessionCacheFloorBytes
	}
	return &Registry{
		m:                      make(map[string]*Session),
		now:                    time.Now,
		totalCacheBytes:        totalCacheBytes,
		sessionCacheFloorBytes: sessionCacheFloorBytes,
	}
}

// initialRingMaxBytes is the starting per-session ring budget: a tenth of
// the global cap (≈ the "~5MB per session at full saturation" sizing from
// SPEC.md § 18.1), never below the eviction floor. BalanceGlobalLRU grows
// or shrinks it from there.
func (r *Registry) initialRingMaxBytes() int64 {
	initial := r.totalCacheBytes / 10
	if initial < r.sessionCacheFloorBytes {
		initial = r.sessionCacheFloorBytes
	}
	return initial
}

// Register creates or replaces a session entry. State defaults to "running",
// LastSeq to 0, Ended to false.
func (r *Registry) Register(sessionID, machineID string, metadata daemon_coordinator.SessionStartedJsonMetadata) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	s := &Session{
		SessionID:       sessionID,
		MachineID:       machineID,
		Metadata:        metadata,
		State:           "running",
		LastSeq:         0,
		Ended:           false,
		LastTouched:     now,
		Ring:            broker.NewRingBuffer(r.initialRingMaxBytes()),
		AttachedClients: make(map[string]ClientConn),
	}
	r.m[sessionID] = s
	return s
}

// Get returns the actual session pointer and ok=true, or (nil, false) if not present.
// The session pointer is safe for concurrent access due to its internal RWMutex.
func (r *Registry) Get(sessionID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[sessionID]
	if !ok {
		return nil, false
	}
	return s, true
}

// AdvanceSeq sets LastSeq = max(LastSeq, seq). Returns true if LastSeq
// advanced (i.e. seq > previous LastSeq); false if seq was stale or equal.
func (r *Registry) AdvanceSeq(sessionID string, seq int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[sessionID]
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq > s.LastSeq {
		s.LastSeq = seq
		s.LastTouched = r.now()
		return true
	}
	return false
}

// SetState updates State on a known session. No-op if unknown.
func (r *Registry) SetState(sessionID, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.mu.Lock()
		s.State = state
		s.LastTouched = r.now()
		s.mu.Unlock()
	}
}

// MarkEnded sets Ended=true and stamps EndedAt.
func (r *Registry) MarkEnded(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.mu.Lock()
		s.Ended = true
		s.EndedAt = r.now()
		s.LastTouched = s.EndedAt
		s.State = "ended"
		s.mu.Unlock()
	}
}

// PauseAllForMachine flips every non-ended session belonging to machineID
// to State="paused" and broadcasts a session_state_change frame to each
// session's attached clients (SPEC.md §§ 8.7, 10.3). Ended and already-
// paused sessions are skipped.
func (r *Registry) PauseAllForMachine(machineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, s := range r.m {
		s.mu.Lock()
		if s.MachineID != machineID || s.Ended || s.State == "paused" {
			s.mu.Unlock()
			continue
		}
		from := s.State
		s.State = "paused"
		s.LastTouched = now
		s.mu.Unlock()

		frame := coordinator_app.SessionStateChangeJson{
			Type:      "session_state_change",
			V:         1,
			SessionId: s.SessionID,
			MachineId: machineID,
			From:      coordinator_app.State(from),
			To:        coordinator_app.StatePaused,
		}
		if b, err := json.Marshal(frame); err == nil {
			s.Broadcast(b)
		}
	}
}

// RestoreLastSeq sets LastSeq = max(LastSeq, n).
func (r *Registry) RestoreLastSeq(sessionID string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[sessionID]; ok {
		s.mu.Lock()
		if n > s.LastSeq {
			s.LastSeq = n
			s.LastTouched = r.now()
		}
		s.mu.Unlock()
	}
}

// LRUItems returns a list of LRU items for all registered sessions.
func (r *Registry) LRUItems() []broker.LRUItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]broker.LRUItem, 0, len(r.m))
	for _, s := range r.m {
		s.mu.RLock()
		items = append(items, broker.LRUItem{
			SessionID:   s.SessionID,
			Ended:       s.Ended,
			LastTouched: s.LastTouched,
			Ring:        s.Ring,
		})
		s.mu.RUnlock()
	}
	return items
}

// Publish appends an entry to a session's RingBuffer, fans it out to the
// session's attached clients, and triggers Global LRU balancing. The
// append + fan-out pair is atomic with respect to AttachWithReplay (see
// Session.Publish), so attaching clients never miss or double-receive a
// frame.
func (r *Registry) Publish(sessionID string, entry broker.Entry) {
	r.mu.RLock()
	s, ok := r.m[sessionID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	s.Publish(entry)
	s.mu.Lock()
	s.LastTouched = r.now()
	s.mu.Unlock()

	broker.BalanceGlobalLRU(r.LRUItems(), sessionID, r.totalCacheBytes, r.sessionCacheFloorBytes)
}

// List returns a snapshot of all registered sessions.
func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]*Session, 0, len(r.m))
	for _, s := range r.m {
		res = append(res, s)
	}
	return res
}
