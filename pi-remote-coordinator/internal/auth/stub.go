// SPDX-License-Identifier: MIT
package auth

import "net/http"

// Stub is the in-process auth implementation used by tests and the
// `-auth=stub` flag. It hard-codes a small set of fixtures documented in
// docs/phase1/batch-2-coordinator-link.md:
//
//   - CF-Access-Client-Id: "test-machine" + any non-empty
//     CF-Access-Client-Secret → Identity{MachineID:"test-machine"}.
//   - Cookie: CF_Authorization=test-jwt-clayton →
//     Identity{Email:"clayton@example.com", ClientID:"test-client-1"}.
//   - test-jwt-expired / test-jwt-malformed / anything else →
//     ErrUnauthenticated.
//
// The stub maps the CF-Access-Client-Id header value verbatim to
// Identity.MachineID; the registry-side machine_id (e.g. "macbook-pro")
// comes from the daemon's machine_register frame, NOT from the auth header.
type Stub struct{}

// NewStub returns a ready-to-use Stub. The zero value works too; the
// constructor exists for symmetry with NewCFAccess and to make wiring in
// main.go obvious.
func NewStub() *Stub { return &Stub{} }

const (
	stubServiceTokenClientID = "test-machine"
	stubAccessJWTClayton     = "test-jwt-clayton"
	stubAccessJWTEmail       = "clayton@example.com"
	stubAccessJWTClientID    = "test-client-1"
)

// ServiceToken implements Middleware.
func (s *Stub) ServiceToken(r *http.Request) (Identity, error) {
	clientID := r.Header.Get("CF-Access-Client-Id")
	clientSecret := r.Header.Get("CF-Access-Client-Secret")
	if clientID == "" || clientSecret == "" {
		return Identity{}, ErrUnauthenticated
	}
	if clientID != stubServiceTokenClientID {
		return Identity{}, ErrUnauthenticated
	}
	return Identity{MachineID: clientID}, nil
}

// AccessJWT implements Middleware.
func (s *Stub) AccessJWT(r *http.Request) (Identity, error) {
	c, err := r.Cookie("CF_Authorization")
	if err != nil || c == nil || c.Value == "" {
		return Identity{}, ErrUnauthenticated
	}
	switch c.Value {
	case stubAccessJWTClayton:
		return Identity{Email: stubAccessJWTEmail, ClientID: stubAccessJWTClientID}, nil
	default:
		// Includes test-jwt-expired, test-jwt-malformed, and any other value.
		return Identity{}, ErrUnauthenticated
	}
}
