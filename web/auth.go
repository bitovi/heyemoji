package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookieName = "heybitovi_session"
	stateCookieName   = "heybitovi_oauth_state"
	sessionDuration   = 7 * 24 * time.Hour
)

type sessionClaims struct {
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
}

type contextKey struct{ name string }

var emailContextKey = contextKey{"email"}

// Sign-in is OIDC against the platform Keycloak (bitovi realm), which brokers
// Google and restricts it to bitovi.com accounts (hostedDomain on the realm's
// Google IdP) - so there's no domain check here. The endpoints are Keycloak's
// standard paths under the issuer URL rather than fetched from discovery, so
// startup never depends on Keycloak being reachable.
func (s *Server) oidcProtocolURL(path string) string {
	return s.cfg.OIDCIssuerURL + "/protocol/openid-connect/" + path
}

func (s *Server) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.OIDCClientID,
		ClientSecret: s.cfg.OIDCClientSecret,
		RedirectURL:  s.cfg.BaseURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  s.oidcProtocolURL("auth"),
			TokenURL: s.oidcProtocolURL("token"),
		},
	}
}

// idTokenVerifier checks the ID token's signature against Keycloak's JWKS
// (fetched lazily and cached by kid), its issuer, audience, and expiry.
func (s *Server) idTokenVerifier() *oidc.IDTokenVerifier {
	s.verifierOnce.Do(func() {
		keySet := oidc.NewRemoteKeySet(context.Background(), s.oidcProtocolURL("certs"))
		s.verifier = oidc.NewVerifier(s.cfg.OIDCIssuerURL, keySet, &oidc.Config{ClientID: s.cfg.OIDCClientID})
	})
	return s.verifier
}

func (s *Server) secureCookies() bool {
	return strings.HasPrefix(s.cfg.BaseURL, "https://")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, s.oauthConfig().AuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || r.URL.Query().Get("state") == "" || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "Invalid or expired sign-in attempt - please try again.", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Sign-in was cancelled or did not return a code.", http.StatusBadRequest)
		return
	}

	token, err := s.oauthConfig().Exchange(r.Context(), code)
	if err != nil {
		log.Printf("web: oidc code exchange failed: %v", err)
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}

	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		log.Printf("web: oidc token response had no id_token")
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}
	idToken, err := s.idTokenVerifier().Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Printf("web: id_token verification failed: %v", err)
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}

	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		log.Printf("web: decoding id_token claims failed: %v", err)
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}
	if claims.Email == "" || !claims.EmailVerified {
		log.Printf("web: rejected sign-in for %q (email missing or not verified)", claims.Email)
		http.Error(w, "Sign-in failed: email address is missing or not verified.", http.StatusForbidden)
		return
	}

	// The state cookie is single-use.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	s.setSession(w, claims.Email)
	http.Redirect(w, r, "/app/leaderboard", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

type idTokenClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func (s *Server) setSession(w http.ResponseWriter, email string) {
	claims := sessionClaims{Email: email, Exp: time.Now().Add(sessionDuration).Unix()}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.signSession(claims),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionDuration),
	})
}

// signSession produces a stateless, HMAC-signed session token - no server-side session
// store needed. There's a real trade-off here: this can't support "sign out
// everywhere" revocation. Acceptable for a small internal, low-sensitivity tool.
func (s *Server) signSession(claims sessionClaims) string {
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(payloadB64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadB64 + "." + sig
}

func (s *Server) verifySession(value string) (sessionClaims, bool) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return sessionClaims{}, false
	}

	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return sessionClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionClaims{}, false
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, false
	}
	if time.Now().Unix() > claims.Exp {
		return sessionClaims{}, false
	}
	return claims, true
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthDisabled {
			ctx := context.WithValue(r.Context(), emailContextKey, "test-mode (auth disabled)")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		claims, ok := s.verifySession(cookie.Value)
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), emailContextKey, claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func emailFromContext(ctx context.Context) string {
	email, _ := ctx.Value(emailContextKey).(string)
	return email
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
