// SPDX-License-Identifier: MIT

// Package auth implements the coordinator's HTTP authentication boundary.
// Two paths exist:
//
//   - ServiceToken: validates Cloudflare Access service-token headers on the
//     daemon WebSocket endpoint (SPEC.md § 11.1).
//   - AccessJWT:    validates the CF_Authorization JWT cookie set by
//     Cloudflare Access on the client WebSocket endpoint (SPEC.md § 11.2).
//
// Both methods MUST run BEFORE websocket.Accept on the corresponding handler;
// once a 101 has been sent, you can only close, not 403. On
// ErrUnauthenticated the handler writes 403 with body
// "ERR_COORD_AUTH_REQUIRED" (errors/codes.md).
package auth

import (
	"errors"
	"net/http"
)

// ErrUnauthenticated is the sentinel returned by both Middleware methods
// whenever the request cannot be authenticated. Handlers translate this to
// 403 + ERR_COORD_AUTH_REQUIRED.
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// Identity is the principal extracted from an authenticated request.
//
// For ServiceToken validation, MachineID is populated (the value of the
// CF-Access-Client-Id header in the stub; in production it's the verified
// subject from the service-token mTLS handshake).
//
// For AccessJWT validation, Email and ClientID are populated. Email is the
// user identity; ClientID is the registered Pi-remote client device id
// (set in the cookie or derived from device-binding state). See
// SPEC.md §§ 8.5, 11.2.
type Identity struct {
	MachineID string
	Email     string
	ClientID  string
}

// Middleware authenticates inbound HTTP requests on the two WebSocket
// endpoints. Implementations MUST be safe for concurrent use.
type Middleware interface {
	// ServiceToken validates the CF-Access-Client-Id / -Secret pair on the
	// /v1/daemon endpoint. Returns ErrUnauthenticated on failure.
	ServiceToken(r *http.Request) (Identity, error)

	// AccessJWT validates the CF_Authorization cookie on the /v1/client
	// endpoint. Returns ErrUnauthenticated on failure.
	AccessJWT(r *http.Request) (Identity, error)
}
