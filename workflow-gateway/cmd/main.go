package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

func main() {
	cfg := server.DefaultConfig()
	s := server.New(cfg)
	s.SetReady(true)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		if err := s.Start(); err != nil {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutdown signal received, draining requests...")
	s.SetReady(false)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
	log.Println("Server shut down gracefully")
}
