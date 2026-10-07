package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

func main() {
	// Parse command-line flags
	var (
		bindAddr        string
		readTimeout     time.Duration
		writeTimeout    time.Duration
		idleTimeout     time.Duration
		shutdownTimeout time.Duration
	)

	flag.StringVar(&bindAddr, "bind-address", ":8080", "The address the server binds to.")
	flag.DurationVar(&readTimeout, "read-timeout", 15*time.Second, "Read timeout for HTTP connections.")
	flag.DurationVar(&writeTimeout, "write-timeout", 15*time.Second, "Write timeout for HTTP connections.")
	flag.DurationVar(&idleTimeout, "idle-timeout", 60*time.Second, "Idle timeout for HTTP connections.")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second, "Graceful shutdown timeout.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Set up structured logging
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	// Load Kubernetes config (in-cluster or from KUBECONFIG env)
	restCfg := ctrl.GetConfigOrDie()

	// Create scheme with base Kubernetes types and logic-operator CRDs
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(logicv1.AddToScheme(scheme))

	// Initialize discovery and route store
	store := discovery.NewRouteStore()
	disc := discovery.NewDiscovery(restCfg, store, scheme)

	// Create HTTP server with configured options
	serverCfg := server.Config{
		Addr:            bindAddr,
		ReadTimeout:     readTimeout,
		WriteTimeout:    writeTimeout,
		IdleTimeout:     idleTimeout,
		ShutdownTimeout: shutdownTimeout,
	}
	srv := server.New(serverCfg, store)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Create main context (cancelled on shutdown signal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start discovery in background
	go func() {
		if err := disc.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			setupLog.Error(err, "discovery failed")
			os.Exit(1)
		}
	}()

	// Start HTTP server in background. ListenAndServe returns ErrServerClosed on
	// graceful shutdown, which is not an error.
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			setupLog.Error(err, "server failed")
			os.Exit(1)
		}
	}()

	// Wait for discovery cache sync before marking ready. Startup fails if sync
	// does not complete, ensuring the routing table is populated before the
	// gateway advertises readiness.
	discCtx, discCancel := context.WithTimeout(ctx, 30*time.Second)
	if !disc.WaitForCacheSync(discCtx) {
		discCancel()
		setupLog.Error(nil, "cache sync timeout: gateway requires a synced routing table to serve requests")
		os.Exit(1)
	}
	discCancel()

	setupLog.Info("workflow gateway ready; cache synchronized")
	srv.SetReady(true)

	// Wait for shutdown signal
	<-sigChan
	setupLog.Info("shutdown signal received, draining requests")
	srv.SetReady(false)

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), serverCfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "shutdown error")
		os.Exit(1)
	}

	cancel() // stop discovery
	setupLog.Info("server shut down gracefully")
}
