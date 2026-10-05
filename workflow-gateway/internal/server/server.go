package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Addr:         ":8080",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

type Server struct {
	config Config
	router *chi.Mux
	ready  bool
}

func New(config Config) *Server {
	router := chi.NewRouter()
	s := &Server{
		config: config,
		router: router,
		ready:  false,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.router.Get("/health", s.healthHandler)
	s.router.Get("/ready", s.readyHandler)
}

func (s *Server) Start() error {
	server := &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	fmt.Printf("Starting workflow gateway server on %s\n", s.config.Addr)
	return server.ListenAndServe()
}

func (s *Server) SetReady(ready bool) {
	s.ready = ready
}
