package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

type ManagementConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type ManagementServer struct {
	config  ManagementConfig
	router  *chi.Mux
	store   *discovery.RouteStore
	isReady func() bool
	server  *http.Server
}

func NewManagement(config ManagementConfig, store *discovery.RouteStore, isReady func() bool) *ManagementServer {
	router := chi.NewRouter()
	m := &ManagementServer{
		config:  config,
		router:  router,
		store:   store,
		isReady: isReady,
	}
	m.registerRoutes()
	return m
}

func (m *ManagementServer) registerRoutes() {
	m.router.Get("/health", m.healthHandler)
	m.router.Get("/ready", m.readyHandler)
	m.router.Get("/debug/routes", m.debugRoutesHandler)
}

func (m *ManagementServer) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *ManagementServer) readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !m.isReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (m *ManagementServer) debugRoutesHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.store.Snapshot())
}

func (m *ManagementServer) Start() error {
	m.server = &http.Server{
		Addr:         m.config.Addr,
		Handler:      m.router,
		ReadTimeout:  m.config.ReadTimeout,
		WriteTimeout: m.config.WriteTimeout,
	}
	logf.Log.WithName("management").Info("starting management server", "addr", m.config.Addr)
	return m.server.ListenAndServe()
}

func (m *ManagementServer) Shutdown(ctx context.Context) error {
	if m.server == nil {
		return nil
	}
	return m.server.Shutdown(ctx)
}
