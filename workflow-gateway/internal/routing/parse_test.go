package routing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

// chiCtx builds an *http.Request with chi URL params injected.
func chiCtx(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestResolve_WithVersion(t *testing.T) {
	store := discovery.NewRouteStore()
	store.Replace(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	r := chiCtx(
		httptest.NewRequest(http.MethodPost, "/", nil),
		map[string]string{"namespace": "demo", "name": "hello", "version": "1.0.0"},
	)
	url, err := routing.Resolve(r, store)
	assert.NoError(t, err)
	assert.Equal(t, "https://hello.example.com", url)
}

func TestResolve_WithoutVersion_LatestUsed(t *testing.T) {
	store := discovery.NewRouteStore()
	store.Replace(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
		{Namespace: "demo", Name: "hello", Version: "2.0.0"}: "https://hello.example.com",
	})
	// No "version" param — simulates /v1/runtime/{ns}/{name} route.
	// All versions of the workflow share the same upstream URL.
	r := chiCtx(
		httptest.NewRequest(http.MethodPost, "/", nil),
		map[string]string{"namespace": "demo", "name": "hello"},
	)
	url, err := routing.Resolve(r, store)
	assert.NoError(t, err)
	assert.Equal(t, "https://hello.example.com", url)
}

func TestResolve_NotFound_WithVersion(t *testing.T) {
	store := discovery.NewRouteStore()
	r := chiCtx(
		httptest.NewRequest(http.MethodPost, "/", nil),
		map[string]string{"namespace": "demo", "name": "missing", "version": "1.0.0"},
	)
	_, err := routing.Resolve(r, store)
	assert.True(t, errors.Is(err, routing.ErrWorkflowNotFound))
}

func TestResolve_NotFound_WithoutVersion(t *testing.T) {
	store := discovery.NewRouteStore()
	r := chiCtx(
		httptest.NewRequest(http.MethodPost, "/", nil),
		map[string]string{"namespace": "demo", "name": "missing"},
	)
	_, err := routing.Resolve(r, store)
	assert.True(t, errors.Is(err, routing.ErrWorkflowNotFound))
}

func TestResolve_LiteralInstanceNotTreatedAsVersion(t *testing.T) {
	// When chi routes /v1/runtime/{ns}/{name}/instance/{id}/suspend,
	// the "version" URL param is absent (empty string). Resolve must
	// call LookupLatest, not Lookup with version="instance".
	store := discovery.NewRouteStore()
	store.Replace(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	// Simulate what chi populates for the version-absent suspend route.
	r := chiCtx(
		httptest.NewRequest(http.MethodPost, "/", nil),
		map[string]string{"namespace": "demo", "name": "hello"}, // no "version"
	)
	url, err := routing.Resolve(r, store)
	assert.NoError(t, err)
	assert.Equal(t, "https://hello.example.com", url)
}
