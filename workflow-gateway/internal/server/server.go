package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/dataindex"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
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
		Addr:            ":" + strconv.Itoa(int(logicv1.WorkflowGatewayHTTPPort)),
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    15 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 30 * time.Second,
	}
}

type Server struct {
	config      Config
	router      *chi.Mux
	ready       atomic.Bool
	server      *http.Server
	resolver    *routing.Resolver
	definitions *definitions.Reader
	diClient    *dataindex.Client
}

func New(config Config, resolver *routing.Resolver, defReader *definitions.Reader, diClient *dataindex.Client) *Server {
	router := chi.NewRouter()
	s := &Server{
		config:      config,
		router:      router,
		resolver:    resolver,
		definitions: defReader,
		diClient:    diClient,
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
			r.Post("/", s.executeAsyncHandler)
			r.Post("/sync", s.executeSyncHandler)
			r.Post("/instance/{id}/suspend", s.suspendHandler)
			r.Post("/instance/{id}/resume", s.resumeHandler)
			r.Post("/instance/{id}/cancel", s.cancelHandler)
			r.Post("/{version}", s.executeAsyncHandler)
			r.Post("/{version}/sync", s.executeSyncHandler)
			r.Post("/{version}/instance/{id}/suspend", s.suspendHandler)
			r.Post("/{version}/instance/{id}/resume", s.resumeHandler)
			r.Post("/{version}/instance/{id}/cancel", s.cancelHandler)
		})
		r.Get("/openapi.json", s.openapiHandler)
		r.Get("/openapi", s.openapiHandler)
	})
}

func workflowNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "workflow not found"})
}

// Definitions

// listDefinitionsHandler returns the workflow catalog from the in-memory
// routing table.
func (s *Server) listDefinitionsHandler(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.resolver.ExecutionEndpointsSnapshot()
	keys := make([]routing.WorkflowKey, 0, len(snapshot))
	for _, r := range snapshot {
		keys = append(keys, r.WorkflowKey)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keys)
}

// getDefinitionHandler fetches a specific flow document on demand. The informer
// cache omits LogicFlowDefinition.Spec.Flow to reduce memory use, so the reader
// retrieves the full resource from the API server when this endpoint is called.
func (s *Server) getDefinitionHandler(w http.ResponseWriter, r *http.Request) {
	ns, name, version := workflowParams(r)

	flowDef, err := s.definitions.FetchDefinition(r.Context(), ns, name, version)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, definitions.ErrDefinitionNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "workflow definition not found"})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	accept := r.Header.Get("Accept")

	w.Header().Set("Content-Type", "application/json") // default

	if strings.Contains(accept, "application/yaml") || strings.Contains(accept, "application/x-yaml") {
		w.Header().Set("Content-Type", "application/yaml")
		_ = yaml.NewEncoder(w).Encode(flowDef.Spec.Flow)
	} else {
		_ = json.NewEncoder(w).Encode(flowDef.Spec.Flow)
	}
}

// Instances — routes to data-index

func (s *Server) listInstancesHandler(w http.ResponseWriter, r *http.Request) {
	if s.diClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "data index not available"})
		return
	}
	q := r.URL.Query()
	filter := dataindex.InstanceFilter{
		Namespace: q.Get("namespace"),
		Name:      q.Get("name"),
		Version:   q.Get("version"),
		Status:    q.Get("status"),
	}
	instances, err := s.diClient.ListInstances(r.Context(), filter)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		if errors.Is(err, dataindex.ErrEndpointNotDiscovered) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "data index not available"})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "upstream error"})
		return
	}
	_ = json.NewEncoder(w).Encode(instances)
}

func (s *Server) getInstanceHandler(w http.ResponseWriter, r *http.Request) {
	if s.diClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "data index not available"})
		return
	}
	id := chi.URLParam(r, "id")
	inst, err := s.diClient.GetInstance(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		if errors.Is(err, dataindex.ErrEndpointNotDiscovered) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "data index not available"})
			return
		}
		if errors.Is(err, dataindex.ErrInstanceNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "instance not found"})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "upstream error"})
		return
	}
	_ = json.NewEncoder(w).Encode(inst)
}

// Runtime — resolves upstream URL via routing table and proxies to runner.

