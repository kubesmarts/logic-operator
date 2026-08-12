package v1

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
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

func TestWebhookIntegration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add logic scheme: %v", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: getFirstFoundEnvTestBinaryDir(),
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() { _ = testEnv.Stop() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	if err := builder.WebhookManagedBy(mgr, &LogicFlowDefinition{}).
		WithValidator(&LogicFlowDefinitionValidator{Reader: mgr.GetClient()}).
		Complete(); err != nil {
		t.Fatalf("register LFD webhook: %v", err)
	}
	if err := builder.WebhookManagedBy(mgr, &LogicFlowRuntime{}).
		WithValidator(&LogicFlowRuntimeValidator{}).
		Complete(); err != nil {
		t.Fatalf("register LFR webhook: %v", err)
	}
	if err := builder.WebhookManagedBy(mgr, &LogicFlowService{}).
		WithValidator(&LogicFlowServiceValidator{Reader: mgr.GetClient()}).
		Complete(); err != nil {
		t.Fatalf("register LFS webhook: %v", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("manager exited: %v", err)
		}
	}()

	// Wait for webhook server TLS to be ready
	dialer := &net.Dialer{Timeout: time.Second}
	addr := fmt.Sprintf("%s:%d",
		testEnv.WebhookInstallOptions.LocalServingHost,
		testEnv.WebhookInstallOptions.LocalServingPort)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	testLFDWebhooks(t, ctx, k8sClient)
	testLFRWebhooks(t, ctx, k8sClient)
}

func testLFDWebhooks(t *testing.T, ctx context.Context, k8sClient client.Client) {
	t.Run("LFD/valid create succeeds", func(t *testing.T) {
		def := &LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "valid-def", Namespace: testNamespace},
			Spec: LogicFlowDefinitionSpec{
				RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
				Flow:       validFlowRaw(),
			},
		}
		if err := k8sClient.Create(ctx, def); err != nil {
			t.Fatalf("expected valid create to succeed: %v", err)
		}
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, def) })
	})

	t.Run("LFD/invalid flow rejected", func(t *testing.T) {
		def := &LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-flow-def", Namespace: testNamespace},
			Spec: LogicFlowDefinitionSpec{
				RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
				Flow:       invalidFlowRaw(),
			},
		}
		err := k8sClient.Create(ctx, def)
		if err == nil {
			t.Cleanup(func() { _ = k8sClient.Delete(ctx, def) })
			t.Fatal("expected invalid flow to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFD/empty runtimeRef rejected", func(t *testing.T) {
		def := &LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "no-ref-def", Namespace: testNamespace},
			Spec: LogicFlowDefinitionSpec{
				RuntimeRef: corev1.LocalObjectReference{Name: ""},
				Flow:       validFlowRaw(),
			},
		}
		err := k8sClient.Create(ctx, def)
		if err == nil {
			t.Cleanup(func() { _ = k8sClient.Delete(ctx, def) })
			t.Fatal("expected empty runtimeRef to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFD/runtimeRef change rejected (immutable)", func(t *testing.T) {
		def := &LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "immut-def", Namespace: testNamespace},
			Spec: LogicFlowDefinitionSpec{
				RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
				Flow:       validFlowRaw(),
			},
		}
		if err := k8sClient.Create(ctx, def); err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, def) })

		// Re-fetch to get resourceVersion
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(def), def); err != nil {
			t.Fatalf("get: %v", err)
		}
		def.Spec.RuntimeRef.Name = "other-runtime"
		err := k8sClient.Update(ctx, def)
		if err == nil {
			t.Fatal("expected runtimeRef change to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFD/metadata update allowed", func(t *testing.T) {
		def := &LogicFlowDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "meta-def", Namespace: testNamespace},
			Spec: LogicFlowDefinitionSpec{
				RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
				Flow:       validFlowRaw(),
			},
		}
		if err := k8sClient.Create(ctx, def); err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, def) })

		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(def), def); err != nil {
			t.Fatalf("get: %v", err)
		}
		def.Labels = map[string]string{"team": "platform"}
		if err := k8sClient.Update(ctx, def); err != nil {
			t.Errorf("expected metadata update to succeed: %v", err)
		}
	})
}

func testLFRWebhooks(t *testing.T, ctx context.Context, k8sClient client.Client) {
	t.Run("LFR/valid empty spec succeeds", func(t *testing.T) {
		rt := &LogicFlowRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "valid-rt", Namespace: testNamespace},
			Spec:       LogicFlowRuntimeSpec{},
		}
		if err := k8sClient.Create(ctx, rt); err != nil {
			t.Fatalf("expected valid create to succeed: %v", err)
		}
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, rt) })
	})

	t.Run("LFR/API_KEY without keys rejected", func(t *testing.T) {
		rt := &LogicFlowRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-apikey-rt", Namespace: testNamespace},
			Spec: LogicFlowRuntimeSpec{
				Security: RuntimeSecuritySpec{Type: RuntimeSecurityAPIKey},
			},
		}
		err := k8sClient.Create(ctx, rt)
		if err == nil {
			t.Cleanup(func() { _ = k8sClient.Delete(ctx, rt) })
			t.Fatal("expected API_KEY without keys to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFR/OIDC without config rejected", func(t *testing.T) {
		rt := &LogicFlowRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-oidc-rt", Namespace: testNamespace},
			Spec: LogicFlowRuntimeSpec{
				Security: RuntimeSecuritySpec{Type: RuntimeSecurityOIDC},
			},
		}
		err := k8sClient.Create(ctx, rt)
		if err == nil {
			t.Cleanup(func() { _ = k8sClient.Delete(ctx, rt) })
			t.Fatal("expected OIDC without config to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFR/minimal image with persistence rejected", func(t *testing.T) {
		rt := &LogicFlowRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-image-rt", Namespace: testNamespace},
			Spec: LogicFlowRuntimeSpec{
				RuntimeSpec: RuntimeSpec{
					ApplicationSpec: ApplicationSpec{
						Image: fmt.Sprintf("%s/%s:0.15.1-%s", FlowRunnerRegistry, FlowRunnerImage, ImageVariantMinimal),
					},
					Persistence: &PersistenceOptionsSpec{
						PostgreSQL: &PersistencePostgreSQL{
							SecretRef: PostgreSQLSecretOptions{Name: testPGSecret},
							JdbcUrl:   "jdbc:postgresql://localhost:5432/test",
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, rt)
		if err == nil {
			t.Cleanup(func() { _ = k8sClient.Delete(ctx, rt) })
			t.Fatal("expected minimal image with persistence to be rejected")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected Forbidden, got: %v", err)
		}
	})

	t.Run("LFR/API_KEY with valid keys succeeds", func(t *testing.T) {
		rt := &LogicFlowRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "good-apikey-rt", Namespace: testNamespace},
			Spec: LogicFlowRuntimeSpec{
				Security: RuntimeSecuritySpec{
					Type: RuntimeSecurityAPIKey,
					APIKey: &APIKeyAuthSpec{
						Keys: []APIKeySpec{{
							Name:      "test-key",
							SecretRef: SecretKeySelector{Name: "my-secret"},
							Roles:     []RuntimeSecurityRole{RuntimeSecurityRoleInvoker},
						}},
					},
				},
			},
		}
		if err := k8sClient.Create(ctx, rt); err != nil {
			t.Fatalf("expected valid API_KEY to succeed: %v", err)
		}
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, rt) })
	})
}
