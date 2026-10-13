package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// StartEnvtest starts an envtest control plane with CRDs loaded from crdPath.
// Returns the rest config, a scheme with logicv1 registered, and a stop
// function. When envtest binaries are unavailable, cfg and scheme are nil and
// stop is a harmless no-op so callers can still run unit tests.
func StartEnvtest(crdPath string) (cfg *rest.Config, scheme *runtime.Scheme, stop func()) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
	}
	if dir := BinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest unavailable, running unit tests only: %v\n", err)
		return nil, nil, func() {}
	}

	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		panic(err)
	}
	if err := logicv1.AddToScheme(s); err != nil {
		panic(err)
	}

	return cfg, s, func() { _ = testEnv.Stop() }
}

// BinaryDir locates the envtest binary directory under <repo-root>/bin/k8s.
// Returns "" when KUBEBUILDER_ASSETS is already set (envtest reads it directly).
func BinaryDir() string {
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		return ""
	}
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

// NewTestClient returns a typed Kubernetes client for the envtest cluster.
func NewTestClient(t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) client.Client {
	t.Helper()
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	return c
}

// NewDefinition builds a LogicFlowDefinition fixture with workflow-identity
// labels and a minimal flow document.
func NewDefinition(namespace, name, wfNamespace, wfName, wfVersion string) *logicv1.LogicFlowDefinition {
	return &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				logicv1.LabelWorkflowNamespace: wfNamespace,
				logicv1.LabelWorkflowName:      wfName,
				logicv1.LabelWorkflowVersion:   wfVersion,
			},
		},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "rt1"},
			Flow: runtime.RawExtension{
				Raw: []byte(`{"document":{"dsl":"1.0.0"},"do":[]}`),
			},
		},
	}
}

// CreateServiceWithURL creates a LogicFlowService and sets its status URL.
func CreateServiceWithURL(ctx context.Context, t *testing.T, c client.Client, namespace, name, defName, url string) {
	t.Helper()
	svc := &logicv1.LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: logicv1.LogicFlowServiceSpec{
			DefaultDefinition: &corev1.LocalObjectReference{Name: defName},
			Ingress:           logicv1.IngressSpec{Host: "host.example.com"},
		},
	}
	require.NoError(t, c.Create(ctx, svc))
	svc.Status.URL = url
	require.NoError(t, c.Status().Update(ctx, svc))
}