// resolveWorkflow confirms the workflow exists in the routing table.
func (s *Server) resolveWorkflow(w http.ResponseWriter, r *http.Request) (routing.URL, bool) {
	ns, name, version := workflowParams(r)
	execURL, err := s.resolver.ResolveExecution(ns, name, version)
	if err == nil {
		return execURL, true
	}
	if errors.Is(err, routing.ErrWorkflowNotFound) {
		workflowNotFound(w)
	} else {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	return "", false
}

// resolveInstance resolves the destination for an instance operation, trying
// lease->pod routing with a fallback to service routing.
func (s *Server) resolveInstance(w http.ResponseWriter, r *http.Request) (routing.Destination, bool) {
	ns, name, _ := workflowParams(r)
	id := instanceID(r)
	dest, err := s.resolver.ResolveInstance(r.Context(), ns, name, id)
	if err != nil {
		workflowNotFound(w)
		return routing.Destination{}, false
	}
	return dest, true
}

// proxyInstanceOperation proxies an instance operation (suspend/resume/cancel) to its resolved destination.
// Gateway path: /v1/runtime/{namespace}/{name}[/{version}]/instance/{id}/{operation}
// Runner path: /q/flow/exec/{namespace}/{name}[/{version}]/{id}/{operation}
func (s *Server) proxyInstanceOperation(w http.ResponseWriter, r *http.Request, dest routing.Destination) {
	target, err := url.Parse(string(dest.URL))
	if err != nil {
		http.Error(w, "invalid destination URL", http.StatusInternalServerError)
		return
	}
	ns, name, version := workflowParams(r)
	id := instanceID(r)

	// Extract operation (suspend/resume/cancel) from request URL
	parts := strings.Split(r.URL.Path, "/")
	operation := parts[len(parts)-1]

	runnerPath := constructRunnerPath(ns, name, version, id, operation)
	proxy := newReverseProxy(target, runnerPath)
	proxy.ServeHTTP(w, r)
}

// executeAsyncHandler proxies async workflow execution to the runner and caches the instance.
// Returns 202 Accepted with instanceId and workflowApplicationId (pod name).
func (s *Server) executeAsyncHandler(w http.ResponseWriter, r *http.Request) {
	execURL, ok := s.resolveWorkflow(w, r)
	if !ok {
		return
	}
	target, err := url.Parse(string(execURL))
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusInternalServerError)
		return
	}
	ns, name, version := workflowParams(r)

	runnerPath := constructRunnerPath(ns, name, version)
	proxy := newReverseProxyWithQuery(target, runnerPath, "wait=false")
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusAccepted {
			// Parse response to extract instanceId and workflowApplicationId (pod name)
			var result map[string]interface{}
			body, _ := io.ReadAll(resp.Body)
			if err := json.Unmarshal(body, &result); err == nil {
				if instanceID, ok := result["instanceId"].(string); ok {
					if podName, ok := result["workflowApplicationId"].(string); ok {
						// Cache the instance with the pod name (lease holder identity)
						s.resolver.CacheInstance(instanceID, podName)
					}
				}
			}
			// Restore body for client response
			resp.Body = io.NopCloser(strings.NewReader(formatAsyncResponse(result)))
		}
		return nil
	}
	proxy.ServeHTTP(w, r)
}

// executeSyncHandler proxies sync workflow execution to the runner.
// Returns 200 OK with workflow output. No caching needed (instance is transient).
func (s *Server) executeSyncHandler(w http.ResponseWriter, r *http.Request) {
	execURL, ok := s.resolveWorkflow(w, r)
	if !ok {
		return
	}
	target, err := url.Parse(string(execURL))
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusInternalServerError)
		return
	}
	ns, name, version := workflowParams(r)

	runnerPath := constructRunnerPath(ns, name, version)
	proxy := newReverseProxyWithQuery(target, runnerPath, "wait=true")
	proxy.ServeHTTP(w, r)
}

// constructRunnerPath builds the runner API path for exec/instance operations.
// Format: /q/flow/exec/{namespace}/{name}[/{version}][/{id}[/{operation}]]
func constructRunnerPath(namespace, name, version string, parts ...string) string {
	var path strings.Builder
	path.WriteString("/q/flow/exec/")
	path.WriteString(namespace)
	path.WriteString("/")
	path.WriteString(name)
	if version != "" {
		path.WriteString("/")
		path.WriteString(version)
	}
	for _, part := range parts {
		if part != "" {
			path.WriteString("/")
			path.WriteString(part)
		}
	}
	return path.String()
}

// newReverseProxy creates a reverse proxy with path rewriting and no Director to avoid deprecation.
func newReverseProxy(target *url.URL, rewritePath string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = rewritePath
			r.Out.RequestURI = ""
			r.Out.Host = target.Host
		},
	}
}

// newReverseProxyWithQuery creates a reverse proxy with path and query rewriting.
func newReverseProxyWithQuery(target *url.URL, rewritePath, query string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = rewritePath
			r.Out.URL.RawQuery = query
			r.Out.RequestURI = ""
			r.Out.Host = target.Host
		},
	}
}

// formatAsyncResponse re-encodes the async response after caching.
func formatAsyncResponse(result map[string]interface{}) string {
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) suspendHandler(w http.ResponseWriter, r *http.Request) {
	dest, ok := s.resolveInstance(w, r)
	if !ok {
		return
	}
	s.proxyInstanceOperation(w, r, dest)
}

func (s *Server) resumeHandler(w http.ResponseWriter, r *http.Request) {
	dest, ok := s.resolveInstance(w, r)
	if !ok {
		return
	}
	s.proxyInstanceOperation(w, r, dest)
}

func (s *Server) cancelHandler(w http.ResponseWriter, r *http.Request) {
	dest, ok := s.resolveInstance(w, r)
	if !ok {
		return
	}
	s.proxyInstanceOperation(w, r, dest)
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
