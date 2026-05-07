package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/webui"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

type Server struct {
	port     int
	creds    *CredentialsManager
	jobs     *JobManager
	repo     RunRepository
	router   *chi.Mux
	upgrader websocket.Upgrader
}

func New(port int, credsFile string, logsDir string, dbPath string) (*Server, error) {
	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	s := &Server{
		port:  port,
		creds: NewCredentialsManager(credsFile),
		jobs:  NewJobManager(logsDir, repo),
		repo:  repo,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	s.router = chi.NewRouter()
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	s.router.Route("/api", func(r chi.Router) {
		r.Get("/connectors", s.handleGetConnectors)
		r.Get("/credentials", s.handleGetCredentials)
		r.Post("/credentials", s.handleSaveCredential)
		r.Delete("/credentials/{id}", s.handleDeleteCredential)
		r.Post("/run", s.handleRunJob)
		r.Get("/jobs/{id}", s.handleGetJob)
		r.Get("/runs", s.handleListRuns)
		r.Get("/runs/{id}", s.handleGetRun)
		r.Get("/runs/{id}/logs", s.handleGetRunLogs)
	})

	s.router.Get("/api/ws/logs/{id}", s.handleLogsWebSocket)

	distFS, err := fs.Sub(webui.DistFS, "dist")
	if err != nil {
		panic(fmt.Sprintf("failed to get embedded fs: %v", err))
	}
	fileServer := http.FileServer(http.FS(distFS))

	s.router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		if _, err := fs.Stat(distFS, strings.TrimPrefix(path, "/")); err != nil {
			path = "/index.html"
			r.URL.Path = path
		}

		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.creds.Load(); err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("Starting ingestr web UI on http://localhost%s\n", addr)

	server := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) handleGetConnectors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(GetConnectors())
}

func (s *Server) handleGetCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.creds.List())
}

func (s *Server) handleSaveCredential(w http.ResponseWriter, r *http.Request) {
	var cred Credential
	if err := json.NewDecoder(r.Body).Decode(&cred); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := s.creds.Add(cred)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.creds.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type RunJobRequest struct {
	SourceURI      string   `json:"sourceUri"`
	DestURI        string   `json:"destUri"`
	SourceTable    string   `json:"sourceTable"`
	DestTable      string   `json:"destTable"`
	Strategy       string   `json:"strategy"`
	PrimaryKeys    []string `json:"primaryKeys"`
	IncrementalKey string   `json:"incrementalKey"`
}

func (s *Server) handleRunJob(w http.ResponseWriter, r *http.Request) {
	var req RunJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := config.DefaultConfig()
	cfg.SourceURI = req.SourceURI
	cfg.DestURI = req.DestURI
	cfg.SourceTable = req.SourceTable
	cfg.DestTable = req.DestTable
	if cfg.DestTable == "" {
		cfg.DestTable = cfg.SourceTable
	}
	if req.Strategy != "" {
		cfg.IncrementalStrategy = config.IncrementalStrategy(req.Strategy)
	}
	cfg.PrimaryKeys = req.PrimaryKeys
	cfg.IncrementalKey = req.IncrementalKey
	cfg.Yes = true

	if err := cfg.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID, err := s.jobs.StartJob(r.Context(), cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"jobId": jobID})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := s.jobs.GetJob(id)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleLogsWebSocket(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	logChan := s.jobs.SubscribeLogs(jobID)
	defer s.jobs.UnsubscribeLogs(jobID, logChan)

	for log := range logChan {
		if err := conn.WriteJSON(log); err != nil {
			return
		}
	}
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	// Parse pagination params
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	runs, total, err := s.jobs.ListRunsPaginated(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []*RunRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"runs":   runs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := s.jobs.GetRun(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(run)
}

func (s *Server) handleGetRunLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	logs, err := s.jobs.GetRunLogs(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []*LogRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}
