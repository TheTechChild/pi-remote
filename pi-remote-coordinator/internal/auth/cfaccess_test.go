// SPDX-License-Identifier: MIT
package auth

import (
	"testing"

	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
)

// C-11: Constructor with valid config returns non-nil, no error.
func TestCFAccess_Constructor_Valid(t *testing.T) {
	m, err := NewCFAccess(config.CloudflareConfig{AccessAud: "test-aud", TeamDomain: "team.cloudflareaccess.com"})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if m == nil {
		t.Fatal("NewCFAccess returned nil with no error")
	}
}

// C-12: Constructor with empty access_aud returns a clear error.
func TestCFAccess_Constructor_EmptyAud(t *testing.T) {
	m, err := NewCFAccess(config.CloudflareConfig{})
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if m != nil {
		t.Errorf("middleware = %v, want nil", m)
	}
	// The error message should mention access_aud so an operator knows
	// what to set; we don't pin the exact wording.
	if !contains(err.Error(), "access_aud") {
		t.Errorf("error message = %q, want substring %q", err.Error(), "access_aud")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
