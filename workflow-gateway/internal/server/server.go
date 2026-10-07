package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

type Config struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		Addr:            ":8080",
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    15 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 30 * time.Second,
	}
}

type Server struct {
	config Config
	router *chi.Mux
	ready  atomic.Bool
	server *http.Server
	store  *discovery.RouteStore
}

func New(config Config, store *discovery.RouteStore) *Server {
	router := chi.NewRouter()
	s := &Server{
		config: config,
		router: router,
		store:  store,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.router.Get("/health", s.healthHandler)
	s.router.Get("/ready", s.readyHandler)
	s.router.Get("/debug/routes", s.debugRoutesHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.IsReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (s *Server) debugRoutesHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snapshot := s.store.Snapshot()
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	logf.Log.WithName("server").Info("starting workflow gateway server", "addr", s.config.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

func (s *Server) IsReady() bool {
	return s.ready.Load()
}
