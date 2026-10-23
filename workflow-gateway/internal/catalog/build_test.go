package catalog

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateExternalURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid https URL",
			url:     "https://hello.example.com",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://hello.example.com",
			wantErr: false,
		},
		{
			name:    "valid https URL with port",
			url:     "https://hello.example.com:8080",
			wantErr: false,
		},
		{
			name:    "cluster-internal DNS .svc",
			url:     "https://hello.default.svc",
			wantErr: true,
			errMsg:  "cluster-internal",
		},
		{
			name:    "cluster-internal DNS .svc.cluster.local",
			url:     "https://hello.default.svc.cluster.local",
			wantErr: true,
			errMsg:  "cluster-internal",
		},
		{
			name:    "single-label hostname",
			url:     "https://localhost",
			wantErr: true,
			errMsg:  "non-external host",
		},
		{
			name:    "empty scheme",
			url:     "hello.example.com",
			wantErr: true,
			errMsg:  "no URL scheme",
		},
		{
			name:    "unsupported scheme",
			url:     "ftp://hello.example.com",
			wantErr: true,
			errMsg:  "unsupported scheme",
		},
		{
			name:    "malformed URL",
			url:     "ht!tp://hello.example.com",
			wantErr: true,
			errMsg:  "malformed URL",
		},
		{
			name:    "empty host",
			url:     "https://",
			wantErr: true,
			errMsg:  "empty host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExternalURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildExecutionEndpoints(t *testing.T) {
	t.Run("default definition happy path", func(t *testing.T) {
		def := newDefinition("default", "flow1", "myapp", "v1")
		svc := newService("default", "svc1", "flow1", "https://external.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.Empty(t, errs)
		expected := routing.WorkflowKey{Namespace: "myapp", Name: "flow", Version: "v1"}
		assert.Equal(t, routing.URL("https://external.example.com"), endpoints[expected])
		assert.Equal(t, 1, len(endpoints))
	})

	t.Run("service with empty URL is skipped", func(t *testing.T) {
		def := newDefinition("default", "flow1", "myapp", "v1")
		svc := newService("default", "svc1", "flow1", "")
		svc.Status.URL = "" // Ensure URL is empty

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.Empty(t, errs)
		assert.Empty(t, endpoints)
	})

	t.Run("invalid URL yields error entry", func(t *testing.T) {
		def := newDefinition("default", "flow1", "myapp", "v1")
		svc := newService("default", "svc1", "flow1", "https://invalid.default.svc.cluster.local")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.NotEmpty(t, errs)
		assert.Empty(t, endpoints)
		assert.Contains(t, errs[0].Error(), "has invalid URL")
	})

	t.Run("definition missing workflow labels yields error", func(t *testing.T) {
		defNoLabels := &logicv1.LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "flow1",
				Labels:    map[string]string{}, // Empty labels
			},
		}
		svc := newService("default", "svc1", "flow1", "https://external.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return defNoLabels, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.NotEmpty(t, errs)
		assert.Empty(t, endpoints)
		assert.Contains(t, errs[0].Error(), "missing workflow labels")
	})

	t.Run("missing referenced definition yields error", func(t *testing.T) {
		svc := newService("default", "svc1", "flow1", "https://external.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			return nil, fmt.Errorf("definition %s/%s not found", namespace, name)
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.NotEmpty(t, errs)
		assert.Empty(t, endpoints)
		assert.Contains(t, errs[0].Error(), "references definition")
	})

	t.Run("duplicate key with differing URLs yields error", func(t *testing.T) {
		def := newDefinition("default", "flow1", "myapp", "v1")
		svc1 := newService("default", "svc1", "flow1", "https://external1.example.com")
		svc2 := newService("default", "svc2", "flow1", "https://external2.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc1, svc2}, resolve)

		assert.NotEmpty(t, errs)
		assert.Equal(t, 1, len(endpoints)) // First one wins
		assert.Contains(t, errs[0].Error(), "duplicate workflow key")
	})

	t.Run("traffic definition references", func(t *testing.T) {
		defV1 := newDefinition("default", "flow-v1", "myapp", "v1.0")
		defV2 := newDefinition("default", "flow-v2", "myapp", "v2.0")

		svc := logicv1.LogicFlowService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "svc1",
			},
			Spec: logicv1.LogicFlowServiceSpec{
				Traffic: []logicv1.TrafficSpec{
					{
						DefinitionRef: corev1.LocalObjectReference{Name: "flow-v1"},
						Weight:        50,
					},
					{
						DefinitionRef: corev1.LocalObjectReference{Name: "flow-v2"},
						Weight:        50,
					},
				},
			},
			Status: logicv1.LogicFlowServiceStatus{
				URL: "https://external.example.com",
			},
		}

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow-v1" {
				return defV1, nil
			}
			if namespace == "default" && name == "flow-v2" {
				return defV2, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.Empty(t, errs)
		assert.Equal(t, 2, len(endpoints))

		keyV1 := routing.WorkflowKey{Namespace: "myapp", Name: "flow", Version: "v1.0"}
		keyV2 := routing.WorkflowKey{Namespace: "myapp", Name: "flow", Version: "v2.0"}
		assert.Equal(t, routing.URL("https://external.example.com"), endpoints[keyV1])
		assert.Equal(t, routing.URL("https://external.example.com"), endpoints[keyV2])
	})

	t.Run("same definition referenced by multiple services", func(t *testing.T) {
		def := newDefinition("default", "flow1", "myapp", "v1")
		svc1 := newService("default", "svc1", "flow1", "https://external1.example.com")

		// Create svc2 with same definition and URL - no conflict
		svc2 := newService("default", "svc2", "flow1", "https://external1.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc1, svc2}, resolve)

		// Same URL, no error
		assert.Empty(t, errs)
		expected := routing.WorkflowKey{Namespace: "myapp", Name: "flow", Version: "v1"}
		assert.Equal(t, routing.URL("https://external1.example.com"), endpoints[expected])
	})

	t.Run("resolver error is handled gracefully", func(t *testing.T) {
		svc := newService("default", "svc1", "flow1", "https://external.example.com")

		resolve := func(_, _ string) (*logicv1.LogicFlowDefinition, error) {
			return nil, fmt.Errorf("transient error")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc}, resolve)

		assert.NotEmpty(t, errs)
		assert.Empty(t, endpoints)
		assert.Contains(t, errs[0].Error(), "transient error")
	})

	t.Run("multiple services and definitions", func(t *testing.T) {
		def1 := newDefinition("default", "flow1", "app1", "v1")
		def2 := newDefinition("kube-sys", "flow2", "app2", "v1")
		svc1 := newService("default", "svc1", "flow1", "https://ep1.example.com")
		svc2 := newService("kube-sys", "svc2", "flow2", "https://ep2.example.com")

		resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
			if namespace == "default" && name == "flow1" {
				return def1, nil
			}
			if namespace == "kube-sys" && name == "flow2" {
				return def2, nil
			}
			return nil, fmt.Errorf("not found")
		}

		endpoints, errs := BuildExecutionEndpoints([]logicv1.LogicFlowService{svc1, svc2}, resolve)

		assert.Empty(t, errs)
		assert.Equal(t, 2, len(endpoints))
		key1 := routing.WorkflowKey{Namespace: "app1", Name: "flow", Version: "v1"}
		key2 := routing.WorkflowKey{Namespace: "app2", Name: "flow", Version: "v1"}
		assert.Equal(t, routing.URL("https://ep1.example.com"), endpoints[key1])
		assert.Equal(t, routing.URL("https://ep2.example.com"), endpoints[key2])
	})
}

// Helper functions

func newDefinition(namespace, name, wfNamespace, wfVersion string) *logicv1.LogicFlowDefinition {
	return &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				logicv1.LabelWorkflowNamespace: wfNamespace,
				logicv1.LabelWorkflowName:      "flow",
				logicv1.LabelWorkflowVersion:   wfVersion,
			},
		},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "rt1"},
		},
	}
}

func newService(namespace, name, defName, url string) logicv1.LogicFlowService {
	svc := logicv1.LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: logicv1.LogicFlowServiceSpec{
			DefaultDefinition: &corev1.LocalObjectReference{Name: defName},
			Ingress:           logicv1.IngressSpec{Host: "host.example.com"},
		},
		Status: logicv1.LogicFlowServiceStatus{
			URL: url,
		},
	}
	return svc
}
