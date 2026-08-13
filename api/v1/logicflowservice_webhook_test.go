package v1

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testServiceValidator(objs ...runtime.Object) *LogicFlowServiceValidator {
	scheme := runtime.NewScheme()
	_ = AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objs) > 0 {
		builder = builder.WithRuntimeObjects(objs...)
	}
	return &LogicFlowServiceValidator{Reader: builder.Build()}
}

func testDefinition(name, runtimeRef, workflowName string) *LogicFlowDefinition {
	return &LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				LabelWorkflowName: workflowName,
			},
		},
		Spec: LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: runtimeRef},
			Flow:       validFlowRaw(),
		},
	}
}

func testService(opts ...func(*LogicFlowService)) *LogicFlowService {
	svc := &LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: testNamespace},
		Spec: LogicFlowServiceSpec{
			Ingress: IngressSpec{
				Host: "test.example.com",
			},
		},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func withDefaultDefinition(name string) func(*LogicFlowService) {
	return func(svc *LogicFlowService) {
		svc.Spec.DefaultDefinition = &corev1.LocalObjectReference{Name: name}
	}
}

func withTraffic(entries ...TrafficSpec) func(*LogicFlowService) {
	return func(svc *LogicFlowService) {
		svc.Spec.Traffic = entries
	}
}

func withHost(host string) func(*LogicFlowService) {
	return func(svc *LogicFlowService) {
		svc.Spec.Ingress.Host = host
	}
}

func withGatewayRef(name string) func(*LogicFlowService) {
	return func(svc *LogicFlowService) {
		svc.Spec.Ingress.GatewayRef = &GatewayRef{Name: name}
	}
}

func withTLS(tls TLSSpec) func(*LogicFlowService) {
	return func(svc *LogicFlowService) {
		svc.Spec.Ingress.TLS = tls
	}
}

func traffic(defName string, weight int32) TrafficSpec {
	return TrafficSpec{
		DefinitionRef: corev1.LocalObjectReference{Name: defName},
		Weight:        weight,
	}
}

