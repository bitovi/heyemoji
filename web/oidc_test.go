package web

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const testRealmPath = "/realms/bitovi"

// fakeKeycloak stands in for the platform Keycloak: it serves the token
// endpoint (returning whatever id_token the test sets) and the JWKS, so the
// code exchange and ID-token verification in handleCallback run for real.
type fakeKeycloak struct {
	server    *httptest.Server
	issuer    string
	key       *rsa.PrivateKey
	idToken   string
	gotCode   string
	gotClient string
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kc := &fakeKeycloak{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+testRealmPath+"/protocol/openid-connect/certs", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}}
		_ = json.NewEncoder(w).Encode(jwks)
	})
	mux.HandleFunc("POST "+testRealmPath+"/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		kc.gotCode = r.PostForm.Get("code")
		kc.gotClient, _, _ = r.BasicAuth()
		if kc.gotClient == "" {
			kc.gotClient = r.PostForm.Get("client_id")
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300}
		if kc.idToken != "" {
			resp["id_token"] = kc.idToken
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	kc.server = httptest.NewServer(mux)
	t.Cleanup(kc.server.Close)
	kc.issuer = kc.server.URL + testRealmPath
	return kc
}

type tokenOpts struct {
	key      *rsa.PrivateKey
	issuer   string
	audience string
	expiry   time.Time
}

func (kc *fakeKeycloak) sign(t *testing.T, claims map[string]any, opts tokenOpts) string {
	t.Helper()
	if opts.key == nil {
		opts.key = kc.key
	}
	if opts.issuer == "" {
		opts.issuer = kc.issuer
	}
	if opts.audience == "" {
		opts.audience = "heyemoji-sso"
	}
	if opts.expiry.IsZero() {
		opts.expiry = time.Now().Add(5 * time.Minute)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: opts.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	std := jwt.Claims{
		Issuer:   opts.issuer,
		Subject:  "user-1",
		Audience: jwt.Audience{opts.audience},
		IssuedAt: jwt.NewNumericDate(time.Now()),
		Expiry:   jwt.NewNumericDate(opts.expiry),
	}
	raw, err := jwt.Signed(signer).Claims(std).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newOIDCServer(kc *fakeKeycloak) *Server {
	return &Server{cfg: Config{
		BaseURL:          "https://heybitovi.test",
		SessionSecret:    "test-secret",
		OIDCIssuerURL:    kc.issuer,
		OIDCClientID:     "heyemoji-sso",
		OIDCClientSecret: "client-secret",
	}}
}

// signIn runs /auth/login, then /auth/callback with the state it set, and
// returns the callback response.
func signIn(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	s.handleLogin(login, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	loc, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=the-code&state="+url.QueryEscape(state), nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handleCallback(rec, req)
	return rec
}

func sessionEmail(s *Server, rec *httptest.ResponseRecorder) (string, bool) {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			claims, ok := s.verifySession(c.Value)
			return claims.Email, ok
		}
	}
	return "", false
}

var validClaims = map[string]any{"email": "dev@bitovi.com", "email_verified": true, "name": "Dev Person"}

func TestLogin_RedirectsToKeycloak(t *testing.T) {
	kc := newFakeKeycloak(t)
	s := newOIDCServer(kc)

	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loc.Scheme+"://"+loc.Host+loc.Path, kc.issuer+"/protocol/openid-connect/auth"; got != want {
		t.Errorf("authorize URL = %q, want %q", got, want)
	}
	q := loc.Query()
	if q.Get("client_id") != "heyemoji-sso" || q.Get("redirect_uri") != "https://heybitovi.test/auth/callback" ||
		q.Get("scope") != "openid email profile" || q.Get("state") == "" {
		t.Errorf("unexpected authorize params: %v", q)
	}
}

func TestCallback_ValidTokenSignsIn(t *testing.T) {
	kc := newFakeKeycloak(t)
	s := newOIDCServer(kc)
	kc.idToken = kc.sign(t, validClaims, tokenOpts{})

	rec := signIn(t, s)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/app/leaderboard" {
		t.Fatalf("got %d to %q, want 302 to /app/leaderboard (body: %s)", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if email, ok := sessionEmail(s, rec); !ok || email != "dev@bitovi.com" {
		t.Errorf("session email = %q (valid %v), want dev@bitovi.com", email, ok)
	}
	if kc.gotCode != "the-code" || kc.gotClient != "heyemoji-sso" {
		t.Errorf("token endpoint got code %q client %q, want the-code / heyemoji-sso", kc.gotCode, kc.gotClient)
	}
}

func TestCallback_RejectsBadTokens(t *testing.T) {
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		token  func(kc *fakeKeycloak) string
		status int
	}{
		{"no id_token", func(kc *fakeKeycloak) string { return "" }, http.StatusInternalServerError},
		{"unknown signing key", func(kc *fakeKeycloak) string { return kc.sign(t, validClaims, tokenOpts{key: otherKey}) }, http.StatusInternalServerError},
		{"wrong issuer", func(kc *fakeKeycloak) string {
			return kc.sign(t, validClaims, tokenOpts{issuer: "https://evil.example/realms/bitovi"})
		}, http.StatusInternalServerError},
		{"wrong audience", func(kc *fakeKeycloak) string { return kc.sign(t, validClaims, tokenOpts{audience: "other-client"}) }, http.StatusInternalServerError},
		{"expired", func(kc *fakeKeycloak) string {
			return kc.sign(t, validClaims, tokenOpts{expiry: time.Now().Add(-time.Minute)})
		}, http.StatusInternalServerError},
		{"email not verified", func(kc *fakeKeycloak) string {
			return kc.sign(t, map[string]any{"email": "dev@bitovi.com", "email_verified": false}, tokenOpts{})
		}, http.StatusForbidden},
		{"email_verified absent", func(kc *fakeKeycloak) string {
			return kc.sign(t, map[string]any{"email": "dev@bitovi.com"}, tokenOpts{})
		}, http.StatusForbidden},
		{"email empty", func(kc *fakeKeycloak) string {
			return kc.sign(t, map[string]any{"email": "", "email_verified": true}, tokenOpts{})
		}, http.StatusForbidden},
		{"email absent", func(kc *fakeKeycloak) string {
			return kc.sign(t, map[string]any{"email_verified": true}, tokenOpts{})
		}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kc := newFakeKeycloak(t)
			s := newOIDCServer(kc)
			kc.idToken = tc.token(kc)

			rec := signIn(t, s)

			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.status, rec.Body)
			}
			if _, ok := sessionEmail(s, rec); ok {
				t.Error("a session cookie was set, want none")
			}
		})
	}
}

func TestCallback_RejectsMismatchedState(t *testing.T) {
	kc := newFakeKeycloak(t)
	s := newOIDCServer(kc)
	kc.idToken = kc.sign(t, validClaims, tokenOpts{})

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=the-code&state=forged", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "real-state"})
	rec := httptest.NewRecorder()
	s.handleCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if kc.gotCode != "" {
		t.Error("the code was exchanged despite a state mismatch")
	}
}
