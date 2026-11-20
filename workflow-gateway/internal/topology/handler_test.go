package topology_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
	"github.com/stretchr/testify/require"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func reader(t *testing.T, objects ...client.Object) client.Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}
func request(t *testing.T, r client.Reader, path string, code int) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	server.New(server.DefaultConfig(), nil, nil, nil, r).Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, code, w.Code, w.Body.String())
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	return w.Body.Bytes()
}
func TestTopology(t *testing.T) {
	m := meta.ObjectMeta{Name: "checkout", Namespace: "sales", Generation: 2}
	pg := &v1.DatastoreSpec{PostgreSQL: &v1.PersistencePostgreSQL{
		ServiceRef: v1.PostgreSQLServiceOptions{Name: "private-db-host", Namespace: "db", DatabaseSchema: "workflows"},
		SecretRef:  v1.PostgreSQLSecretOptions{Name: "private-credentials"},
	}}
	p := &v1.ClusterLogicPlatform{ObjectMeta: meta.ObjectMeta{Name: "shared", Generation: 2},
		Spec:   v1.ClusterLogicPlatformSpec{TargetNamespace: "infra", Gateway: &v1.WorkflowGatewaySpec{}, Vector: &v1.VectorSpec{}, DataIndex: &v1.DataIndexSpec{Persistence: pg}, Runtime: &v1.RuntimePlatformSpec{Persistence: &v1.RuntimePersistenceSpec{Runtime: *pg}}},
		Status: v1.ClusterLogicPlatformStatus{ObservedGeneration: 2, Vector: &v1.VectorStatus{Ready: true, DaemonSetRef: core.LocalObjectReference{Name: "shared-vector"}}, DataIndex: &v1.DataIndexStatus{Ready: false}}}
	rt := &v1.LogicFlowRuntime{ObjectMeta: m, Status: v1.LogicFlowRuntimeStatus{ObservedGeneration: 1, Phase: v1.ApplicationPhaseReady}}
	d := &v1.LogicFlowDefinition{ObjectMeta: m, Spec: v1.LogicFlowDefinitionSpec{RuntimeRef: core.LocalObjectReference{Name: m.Name}}}
	svc := &v1.LogicFlowService{ObjectMeta: m, Spec: v1.LogicFlowServiceSpec{DefaultDefinition: &core.LocalObjectReference{Name: m.Name}}}
	other := d.DeepCopy()
	other.Namespace = "other"
	r := reader(t, p, rt, d, svc, other)
	payload := request(t, r, "/v1/topology?namespace=sales", 200)
	require.NotContains(t, string(payload), "private-")
	require.NotContains(t, string(payload), "other")
	var graph struct {
		Nodes []struct {
			ID, Kind string
			Status   struct{ State string }
		}
		Edges []struct {
			Type, Source, Target string
			Weight               *int
		}
		Discovery struct{ Partial bool }
	}
	require.NoError(t, json.Unmarshal(payload, &graph))
	require.False(t, graph.Discovery.Partial)
	require.Len(t, graph.Nodes, 8)
	ids := map[string]bool{}
	states := map[string]string{}
	for _, n := range graph.Nodes {
		ids[n.ID] = true
		states[n.Kind] = n.Status.State
	}
	require.Equal(t, "Ready", states["Vector"])
	require.Equal(t, "NotReady", states["DataIndex"])
	require.Equal(t, "Unknown", states["WorkflowGateway"])
	require.Equal(t, "Unknown", states["LogicFlowRuntime"])
	require.Equal(t, "Unknown", states["PostgreSQLSchema"])
	counts := map[string]int{}
	for _, e := range graph.Edges {
		require.True(t, ids[e.Source] && ids[e.Target])
		counts[e.Type]++
		if e.Type == "routesTo" {
			require.NotNil(t, e.Weight)
			require.Equal(t, 100, *e.Weight)
		}
	}
	require.Equal(t, map[string]int{"routesTo": 1, "runsOn": 1, "usesPlatform": 1, "manages": 3, "usesSchema": 2}, counts)
	require.Equal(t, payload, request(t, r, "/v1/topology?namespace=sales", 200))
	require.JSONEq(t, `{"nodes":[],"edges":[],"discovery":{"partial":false,"warnings":[]}}`, string(request(t, r, "/v1/topology?namespace=empty", 200)))
	require.Contains(t, string(request(t, r, "/v1/topology", 200)), "UnresolvedReference")
	p.Spec.Vector = nil
	payload = request(t, reader(t, p), "/v1/topology?namespace=infra", 200)
	require.Contains(t, string(payload), `"Vector":"NotConfigured"`)
	require.NotContains(t, string(payload), `"kind":"Vector"`)
}

type failingRuntimeReader struct{ client.Reader }

func (r failingRuntimeReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*v1.LogicFlowRuntimeList); ok {
		return errors.New("unavailable")
	}
	return r.Reader.List(ctx, list, opts...)
}
func TestDiscoveryFailures(t *testing.T) {
	r := failingRuntimeReader{reader(t)}
	require.JSONEq(t, `{"nodes":[],"edges":[],"discovery":{"partial":true,"warnings":[{"code":"SourceUnavailable","kind":"LogicFlowRuntime"}]}}`, string(request(t, r, "/v1/topology", 200)))
	request(t, nil, "/v1/topology", 503)
	for _, q := range []string{"namespace=", "namespace=a&namespace=b", "namespace=Bad", "namespace=%ff"} {
		request(t, nil, "/v1/topology?"+q, 400)
	}
}