func TestLogicFlowServiceValidator_ValidateCreate(t *testing.T) {
	def1 := testDefinition("def-v1", testRuntimeName, "payment")
	def2 := testDefinition("def-v2", testRuntimeName, "payment")
	defOtherRT := testDefinition("def-other-rt", "other-runtime", "payment")
	defOtherWF := testDefinition("def-other-wf", testRuntimeName, "order")

	tests := []struct {
		name    string
		objs    []runtime.Object
		svc     *LogicFlowService
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid defaultDefinition",
			objs: []runtime.Object{def1},
			svc:  testService(withDefaultDefinition("def-v1")),
		},
		{
			name: "valid traffic single entry",
			objs: []runtime.Object{def1},
			svc:  testService(withTraffic(traffic("def-v1", 100))),
		},
		{
			name: "valid traffic split",
			objs: []runtime.Object{def1, def2},
			svc:  testService(withTraffic(traffic("def-v1", 80), traffic("def-v2", 20))),
		},
		{
			name:    "neither traffic nor defaultDefinition",
			svc:     testService(),
			wantErr: true,
			errMsg:  "one of spec.traffic or spec.defaultDefinition is required",
		},
		{
			name:    "both traffic and defaultDefinition",
			objs:    []runtime.Object{def1},
			svc:     testService(withDefaultDefinition("def-v1"), withTraffic(traffic("def-v1", 100))),
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name:    "traffic weights not 100",
			objs:    []runtime.Object{def1, def2},
			svc:     testService(withTraffic(traffic("def-v1", 80), traffic("def-v2", 10))),
			wantErr: true,
			errMsg:  "must sum to 100, got 90",
		},
		{
			name:    "missing host in nginx mode",
			objs:    []runtime.Object{def1},
			svc:     testService(withDefaultDefinition("def-v1"), withHost("")),
			wantErr: true,
			errMsg:  "spec.ingress.host is required for nginx Ingress mode",
		},
		{
			name: "no host required when gatewayRef is set",
			objs: []runtime.Object{def1},
			svc:  testService(withDefaultDefinition("def-v1"), withHost(""), withGatewayRef("my-gateway")),
		},
		{
			name: "ingressClassName nginx is valid",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), func(s *LogicFlowService) {
				className := "nginx"
				s.Spec.Ingress.IngressClassName = &className
			}),
		},
		{
			name: "ingressClassName non-nginx rejected",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), func(s *LogicFlowService) {
				className := "traefik"
				s.Spec.Ingress.IngressClassName = &className
			}),
			wantErr: true,
			errMsg:  "only supports \"nginx\"",
		},
		{
			name:    "definition not found",
			svc:     testService(withDefaultDefinition("nonexistent")),
			wantErr: true,
			errMsg:  "not found",
		},
		{
			name:    "definitions target different runtimes",
			objs:    []runtime.Object{def1, defOtherRT},
			svc:     testService(withTraffic(traffic("def-v1", 50), traffic("def-other-rt", 50))),
			wantErr: true,
			errMsg:  "all definitions must target the same runtime",
		},
		{
			name:    "definitions have different workflow names",
			objs:    []runtime.Object{def1, defOtherWF},
			svc:     testService(withTraffic(traffic("def-v1", 50), traffic("def-other-wf", 50))),
			wantErr: true,
			errMsg:  "all definitions must be versions of the same workflow",
		},
		{
			name: "TLS with secretRef valid",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), withTLS(TLSSpec{
				Enabled:   true,
				SecretRef: corev1.LocalObjectReference{Name: "my-tls-secret"},
			})),
		},
		{
			name: "TLS with certManager valid",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), withTLS(TLSSpec{
				Enabled: true,
				CertManager: &CertManagerSpec{
					IssuerRef: CertManagerIssuerRef{Name: "letsencrypt"},
				},
			})),
		},
		{
			name: "TLS with both secretRef and certManager",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), withTLS(TLSSpec{
				Enabled:   true,
				SecretRef: corev1.LocalObjectReference{Name: "my-secret"},
				CertManager: &CertManagerSpec{
					IssuerRef: CertManagerIssuerRef{Name: "letsencrypt"},
				},
			})),
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "TLS certManager missing issuer name",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), withTLS(TLSSpec{
				Enabled:     true,
				CertManager: &CertManagerSpec{},
			})),
			wantErr: true,
			errMsg:  "issuerRef.name is required",
		},
		{
			name: "TLS disabled skips validation",
			objs: []runtime.Object{def1},
			svc: testService(withDefaultDefinition("def-v1"), withTLS(TLSSpec{
				Enabled:   false,
				SecretRef: corev1.LocalObjectReference{Name: "my-secret"},
				CertManager: &CertManagerSpec{
					IssuerRef: CertManagerIssuerRef{Name: "letsencrypt"},
				},
			})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := testServiceValidator(tt.objs...)
			_, err := v.ValidateCreate(context.Background(), tt.svc)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if got := err.Error(); !contains(got, tt.errMsg) {
					t.Errorf("ValidateCreate() error = %q, want substring %q", got, tt.errMsg)
				}
			}
		})
	}
}

func TestLogicFlowServiceValidator_ValidateUpdate(t *testing.T) {
	def1 := testDefinition("def-v1", testRuntimeName, "payment")

	base := testService(withDefaultDefinition("def-v1"))

	tests := []struct {
		name    string
		oldObj  *LogicFlowService
		newObj  *LogicFlowService
		wantErr bool
		errMsg  string
	}{
		{
			name:   "no-op update allowed",
			oldObj: base,
			newObj: base.DeepCopy(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := testServiceValidator(def1)
			_, err := v.ValidateUpdate(context.Background(), tt.oldObj, tt.newObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if got := err.Error(); !contains(got, tt.errMsg) {
					t.Errorf("ValidateUpdate() error = %q, want substring %q", got, tt.errMsg)
				}
			}
		})
	}
}

func TestLogicFlowServiceValidator_ValidateDelete(t *testing.T) {
	v := testServiceValidator()
	_, err := v.ValidateDelete(context.Background(), &LogicFlowService{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
