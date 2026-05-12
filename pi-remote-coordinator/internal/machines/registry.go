// SPDX-License-Identifier: MIT
package machines

import (
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Registry is the in-memory map of machine_id → Machine. Safe for
// concurrent use. Take-over (two daemons claiming the same machine_id)
// is atomic from any observer's perspective: a reader either sees the old
// Conn or the new Conn, never an empty slot.
type Registry struct {
	mu sync.RWMutex
	m  map[string]*Machine

	now func() time.Time
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		m:   make(map[string]*Machine),
		now: time.Now,
	}
}

// Register inserts or replaces the entry for machineID. If a previous
// entry exists, its Conn is closed (StatusPolicyViolation, reason
// "machine_register take-over") and the new entry replaces it atomically
// under the registry mutex.
//
// The previous-Conn close is invoked while we hold the write lock, which
// is fine: fakeConn.Close is non-blocking, and *websocket.Conn.Close on
// coder/websocket signals a close frame asynchronously — it does not wait
// for the peer.
func (r *Registry) Register(machineID, displayName, daemonVersion string, capabilities []string, conn Conn) *Machine {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if prev, ok := r.m[machineID]; ok && prev.Conn != nil && prev.Conn != conn {
		_ = prev.Conn.Close(websocket.StatusPolicyViolation, "machine_register take-over")
	}
	m := &Machine{
		ID:            machineID,
		DisplayName:   displayName,
		DaemonVersion: daemonVersion,
		Capabilities:  capabilities,
		State:         "online",
		Conn:          conn,
		LastSeen:      now,
	}
	r.m[machineID] = m
	return m
}

// Get returns a snapshot of the machine and ok=true, or (nil, false).
// The returned pointer is a copy of the registry entry: callers may read
// its fields without holding any lock, but mutating it does not affect
// the registry.
func (r *Registry) Get(machineID string) (*Machine, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.m[machineID]
	if !ok {
		return nil, false
	}
	snap := *m
	return &snap, true
}

// SetSuspended flips State to "suspended". No-op if unknown.
func (r *Registry) SetSuspended(machineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.m[machineID]; ok {
		m.State = "suspended"
		m.LastSeen = r.now()
	}
}

// UnregisterByConn removes the entry only if its current Conn matches the
// one passed in. This is the safe close-cleanup path: if a take-over has
// already swapped in a newer Conn, the older daemon's deferred unregister
// is a no-op.
func (r *Registry) UnregisterByConn(machineID string, conn Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.m[machineID]; ok && m.Conn == conn {
		delete(r.m, machineID)
	}
}
