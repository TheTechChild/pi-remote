// SPDX-License-Identifier: MIT
package auth

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
)

// C-1: ErrUnauthenticated must be a sentinel that errors.Is recognises both
// when returned directly and when wrapped with fmt.Errorf("...%w", ...).
func TestErrUnauthenticatedIsSentinel(t *testing.T) {
	if ErrUnauthenticated == nil {
		t.Fatal("ErrUnauthenticated must not be nil")
	}
	if !errors.Is(ErrUnauthenticated, ErrUnauthenticated) {
		t.Fatal("errors.Is(ErrUnauthenticated, ErrUnauthenticated) returned false")
	}
	wrapped := fmt.Errorf("wrapped: %w", ErrUnauthenticated)
	if !errors.Is(wrapped, ErrUnauthenticated) {
		t.Fatal("errors.Is on wrapped error returned false")
	}
}

// C-2: Both implementations satisfy the Middleware interface, and the
// interface has exactly the two expected methods.
func TestMiddlewareInterfaceShape(t *testing.T) {
	// Compile-time guards.
	var _ Middleware = (*Stub)(nil)
	var _ Middleware = (*CFAccess)(nil)

	// Reflection: ensure the interface has exactly two methods named as
	// specified. Use a pointer-to-interface trick.
	iface := reflect.TypeOf((*Middleware)(nil)).Elem()
	if iface.Kind() != reflect.Interface {
		t.Fatalf("Middleware is not an interface, kind=%v", iface.Kind())
	}
	if iface.NumMethod() != 2 {
		t.Fatalf("Middleware has %d methods, want 2", iface.NumMethod())
	}
	want := map[string]bool{"ServiceToken": false, "AccessJWT": false}
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		if _, ok := want[m.Name]; !ok {
			t.Errorf("unexpected method %q on Middleware", m.Name)
			continue
		}
		want[m.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("Middleware missing method %q", name)
		}
	}

	// Sanity: a Stub constructor exists and produces a usable value.
	s := NewStub()
	if s == nil {
		t.Fatal("NewStub() returned nil")
	}

	// Sanity: a CFAccess constructor exists; we only construct here to keep
	// the import live — full constructor behaviour is covered in C-11/C-12.
	_, _ = NewCFAccess(config.CloudflareConfig{AccessAud: "test-aud"})
}
