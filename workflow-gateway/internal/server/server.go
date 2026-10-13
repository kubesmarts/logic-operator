package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
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

// Handler returns the underlying HTTP handler, allowing the server to be
// wrapped by httptest.NewServer in integration tests.
func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) registerRoutes() {
	s.router.Route("/v1", func(r chi.Router) {
		r.Route("/definitions", func(r chi.Router) {
			r.Get("/", s.listDefinitionsHandler)
			r.Get("/{namespace}/{name}/{version}", s.getDefinitionHandler)
		})
		r.Route("/instances", func(r chi.Router) {
			r.Get("/", s.listInstancesHandler)
			r.Get("/{id}", s.getInstanceHandler)
		})
		r.Route("/runtime/{namespace}/{name}", func(r chi.Router) {
			r.Post("/", s.executeHandler)
			r.Post("/instance/{id}/suspend", s.suspendHandler)
			r.Post("/instance/{id}/resume", s.resumeHandler)
			r.Post("/instance/{id}/cancel", s.cancelHandler)
			r.Post("/{version}", s.executeHandler)
			r.Post("/{version}/instance/{id}/suspend", s.suspendHandler)
			r.Post("/{version}/instance/{id}/resume", s.resumeHandler)
			r.Post("/{version}/instance/{id}/cancel", s.cancelHandler)
		})
		r.Get("/openapi.json", s.openapiHandler)
	})
}

func notImplemented(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not implemented"})
}

func workflowNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "workflow not found"})
}

// Definitions

// listDefinitionsHandler returns the workflow catalog from the in-memory
// routing table. The upstream URL is an internal routing detail and is
// intentionally omitted from the response.
func (s *Server) listDefinitionsHandler(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.store.Snapshot()
	keys := make([]discovery.WorkflowKey, 0, len(snapshot))
	for _, r := range snapshot {
		keys = append(keys, r.WorkflowKey)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keys)
}

// getDefinitionHandler is a stub. Returning a specific definition requires the
// full flow document, which is not cached in memory (LogicFlowDefinition.Spec
// is stripped by the informer transform to reduce memory footprint). A live
// K8s API call is needed; tracked as a future issue.
func (s *Server) getDefinitionHandler(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w)
}

// Instances — stub: routes to data-index (future issue)

func (s *Server) listInstancesHandler(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w)
}

func (s *Server) getInstanceHandler(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w)
}

// Runtime — resolves upstream URL via routing table; proxy is a future issue.

// resolveWorkflow looks up the workflow from the routing table.
// Returns false (and writes the error response) if not found or on any error.
func (s *Server) resolveWorkflow(w http.ResponseWriter, r *http.Request) bool {
	_, err := routing.Resolve(r, s.store)
	if err == nil {
		return true
	}
	if errors.Is(err, routing.ErrWorkflowNotFound) {
		workflowNotFound(w)
	} else {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	return false
}

func (s *Server) executeHandler(w http.ResponseWriter, r *http.Request) {
	if !s.resolveWorkflow(w, r) {
		return
	}
	notImplemented(w)
}

func (s *Server) suspendHandler(w http.ResponseWriter, r *http.Request) {
	if !s.resolveWorkflow(w, r) {
		return
	}
	notImplemented(w)
}

func (s *Server) resumeHandler(w http.ResponseWriter, r *http.Request) {
	if !s.resolveWorkflow(w, r) {
		return
	}
	notImplemented(w)
}

func (s *Server) cancelHandler(w http.ResponseWriter, r *http.Request) {
	if !s.resolveWorkflow(w, r) {
		return
	}
	notImplemented(w)
}

func (s *Server) openapiHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(openapiJSON)
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
