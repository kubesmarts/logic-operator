package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/catalog"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/dataindex"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/kube"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/lease"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

func main() {
	// Parse command-line flags
	var (
		bindAddr        string
		managementAddr  string
		readTimeout     time.Duration
		writeTimeout    time.Duration
		idleTimeout     time.Duration
		shutdownTimeout time.Duration
	)

	flag.StringVar(&bindAddr, "bind-address", ":"+strconv.Itoa(int(logicv1.WorkflowGatewayHTTPPort)), "The address the server binds to.")
	flag.StringVar(&managementAddr, "management-bind-address", ":"+strconv.Itoa(int(logicv1.WorkflowGatewayManagementPort)), "The address the management server binds to.")
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

	operatorNamespace := os.Getenv("POD_NAMESPACE")
	if operatorNamespace == "" {
		operatorNamespace = "default"
		setupLog.Info("POD_NAMESPACE not set, defaulting", "namespace", operatorNamespace)
	}

	// Load Kubernetes config (in-cluster or from KUBECONFIG env)
	restCfg := ctrl.GetConfigOrDie()

	// Create scheme with base Kubernetes types and logic-operator CRDs
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(logicv1.AddToScheme(scheme))

	// Build the shared controller-runtime manager. Its cache is shared by all
	// watchers; the flow document is stripped from LogicFlowDefinitions while
	// RuntimeRef and workflow identity labels remain available for routing.
	mgr, err := kube.NewManager(restCfg, scheme, map[client.Object]crcache.ByObject{
		&logicv1.LogicFlowDefinition{}: {Transform: catalog.StripDefinitionSpec},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Routing core: execution endpoint table + instance cache + lease->pod resolver.
	resolver := routing.NewResolver(
		routing.NewExecutionEndpointStore(),
		routing.NewInstanceStore(),
		lease.NewResolver(mgr.GetClient()),
	)

	// Platform store + Data Index client.
	platformStore := platform.NewPlatformStore()
	diClient := dataindex.NewClient(platformStore)

	// On-demand reader for full LogicFlowDefinition documents.
	defReader := definitions.NewReader(mgr.GetCache(), mgr.GetAPIReader())

	// Register watchers as manager runnables (share the manager's cache).
	catalogWatcher := catalog.NewWatcher(mgr.GetCache(), resolver)
	if err := mgr.Add(catalogWatcher); err != nil {
		setupLog.Error(err, "unable to register catalog watcher")
		os.Exit(1)
	}
	if err := mgr.Add(platform.NewPlatformWatcher(mgr.GetCache(), platformStore, operatorNamespace)); err != nil {
		setupLog.Error(err, "unable to register platform watcher")
		os.Exit(1)
	}

	// Create HTTP server with configured options
	serverCfg := server.Config{
		Addr:            bindAddr,
		ReadTimeout:     readTimeout,
		WriteTimeout:    writeTimeout,
		IdleTimeout:     idleTimeout,
		ShutdownTimeout: shutdownTimeout,
	}
	srv := server.New(serverCfg, resolver, defReader, diClient)

	// Create management server
	mgmtCfg := server.ManagementConfig{
		Addr:            managementAddr,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
	mgmtSrv := server.NewManagement(mgmtCfg, resolver, srv.IsReady)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Create main context (cancelled on shutdown signal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the shared manager (cache + catalog & platform watchers) in background.
	go func() {
		if err := mgr.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			setupLog.Error(err, "manager failed")
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

	// Start management server in background
	go func() {
		if err := mgmtSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			setupLog.Error(err, "management server failed")
			os.Exit(1)
		}
	}()

	// Wait for the shared cache to sync and the routing table to populate before
	// marking ready. Startup fails if sync
	// does not complete, ensuring the routing table is populated before the
	// gateway advertises readiness.
	readyCtx, readyCancel := context.WithTimeout(ctx, 30*time.Second)
	if !mgr.WaitForCacheSync(readyCtx) {
		readyCancel()
		setupLog.Error(nil, "cache sync timeout: gateway requires a synced cache to serve requests")
		os.Exit(1)
	}
	if !catalogWatcher.WaitForInitialSync(readyCtx) {
		readyCancel()
		setupLog.Error(nil, "routing table not populated before readiness timeout")
		os.Exit(1)
	}
	readyCancel()

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

	if err := mgmtSrv.Shutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "management server shutdown error")
	}

	cancel() // stop the manager and its watchers
	setupLog.Info("server shut down gracefully")
}
