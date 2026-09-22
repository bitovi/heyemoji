package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
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

func (s *Server) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.GoogleClientID,
		ClientSecret: s.cfg.GoogleClientSecret,
		RedirectURL:  s.cfg.BaseURL + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func (s *Server) secureCookies() bool {
	return strings.HasPrefix(s.cfg.BaseURL, "https://")
}

func (s *Server) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("web: google oauth exchange failed: %v", err)
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}

	info, err := fetchGoogleUserInfo(r.Context(), s.oauthConfig().Client(r.Context(), token))
	if err != nil {
		log.Printf("web: fetching google userinfo failed: %v", err)
		http.Error(w, "Sign-in failed.", http.StatusInternalServerError)
		return
	}

	if s.cfg.GoogleAllowedDomain != "" && !strings.EqualFold(info.HD, s.cfg.GoogleAllowedDomain) {
		log.Printf("web: rejected sign-in from %s (domain %q, want %q)", info.Email, info.HD, s.cfg.GoogleAllowedDomain)
		http.Error(w, fmt.Sprintf("Sign-in is restricted to %s Google accounts.", s.cfg.GoogleAllowedDomain), http.StatusForbidden)
		return
	}

	s.setSession(w, info.Email)
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

type googleUserInfo struct {
	Email string `json:"email"`
	HD    string `json:"hd"` // Google Workspace hosted domain, empty for personal @gmail.com accounts
	Name  string `json:"name"`
}

func fetchGoogleUserInfo(ctx context.Context, client *http.Client) (*googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d", resp.StatusCode)
	}
	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
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
			http.Redirect(w, r, "/auth/google/login", http.StatusFound)
			return
		}
		claims, ok := s.verifySession(cookie.Value)
		if !ok {
			http.Redirect(w, r, "/auth/google/login", http.StatusFound)
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
