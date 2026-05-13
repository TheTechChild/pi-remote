// SPDX-License-Identifier: MIT
package clients

import (
	"testing"
)

// C-43: Pre-seeded test-client-1 returned by Get when constructed with
// WithStubFixture.
func TestRegistry_PreSeedsStubFixture(t *testing.T) {
	r := NewRegistry(WithStubFixture())
	c, ok := r.Get("test-client-1")
	if !ok {
		t.Fatal("test-client-1 not pre-seeded")
	}
	if c.ID != "test-client-1" {
		t.Errorf("ID = %q", c.ID)
	}
	if c.DeviceDisplayName == "" {
		t.Errorf("DeviceDisplayName empty; stub should populate a sensible value")
	}
}

// C-44: Unknown client_id → nil, false.
func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry(WithStubFixture())
	c, ok := r.Get("not-real")
	if ok {
		t.Errorf("ok = true, want false")
	}
	if c != nil {
		t.Errorf("client = %+v, want nil", c)
	}
}

// Without WithStubFixture, the registry is empty by default. This protects
// against accidentally pre-seeding in production when -auth=cfaccess is
// selected.
func TestRegistry_EmptyByDefault(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("test-client-1"); ok {
		t.Errorf("test-client-1 should NOT be pre-seeded without WithStubFixture")
	}
}
