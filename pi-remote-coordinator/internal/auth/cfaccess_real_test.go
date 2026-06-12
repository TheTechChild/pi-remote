// SPDX-License-Identifier: MIT
package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
)

// jwksFixture serves a JWKS for the given keys and counts fetches.
type jwksFixture struct {
	t       *testing.T
	keys    map[string]*rsa.PrivateKey // kid → key
	srv     *httptest.Server
	fetches int
}

func newJWKS(t *testing.T, kids ...string) *jwksFixture {
	t.Helper()
	f := &jwksFixture{t: t, keys: map[string]*rsa.PrivateKey{}}
	for _, kid := range kids {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		f.keys[kid] = key
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.fetches++
		var doc struct {
			Keys []map[string]string `json:"keys"`
		}
		for kid, key := range f.keys {
			doc.Keys = append(doc.Keys, map[string]string{
				"kty": "RSA", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// mint signs an RS256 JWT with the named kid.
func (f *jwksFixture) mint(kid string, claims map[string]any) string {
	f.t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.keys[kid], crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newTestCFAccess(t *testing.T, f *jwksFixture) *CFAccess {
	t.Helper()
	cf, err := NewCFAccess(config.CloudflareConfig{
		AccessAud:            "aud-clients",
		ServiceTokenAudience: "aud-daemons",
		TeamDomain:           "team.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	cf.certsURL = f.srv.URL // point JWKS at the fixture
	return cf
}

func clientReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/client", nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: token})
	}
	return r
}

func TestCFAccess_AccessJWT_Valid(t *testing.T) {
	f := newJWKS(t, "kid1")
	cf := newTestCFAccess(t, f)
	token := f.mint("kid1", map[string]any{
		"aud": "aud-clients", "email": "clayton@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	id, err := cf.AccessJWT(clientReq(token))
	if err != nil {
		t.Fatalf("AccessJWT: %v", err)
	}
	if id.Email != "clayton@example.com" {
		t.Errorf("Email = %q", id.Email)
	}
	// aud-as-array form also accepted.
	token2 := f.mint("kid1", map[string]any{
		"aud": []string{"other", "aud-clients"}, "email": "clayton@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := cf.AccessJWT(clientReq(token2)); err != nil {
		t.Errorf("array aud rejected: %v", err)
	}
}

func TestCFAccess_AccessJWT_Rejections(t *testing.T) {
	f := newJWKS(t, "kid1")
	cf := newTestCFAccess(t, f)
	exp := time.Now().Add(time.Hour).Unix()

	hs256Header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"kid1"}`))
	valid := f.mint("kid1", map[string]any{"aud": "aud-clients", "email": "e@x", "exp": exp})
	tampered := valid[:len(valid)-3] + "abc"

	cases := map[string]string{
		"expired":      f.mint("kid1", map[string]any{"aud": "aud-clients", "email": "e@x", "exp": time.Now().Add(-time.Hour).Unix()}),
		"wrong aud":    f.mint("kid1", map[string]any{"aud": "aud-daemons", "email": "e@x", "exp": exp}),
		"no email":     f.mint("kid1", map[string]any{"aud": "aud-clients", "exp": exp}),
		"bad alg":      hs256Header + "." + strings.SplitN(valid, ".", 2)[1],
		"tampered sig": tampered,
		"malformed":    "not.a.jwt.at.all",
		"empty":        "",
	}
	for name, token := range cases {
		if _, err := cf.AccessJWT(clientReq(token)); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: err = %v, want ErrUnauthenticated", name, err)
		}
	}
}

func TestCFAccess_ServiceToken(t *testing.T) {
	f := newJWKS(t, "kid1")
	cf := newTestCFAccess(t, f)
	token := f.mint("kid1", map[string]any{
		"aud": "aud-daemons", "common_name": "svc-token-id.access",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	r := httptest.NewRequest(http.MethodGet, "/v1/daemon", nil)
	r.Header.Set("Cf-Access-Jwt-Assertion", token)
	id, err := cf.ServiceToken(r)
	if err != nil {
		t.Fatalf("ServiceToken: %v", err)
	}
	if id.MachineID != "svc-token-id.access" {
		t.Errorf("MachineID = %q", id.MachineID)
	}

	// Client-audience token must not authenticate the daemon path.
	wrongAud := f.mint("kid1", map[string]any{
		"aud": "aud-clients", "common_name": "svc", "exp": time.Now().Add(time.Hour).Unix(),
	})
	r.Header.Set("Cf-Access-Jwt-Assertion", wrongAud)
	if _, err := cf.ServiceToken(r); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("cross-audience accepted: %v", err)
	}
}

func TestCFAccess_KeyRotationRefetch(t *testing.T) {
	f := newJWKS(t, "kid1")
	cf := newTestCFAccess(t, f)

	// Prime the cache with kid1.
	token := f.mint("kid1", map[string]any{"aud": "aud-clients", "email": "e@x", "exp": time.Now().Add(time.Hour).Unix()})
	if _, err := cf.AccessJWT(clientReq(token)); err != nil {
		t.Fatal(err)
	}
	if f.fetches != 1 {
		t.Fatalf("fetches = %d, want 1", f.fetches)
	}

	// Rotate: add kid2 server-side; a kid2 token within the refresh
	// interval must NOT re-fetch (rate limit) and must be rejected.
	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.keys["kid2"] = key2
	token2 := f.mint("kid2", map[string]any{"aud": "aud-clients", "email": "e@x", "exp": time.Now().Add(time.Hour).Unix()})
	if _, err := cf.AccessJWT(clientReq(token2)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("within-interval unknown kid: err = %v", err)
	}
	if f.fetches != 1 {
		t.Fatalf("re-fetched within interval: fetches = %d", f.fetches)
	}

	// After the interval, the unknown kid triggers a refresh and the
	// rotated key validates.
	cf.now = func() time.Time { return time.Now().Add(jwksRefreshInterval + time.Second) }
	if _, err := cf.AccessJWT(clientReq(token2)); err != nil {
		t.Fatalf("post-rotation validation failed: %v", err)
	}
	if f.fetches != 2 {
		t.Errorf("fetches = %d, want 2", f.fetches)
	}
}
