package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireAuth_RedirectsWithoutSession(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}
	called := false
	handler := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/leaderboard", nil))

	if called {
		t.Error("handler was called without a valid session, want it blocked")
	}
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/google/login" {
		t.Errorf("got status %d location %q, want 302 to /auth/google/login", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRequireAuth_AllowsValidSession(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}
	var gotEmail string
	handler := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = emailFromContext(r.Context())
	}))

	token := s.signSession(sessionClaims{Email: "phil@bitovi.com", Exp: time.Now().Add(time.Hour).Unix()})
	req := httptest.NewRequest(http.MethodGet, "/app/leaderboard", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotEmail != "phil@bitovi.com" {
		t.Errorf("email in context = %q, want phil@bitovi.com", gotEmail)
	}
}

func TestRequireAuth_BypassedWhenDisabled(t *testing.T) {
	// HEY_WEB_AUTH_DISABLED bypasses the login redirect entirely, even with no
	// session cookie at all - this is the local-testing escape hatch, so it must
	// actually let requests through unauthenticated.
	s := &Server{cfg: Config{AuthDisabled: true}}
	called := false
	handler := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/leaderboard", nil))

	if !called {
		t.Error("handler was not called with AuthDisabled:true and no session, want it allowed through")
	}
	if rec.Code == http.StatusFound {
		t.Error("got a redirect with AuthDisabled:true, want the request to pass through")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}

	claims := sessionClaims{Email: "phil@bitovi.com", Exp: time.Now().Add(time.Hour).Unix()}
	token := s.signSession(claims)

	got, ok := s.verifySession(token)
	if !ok {
		t.Fatal("verifySession() = false, want true for a freshly signed token")
	}
	if got.Email != claims.Email {
		t.Errorf("Email = %q, want %q", got.Email, claims.Email)
	}
}

func TestSessionRejectsTampering(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}

	token := s.signSession(sessionClaims{Email: "phil@bitovi.com", Exp: time.Now().Add(time.Hour).Unix()})

	// Mutate the last character of a validly-signed token so the signature no longer
	// matches its payload - this is what an attacker trying to forge a different
	// email without knowing SessionSecret would produce.
	mutated := token[:len(token)-1] + "x"
	if _, ok := s.verifySession(mutated); ok {
		t.Error("verifySession() = true for a mutated token, want false")
	}
}

func TestSessionRejectsWrongSecret(t *testing.T) {
	signer := &Server{cfg: Config{SessionSecret: "secret-a"}}
	verifier := &Server{cfg: Config{SessionSecret: "secret-b"}}

	token := signer.signSession(sessionClaims{Email: "phil@bitovi.com", Exp: time.Now().Add(time.Hour).Unix()})
	if _, ok := verifier.verifySession(token); ok {
		t.Error("verifySession() with a different secret = true, want false")
	}
}

func TestSessionRejectsExpired(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}

	token := s.signSession(sessionClaims{Email: "phil@bitovi.com", Exp: time.Now().Add(-time.Minute).Unix()})
	if _, ok := s.verifySession(token); ok {
		t.Error("verifySession() for an expired token = true, want false")
	}
}

func TestSessionRejectsMalformed(t *testing.T) {
	s := &Server{cfg: Config{SessionSecret: "test-secret"}}

	for _, bad := range []string{"", "no-dot-here", "a.b.c", "onlyonepart"} {
		if _, ok := s.verifySession(bad); ok {
			t.Errorf("verifySession(%q) = true, want false", bad)
		}
	}
}
