package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	loom "github.com/constructspace/loom/pkg/loom"
)

// AgentServer serves the Loom agent HTTP API.
type AgentServer struct {
	client *loom.Client
	server *http.Server
	mux    *http.ServeMux
}

// NewAgentServer creates a new agent HTTP server bound to the given port.
func NewAgentServer(client *loom.Client, port int) *AgentServer {
	mux := http.NewServeMux()

	s := &AgentServer{
		client: client,
		mux:    mux,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: jsonMiddleware(mux),
		},
	}

	mux.HandleFunc("POST /api/v1/checkpoint", s.handleCheckpoint)
	mux.HandleFunc("POST /api/v1/rollback", s.handleRollback)
	mux.HandleFunc("GET /api/v1/diff", s.handleDiff)
	mux.HandleFunc("GET /api/v1/log", s.handleLog)
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("GET /api/v1/search", s.handleSearch)

	return s
}

// Handler returns the underlying http.Handler for use with httptest.
func (s *AgentServer) Handler() http.Handler {
	return jsonMiddleware(s.mux)
}

// Start begins serving HTTP requests. It blocks until the context is cancelled
// or an error occurs. On context cancellation it performs a graceful shutdown.
func (s *AgentServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.server.Serve(ln)
	}()

	fmt.Fprintf(os.Stdout, "Agent API listening on %s\n", ln.Addr().String())

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		return s.Stop()
	}
}

// StartWithSignals starts the server and handles OS signals for graceful shutdown.
func (s *AgentServer) StartWithSignals() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.Start(ctx)
}

// Stop gracefully shuts down the server.
func (s *AgentServer) Stop() error {
	return s.server.Shutdown(context.Background())
}

// jsonMiddleware sets the Content-Type header to application/json for all responses.
func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- Handlers ---

type checkpointRequest struct {
	Title   string            `json:"title"`
	Summary string            `json:"summary,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
	Author  string            `json:"author,omitempty"`
}

func (s *AgentServer) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	var req checkpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var opts []loom.CheckpointOption
	if req.Summary != "" {
		opts = append(opts, loom.WithSummary(req.Summary))
	}
	if req.Author != "" {
		opts = append(opts, loom.WithAuthor(req.Author))
	}
	for k, v := range req.Tags {
		opts = append(opts, loom.WithTag(k, v))
	}

	cp, err := s.client.Checkpoint(req.Title, opts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cp)
}

type rollbackRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	Scope        string `json:"scope"`    // "full" (default) or "entity"
	EntityID     string `json:"entity_id"` // required when scope is "entity"
}

func (s *AgentServer) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CheckpointID == "" {
		writeError(w, http.StatusBadRequest, "checkpoint_id is required")
		return
	}

	var err error
	switch req.Scope {
	case "entity":
		if req.EntityID == "" {
			writeError(w, http.StatusBadRequest, "entity_id is required for entity scope")
			return
		}
		err = s.client.RollbackEntity(req.CheckpointID, req.EntityID)
	default:
		err = s.client.Rollback(req.CheckpointID)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *AgentServer) handleDiff(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = "HEAD~1"
	}
	if to == "" {
		to = "HEAD"
	}

	result, err := s.client.Diff(from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *AgentServer) handleLog(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	checkpoints, err := s.client.Log(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, checkpoints)
}

func (s *AgentServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	result, err := s.client.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *AgentServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query parameter is required")
		return
	}

	results, err := s.client.Search(q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
