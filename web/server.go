// Package web implements the read-only HeyBitovi web UI (leaderboard + recognition
// feed), served from the same binary/container as the Slack bot. Access is gated by
// Google Sign-In (OAuth2), not Slack - Bitovi already has Google OAuth set up, so this
// reuses that instead of building a separate Slack OIDC flow.
package web

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"
	"sync"

	"github.com/slack-go/slack"

	"github.com/bitovi/heyemoji/database"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Config struct {
	BaseURL             string
	SessionSecret       string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleAllowedDomain string // if set, only Google Workspace accounts on this domain (the "hd" claim) may sign in
	MaxLeaderEntries    int
	// AuthDisabled skips Google Sign-In entirely and serves pages to anyone - for
	// local testing, never for a deployment reachable by anyone but you.
	AuthDisabled bool
}

func New(cfg Config, db database.Driver, slackClient *slack.Client) *Server {
	return &Server{
		cfg:       cfg,
		db:        db,
		slack:     slackClient,
		templates: template.Must(template.ParseFS(templatesFS, "templates/*.html")),
		userCache: map[string]string{},
	}
}

type Server struct {
	cfg       Config
	db        database.Driver
	slack     *slack.Client
	templates *template.Template

	mu        sync.Mutex
	userCache map[string]string // Slack user ID -> display name, lazily cached
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /auth/google/login", s.handleGoogleLogin)
	mux.HandleFunc("GET /auth/google/callback", s.handleGoogleCallback)
	mux.HandleFunc("GET /auth/logout", s.handleLogout)
	mux.Handle("GET /app/leaderboard", s.requireAuth(http.HandlerFunc(s.handleLeaderboard)))
	mux.Handle("GET /app/feed", s.requireAuth(http.HandlerFunc(s.handleFeed)))
	mux.Handle("GET /app/details", s.requireAuth(http.HandlerFunc(s.handleDetails)))
	mux.HandleFunc("/", s.handleRoot) // catch-all: anything not matched above (unknown path, wrong method, etc.)
	return mux
}

// Run starts the HTTP server on port, blocking until ctx is done or it fails.
func (s *Server) Run(ctx context.Context, port string) error {
	srv := &http.Server{Addr: ":" + port, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	log.Printf("web: listening on :%s", port)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleRoot is both the "/" handler and the catch-all for any path/method not
// matched by a more specific route above it in Handler() - unknown paths land on the
// leaderboard rather than a bare 404.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/leaderboard", http.StatusFound)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("web: render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// resolveDisplayName maps a Slack user ID to a display name via the Slack API,
// caching results for the life of the process (same pattern as the give command's
// username lookup - small enough not to share, kept local to this package).
func (s *Server) resolveDisplayName(ctx context.Context, userID string) string {
	s.mu.Lock()
	if name, ok := s.userCache[userID]; ok {
		s.mu.Unlock()
		return name
	}
	s.mu.Unlock()

	name := userID
	if info, err := s.slack.GetUserInfoContext(ctx, userID); err == nil && info.RealName != "" {
		name = info.RealName
	}

	s.mu.Lock()
	s.userCache[userID] = name
	s.mu.Unlock()
	return name
}
