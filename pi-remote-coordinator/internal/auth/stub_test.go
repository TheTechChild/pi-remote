// SPDX-License-Identifier: MIT
package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newReqWithServiceToken(clientID, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/daemon", nil)
	if clientID != "" {
		r.Header.Set("CF-Access-Client-Id", clientID)
	}
	if secret != "" {
		r.Header.Set("CF-Access-Client-Secret", secret)
	}
	return r
}

func newReqWithCookie(name, value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/client", nil)
	if name != "" {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return r
}

// C-3: ServiceToken accepts test-machine + any non-empty secret.
func TestStub_ServiceToken_Accepts(t *testing.T) {
	s := NewStub()
	id, err := s.ServiceToken(newReqWithServiceToken("test-machine", "any-secret"))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if id.MachineID != "test-machine" {
		t.Errorf("MachineID = %q, want %q", id.MachineID, "test-machine")
	}
	if id.Email != "" || id.ClientID != "" {
		t.Errorf("unexpected fields populated: %+v", id)
	}
}

// C-4: ServiceToken with missing headers → ErrUnauthenticated.
func TestStub_ServiceToken_MissingHeaders(t *testing.T) {
	s := NewStub()
	_, err := s.ServiceToken(httptest.NewRequest(http.MethodGet, "/v1/daemon", nil))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// C-5: ServiceToken with empty client-id → ErrUnauthenticated, even when
// secret is set.
func TestStub_ServiceToken_EmptyClientID(t *testing.T) {
	s := NewStub()
	_, err := s.ServiceToken(newReqWithServiceToken("", "some-secret"))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// Also: missing secret → ErrUnauthenticated (covers the symmetric case).
func TestStub_ServiceToken_EmptySecret(t *testing.T) {
	s := NewStub()
	_, err := s.ServiceToken(newReqWithServiceToken("test-machine", ""))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// And: a non-fixture client-id with a real-looking secret → ErrUnauthenticated.
func TestStub_ServiceToken_UnknownClientID(t *testing.T) {
	s := NewStub()
	_, err := s.ServiceToken(newReqWithServiceToken("not-a-machine", "secret"))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// C-6: AccessJWT cookie test-jwt-clayton → Identity{Email, ClientID}.
func TestStub_AccessJWT_Clayton(t *testing.T) {
	s := NewStub()
	id, err := s.AccessJWT(newReqWithCookie("CF_Authorization", "test-jwt-clayton"))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if id.Email != "clayton@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "clayton@example.com")
	}
	if id.ClientID != "test-client-1" {
		t.Errorf("ClientID = %q, want %q", id.ClientID, "test-client-1")
	}
	if id.MachineID != "" {
		t.Errorf("MachineID unexpectedly set: %q", id.MachineID)
	}
}

// C-7: AccessJWT with any other cookie value → ErrUnauthenticated.
func TestStub_AccessJWT_OtherCookieValue(t *testing.T) {
	s := NewStub()
	_, err := s.AccessJWT(newReqWithCookie("CF_Authorization", "garbage"))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// C-8: AccessJWT with missing cookie → ErrUnauthenticated.
func TestStub_AccessJWT_MissingCookie(t *testing.T) {
	s := NewStub()
	_, err := s.AccessJWT(httptest.NewRequest(http.MethodGet, "/v1/client", nil))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// C-9: AccessJWT with wrong cookie name → ErrUnauthenticated.
func TestStub_AccessJWT_MalformedCookieName(t *testing.T) {
	s := NewStub()
	_, err := s.AccessJWT(newReqWithCookie("CF_Auth", "test-jwt-clayton"))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// C-10: test-jwt-expired fixture → ErrUnauthenticated. The plan calls this
// out explicitly to satisfy #19's "expired JWT" acceptance bullet at the
// stub level.
func TestStub_AccessJWT_ExpiredFixture(t *testing.T) {
	s := NewStub()
	for _, v := range []string{"test-jwt-expired", "test-jwt-malformed"} {
		v := v
		t.Run(v, func(t *testing.T) {
			_, err := s.AccessJWT(newReqWithCookie("CF_Authorization", v))
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("%s: err = %v, want ErrUnauthenticated", v, err)
			}
		})
	}
}
