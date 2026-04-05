package server

import (
	"fmt"
	"net/http"
	"strings"
)

// HubServer is the Loom Hub HTTP server.
type HubServer struct {
	projects *ProjectStore
	users    *UserStore
	mux      *http.ServeMux
}

// NewHubServer creates a new hub server storing data under dataDir.
func NewHubServer(dataDir string) (*HubServer, error) {
	projects := NewProjectStore(dataDir)

	users, err := NewUserStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("init user store: %w", err)
	}

	s := &HubServer{
		projects: projects,
		users:    users,
		mux:      http.NewServeMux(),
	}

	// Auth route (no auth required).
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	// Sync routes (auth required).
	s.mux.HandleFunc("POST /{owner}/{name}/api/v1/negotiate", s.requireAuth(s.handleNegotiate))
	s.mux.HandleFunc("POST /{owner}/{name}/api/v1/push", s.requireAuth(s.handlePush))
	s.mux.HandleFunc("POST /{owner}/{name}/api/v1/pull", s.requireAuth(s.handlePull))

	return s, nil
}

// Handler returns the HTTP handler for the hub server.
func (s *HubServer) Handler() http.Handler {
	return s.mux
}

// Start starts the HTTP server on the given address.
func (s *HubServer) Start(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

// Close releases all resources held by the server.
func (s *HubServer) Close() {
	s.projects.Close()
	s.users.Close()
}

// requireAuth wraps a handler with Bearer token validation.
func (s *HubServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		_, err := s.users.ValidateToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token: "+err.Error())
			return
		}

		next(w, r)
	}
}
