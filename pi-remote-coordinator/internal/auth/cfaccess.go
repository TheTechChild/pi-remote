// SPDX-License-Identifier: MIT
package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
)

// CFAccess is the production Cloudflare Access auth middleware. Wired only
// when the coordinator is started with `-auth=cfaccess`. This file is a
// placeholder for Workstream C: the constructor validates config, but
// ServiceToken/AccessJWT return errCFAccessNotImplemented. Real JWT/JWKS
// validation lands in a later milestone (SPEC.md § 11.2, deferred decision
// notes in docs/phase1/batch-2-coordinator-link.md).
type CFAccess struct {
	cfg config.CloudflareConfig
}

// errCFAccessNotImplemented is returned by the CFAccess methods until the
// real validator ships. Tests never select the cfaccess path.
var errCFAccessNotImplemented = errors.New("auth: cfaccess validator not yet implemented")

// NewCFAccess constructs a CFAccess middleware. Returns a clear error if
// access_aud is empty — without it we cannot validate JWT audience claims.
func NewCFAccess(cfg config.CloudflareConfig) (*CFAccess, error) {
	if cfg.AccessAud == "" {
		return nil, fmt.Errorf("auth: cfaccess requires cloudflare.access_aud to be set")
	}
	return &CFAccess{cfg: cfg}, nil
}

// ServiceToken implements Middleware. Not yet implemented in Workstream C.
func (c *CFAccess) ServiceToken(_ *http.Request) (Identity, error) {
	return Identity{}, errCFAccessNotImplemented
}

// AccessJWT implements Middleware. Not yet implemented in Workstream C.
func (c *CFAccess) AccessJWT(_ *http.Request) (Identity, error) {
	return Identity{}, errCFAccessNotImplemented
}
