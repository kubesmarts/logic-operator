package discovery

import (
	"fmt"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// resolverFrom builds a definitionResolver backed by an in-memory map keyed by "namespace/name".
func resolverFrom(defs map[string]*logicv1.LogicFlowDefinition) definitionResolver {
	return func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
		key := fmt.Sprintf("%s/%s", namespace, name)
		def, ok := defs[key]
		if !ok {
			return nil, fmt.Errorf("definition %s not found", key)
		}
		return def, nil
	}
}

func definitionWithLabels(namespace, name, wfNamespace, wfName, wfVersion string) *logicv1.LogicFlowDefinition {
	return &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				logicv1.LabelWorkflowName:      wfName,
				logicv1.LabelWorkflowNamespace: wfNamespace,
				logicv1.LabelWorkflowVersion:   wfVersion,
			},
		},
	}
}

func TestBuildRoutesDefaultDefinition(t *testing.T) {
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "my-def"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://example.com/acme"},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"ns1/my-def": definitionWithLabels("ns1", "my-def", "acme", "workflow1", "v1"),
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	assert.Empty(t, errs)
	assert.Len(t, routes, 1)

	key := WorkflowKey{Namespace: "acme", Name: "workflow1", Version: "v1"}
	url, ok := routes[key]
	assert.True(t, ok)
	assert.Equal(t, "https://example.com/acme", url)
}

func TestBuildRoutesTrafficSplit(t *testing.T) {
	// Two versions of the same workflow served by one Service via traffic split.
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "hello"},
			Spec: logicv1.LogicFlowServiceSpec{
				Traffic: []logicv1.TrafficSpec{
					{DefinitionRef: corev1.LocalObjectReference{Name: "workflow1-v1.0.0"}, Weight: 80},
					{DefinitionRef: corev1.LocalObjectReference{Name: "workflow1-v2.0.0"}, Weight: 20},
				},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://api.example.com"},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"default/workflow1-v1.0.0": definitionWithLabels("default", "workflow1-v1.0.0", "acme", "workflow1", "1.0"),
		"default/workflow1-v2.0.0": definitionWithLabels("default", "workflow1-v2.0.0", "acme", "workflow1", "2.0"),
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	assert.Empty(t, errs)
	assert.Len(t, routes, 2)

	url1, ok1 := routes[WorkflowKey{Namespace: "acme", Name: "workflow1", Version: "1.0"}]
	assert.True(t, ok1)
	assert.Equal(t, "https://api.example.com", url1)

	url2, ok2 := routes[WorkflowKey{Namespace: "acme", Name: "workflow1", Version: "2.0"}]
	assert.True(t, ok2)
	assert.Equal(t, "https://api.example.com", url2)
}

func TestBuildRoutesArbitraryDefinitionNames(t *testing.T) {
	// Definition names are arbitrary; the routing key comes only from labels.
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "totally-random-name-xyz"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://example.com"},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"ns1/totally-random-name-xyz": definitionWithLabels("ns1", "totally-random-name-xyz", "beta", "orders", "v3"),
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	assert.Empty(t, errs)
	url, ok := routes[WorkflowKey{Namespace: "beta", Name: "orders", Version: "v3"}]
	assert.True(t, ok)
	assert.Equal(t, "https://example.com", url)
}

func TestBuildRoutesInvalidURL(t *testing.T) {
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "my-def"},
			},
			Status: logicv1.LogicFlowServiceStatus{
				URL: "http://service.default.svc.cluster.local", // cluster-internal, invalid
			},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"ns1/my-def": definitionWithLabels("ns1", "my-def", "acme", "workflow1", "v1"),
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	assert.Len(t, errs, 1)
	assert.Empty(t, routes)
}

func TestBuildRoutesMissingLabels(t *testing.T) {
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "my-def"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://example.com"},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"ns1/my-def": {
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns1",
				Name:      "my-def",
				Labels:    map[string]string{}, // missing workflow labels
			},
		},
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	assert.Len(t, errs, 1)
	assert.Empty(t, routes)
}

func TestBuildRoutesMissingDefinition(t *testing.T) {
	// Service references a definition that doesn't exist yet.
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "not-there"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://example.com"},
		},
	}

	routes, errs := buildRoutes(services, resolverFrom(map[string]*logicv1.LogicFlowDefinition{}))

	assert.Len(t, errs, 1)
	assert.Empty(t, routes)
}

func TestBuildRoutesEmptyInput(t *testing.T) {
	routes, errs := buildRoutes([]logicv1.LogicFlowService{}, resolverFrom(map[string]*logicv1.LogicFlowDefinition{}))
	assert.Empty(t, errs)
	assert.Empty(t, routes)
}

func TestBuildRoutesDuplicateKeyRejectedWhenURLsDiffer(t *testing.T) {
	// Two Services that both reference the same Definition with different URLs.
	// This is ambiguous because cache list ordering is not stable across recomputes.
	services := []logicv1.LogicFlowService{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "svc1"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "shared-def"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://url1.example.com"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "svc2"},
			Spec: logicv1.LogicFlowServiceSpec{
				DefaultDefinition: &corev1.LocalObjectReference{Name: "shared-def"},
			},
			Status: logicv1.LogicFlowServiceStatus{URL: "https://url2.example.com"},
		},
	}

	defs := map[string]*logicv1.LogicFlowDefinition{
		"default/shared-def": definitionWithLabels("default", "shared-def", "acme", "workflow1", "v1"),
	}

	routes, errs := buildRoutes(services, resolverFrom(defs))

	// The second service's conflicting URL should be rejected with an error.
	assert.Len(t, errs, 1, "should have one error for the conflicting route")
	// The first service's route should be accepted; the second rejected.
	assert.Len(t, routes, 1)
	assert.Equal(t, "https://url1.example.com", routes[WorkflowKey{Namespace: "acme", Name: "workflow1", Version: "v1"}])
}
