// SPDX-License-Identifier: MIT
package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
)

// CFAccess is the production Cloudflare Access middleware (SPEC.md § 11):
// it validates the RS256 JWTs Cloudflare mints after authenticating a
// request at the edge, against the team's JWKS
// (https://<team_domain>/cdn-cgi/access/certs).
//
//   - AccessJWT: the CF_Authorization cookie (email-PIN flow, § 11.2);
//     audience must equal cloudflare.access_aud; Identity.Email comes
//     from the `email` claim.
//   - ServiceToken: the Cf-Access-Jwt-Assertion header CF injects after
//     validating a daemon's service-token headers (§ 11.1); audience must
//     equal cloudflare.service_token_audience; Identity.MachineID comes
//     from the `common_name` claim.
//
// Verification is implemented with the standard library (rsa.VerifyPKCS1v15
// over RS256) rather than a JWT dependency: the SPEC § 22.3 table has no
// JWT library, and the verified surface is deliberately tiny — RS256 only,
// exact-audience, exp/nbf.
type CFAccess struct {
	cfg      config.CloudflareConfig
	certsURL string
	http     *http.Client
	now      func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey // kid → key
	fetchedAt time.Time
}

// jwksRefreshInterval bounds how often an unknown kid may trigger a
// re-fetch (key rotation) and how stale a cached key set may get.
const jwksRefreshInterval = 5 * time.Minute

// NewCFAccess constructs the middleware. team_domain and both audience
// tags are required (also enforced by config.ValidateCFAccess).
func NewCFAccess(cfg config.CloudflareConfig) (*CFAccess, error) {
	if cfg.AccessAud == "" {
		return nil, fmt.Errorf("auth: cfaccess requires cloudflare.access_aud to be set")
	}
	if cfg.TeamDomain == "" {
		return nil, fmt.Errorf("auth: cfaccess requires cloudflare.team_domain to be set")
	}
	domain := strings.TrimSuffix(strings.TrimPrefix(cfg.TeamDomain, "https://"), "/")
	return &CFAccess{
		cfg:      cfg,
		certsURL: "https://" + domain + "/cdn-cgi/access/certs",
		http:     &http.Client{Timeout: 10 * time.Second},
		now:      time.Now,
	}, nil
}

// ServiceToken implements Middleware (SPEC § 11.1).
func (c *CFAccess) ServiceToken(r *http.Request) (Identity, error) {
	token := r.Header.Get("Cf-Access-Jwt-Assertion")
	if token == "" {
		return Identity{}, fmt.Errorf("%w: missing Cf-Access-Jwt-Assertion", ErrUnauthenticated)
	}
	claims, err := c.verify(token, c.cfg.ServiceTokenAudience)
	if err != nil {
		return Identity{}, err
	}
	if claims.CommonName == "" {
		return Identity{}, fmt.Errorf("%w: service JWT missing common_name", ErrUnauthenticated)
	}
	return Identity{MachineID: claims.CommonName}, nil
}

// AccessJWT implements Middleware (SPEC § 11.2). The JWT is taken from
// the CF_Authorization cookie, falling back to Cf-Access-Jwt-Assertion
// (CF sets both on edge-authenticated browser requests).
func (c *CFAccess) AccessJWT(r *http.Request) (Identity, error) {
	token := ""
	if cookie, err := r.Cookie("CF_Authorization"); err == nil {
		token = cookie.Value
	}
	if token == "" {
		token = r.Header.Get("Cf-Access-Jwt-Assertion")
	}
	if token == "" {
		return Identity{}, fmt.Errorf("%w: missing CF_Authorization", ErrUnauthenticated)
	}
	claims, err := c.verify(token, c.cfg.AccessAud)
	if err != nil {
		return Identity{}, err
	}
	if claims.Email == "" {
		return Identity{}, fmt.Errorf("%w: access JWT missing email", ErrUnauthenticated)
	}
	return Identity{Email: claims.Email}, nil
}

type cfClaims struct {
	Aud        audience `json:"aud"`
	Exp        int64    `json:"exp"`
	Nbf        int64    `json:"nbf"`
	Email      string   `json:"email"`
	CommonName string   `json:"common_name"`
}

// audience tolerates CF's string-or-array aud encoding.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = []string{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err != nil {
		return err
	}
	*a = arr
	return nil
}

// verify checks signature, algorithm, audience, and time claims.
func (c *CFAccess) verify(token, wantAud string) (*cfClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: malformed JWT", ErrUnauthenticated)
	}
	headerB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed JWT header", ErrUnauthenticated)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerB, &header); err != nil {
		return nil, fmt.Errorf("%w: malformed JWT header", ErrUnauthenticated)
	}
	// RS256 only — reject alg confusion (none, HS256, ...) outright.
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unsupported alg %q", ErrUnauthenticated, header.Alg)
	}

	key, err := c.keyFor(header.Kid)
	if err != nil {
		return nil, err
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed JWT signature", ErrUnauthenticated)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, fmt.Errorf("%w: signature verification failed", ErrUnauthenticated)
	}

	payloadB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed JWT payload", ErrUnauthenticated)
	}
	var claims cfClaims
	if err := json.Unmarshal(payloadB, &claims); err != nil {
		return nil, fmt.Errorf("%w: malformed JWT claims", ErrUnauthenticated)
	}

	audOK := false
	for _, a := range claims.Aud {
		if subtle.ConstantTimeCompare([]byte(a), []byte(wantAud)) == 1 {
			audOK = true
		}
	}
	if !audOK {
		return nil, fmt.Errorf("%w: audience mismatch", ErrUnauthenticated)
	}

	now := c.now()
	if claims.Exp != 0 && now.After(time.Unix(claims.Exp, 0)) {
		return nil, fmt.Errorf("%w: token expired", ErrUnauthenticated)
	}
	if claims.Nbf != 0 && now.Add(time.Minute).Before(time.Unix(claims.Nbf, 0)) {
		return nil, fmt.Errorf("%w: token not yet valid", ErrUnauthenticated)
	}
	return &claims, nil
}

// keyFor returns the cached RSA key for kid, refreshing the JWKS at most
// once per jwksRefreshInterval when the kid is unknown (key rotation).
func (c *CFAccess) keyFor(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	if c.now().Sub(c.fetchedAt) < jwksRefreshInterval && c.keys != nil {
		return nil, fmt.Errorf("%w: unknown signing key %q", ErrUnauthenticated, kid)
	}
	if err := c.refreshLocked(); err != nil {
		return nil, err
	}
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: unknown signing key %q", ErrUnauthenticated, kid)
}

func (c *CFAccess) refreshLocked() error {
	resp, err := c.http.Get(c.certsURL)
	if err != nil {
		return fmt.Errorf("auth: JWKS fetch %s: %w", c.certsURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: JWKS fetch %s: HTTP %d", c.certsURL, resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("auth: JWKS decode: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nB, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eB, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nB),
			E: int(new(big.Int).SetBytes(eB).Int64()),
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("auth: JWKS at %s contained no RSA keys", c.certsURL)
	}
	c.keys = keys
	c.fetchedAt = c.now()
	return nil
}
