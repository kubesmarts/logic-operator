package main

import (
	"log"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

func main() {
	cfg := server.DefaultConfig()
	s := server.New(cfg)
	s.SetReady(true)
	if err := s.Start(); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
