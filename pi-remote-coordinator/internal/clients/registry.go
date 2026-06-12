// SPDX-License-Identifier: MIT
package clients

import "sync"

// Registry is the in-memory map of registered remote clients. Phase-1 has
// no persistence; entries are seeded at startup (stub fixture) or by the
// /v1/clients/register endpoint (later milestone).
type Registry struct {
	mu sync.RWMutex
	m  map[string]*Client
}

// Option configures a Registry at construction.
type Option func(*Registry)

// WithStubFixture pre-seeds the test-client-1 fixture used by the stub auth
// path and by the documented manual smoke. Production main only applies
// this when -auth=stub.
func WithStubFixture() Option {
	return func(r *Registry) {
		r.m["test-client-1"] = &Client{
			ID:                "test-client-1",
			DeviceDisplayName: "Clayton's iPhone (stub)",
		}
	}
}

// NewRegistry constructs an empty Registry, then applies the provided
// options.
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{m: make(map[string]*Client)}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register inserts (or replaces) a client record. Called by the
// POST /v1/clients/register handler (SPEC.md § 11.3).
func (r *Registry) Register(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := *c
	r.m[c.ID] = &snap
}

// List returns a snapshot of all registered clients.
func (r *Registry) List() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]*Client, 0, len(r.m))
	for _, c := range r.m {
		snap := *c
		res = append(res, &snap)
	}
	return res
}

// SetPreferences replaces the stored per-reason push toggles for id.
// Returns false when the client is unknown. The map is copied, and reads
// always observe a complete map (replace-on-write, never mutate-in-place).
func (r *Registry) SetPreferences(id string, prefs map[string]bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.m[id]
	if !ok {
		return false
	}
	cp := make(map[string]bool, len(prefs))
	for k, v := range prefs {
		cp[k] = v
	}
	c.Preferences = cp
	return true
}

// Get returns a snapshot of the client and ok=true, or (nil, false).
func (r *Registry) Get(id string) (*Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.m[id]
	if !ok {
		return nil, false
	}
	snap := *c
	return &snap, true
}
