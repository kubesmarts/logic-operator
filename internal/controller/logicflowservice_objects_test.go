package controller

import (
	"fmt"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testWfNamespace = "payments"
	testWfName      = "payment"
	testWfVersion   = "1.0.0"
)

func newTestService(opts ...func(*logicv1.LogicFlowService)) *logicv1.LogicFlowService {
	svc := &logicv1.LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-service",
			Namespace: testNamespace,
			UID:       "test-uid-svc",
		},
		Spec: logicv1.LogicFlowServiceSpec{
			DefaultDefinition: &corev1.LocalObjectReference{Name: "def-v1"},
			Ingress: logicv1.IngressSpec{
				Host: "workflows.example.com",
			},
		},
	}
	for _, o := range opts {
		o(svc)
	}
	return svc
}

func newTestRuntime() *logicv1.LogicFlowRuntime {
	return &logicv1.LogicFlowRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRuntimeName,
			Namespace: testNamespace,
		},
	}
}

// --- ingressForService tests ---

func TestIngressForService_Basic(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if ingress.Name == nil || *ingress.Name != "my-service" {
		t.Errorf("expected ingress name 'my-service', got %v", ingress.Name)
	}
	if ingress.Namespace == nil || *ingress.Namespace != testNamespace {
		t.Errorf("expected namespace %q, got %v", testNamespace, ingress.Namespace)
	}

	if len(ingress.Spec.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ingress.Spec.Rules))
	}
	rule := ingress.Spec.Rules[0]
	if rule.Host == nil || *rule.Host != "workflows.example.com" {
		t.Errorf("expected host 'workflows.example.com', got %v", rule.Host)
	}

	if len(rule.HTTP.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(rule.HTTP.Paths))
	}
	path := rule.HTTP.Paths[0]
	if path.Path == nil || *path.Path != "/" {
		t.Errorf("expected path '/', got %v", path.Path)
	}
	if path.Backend.Service.Name == nil || *path.Backend.Service.Name != testRuntimeName {
		t.Errorf("expected backend service %q, got %v", testRuntimeName, path.Backend.Service.Name)
	}
	if path.Backend.Service.Port.Number == nil || *path.Backend.Service.Port.Number != int32(defaultPort) {
		t.Errorf("expected backend port %d, got %v", defaultPort, path.Backend.Service.Port.Number)
	}
}

func TestIngressForService_RewriteTarget(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	expectedRewrite := fmt.Sprintf("/q/flow/exec/%s/%s/%s", testWfNamespace, testWfName, testWfVersion)
	if val, ok := ingress.Annotations[annotationNginxRewriteTarget]; !ok || val != expectedRewrite {
		t.Errorf("expected rewrite-target %q, got %q", expectedRewrite, val)
	}
}

func TestIngressForService_UserAnnotations(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.Annotations = map[string]string{
			"custom.io/foo": "bar",
		}
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if ingress.Annotations["custom.io/foo"] != "bar" {
		t.Error("expected user annotation to be preserved")
	}
	if _, ok := ingress.Annotations[annotationNginxRewriteTarget]; !ok {
		t.Error("expected rewrite-target annotation to be present alongside user annotations")
	}
}

func TestIngressForService_NoHost(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.Host = ""
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	rule := ingress.Spec.Rules[0]
	if rule.Host != nil {
		t.Errorf("expected no host on rule, got %v", *rule.Host)
	}
}

func TestIngressForService_IngressClassName(t *testing.T) {
	className := "nginx"
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.IngressClassName = &className
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if ingress.Spec.IngressClassName == nil || *ingress.Spec.IngressClassName != "nginx" {
		t.Errorf("expected ingressClassName 'nginx', got %v", ingress.Spec.IngressClassName)
	}
}

func TestIngressForService_TLSWithSecretRef(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.TLS = logicv1.TLSSpec{
			Enabled:   true,
			SecretRef: corev1.LocalObjectReference{Name: "my-tls-secret"},
		}
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if len(ingress.Spec.TLS) != 1 {
		t.Fatalf("expected 1 TLS entry, got %d", len(ingress.Spec.TLS))
	}
	tls := ingress.Spec.TLS[0]
	if tls.SecretName == nil || *tls.SecretName != "my-tls-secret" {
		t.Errorf("expected secretName 'my-tls-secret', got %v", tls.SecretName)
	}
	if len(tls.Hosts) != 1 || tls.Hosts[0] != "workflows.example.com" {
		t.Errorf("expected TLS host 'workflows.example.com', got %v", tls.Hosts)
	}
}

func TestIngressForService_TLSWithCertManagerClusterIssuer(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.TLS = logicv1.TLSSpec{
			Enabled: true,
			CertManager: &logicv1.CertManagerSpec{
				IssuerRef: logicv1.CertManagerIssuerRef{
					Name: "letsencrypt-prod",
					Kind: "ClusterIssuer",
				},
			},
		}
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if val, ok := ingress.Annotations[annotationCertManagerIssuer]; !ok || val != "letsencrypt-prod" {
		t.Errorf("expected cert-manager cluster-issuer annotation, got %q", val)
	}
	if _, ok := ingress.Annotations[annotationCertManagerNamespaceIssuer]; ok {
		t.Error("did not expect namespace issuer annotation for ClusterIssuer")
	}
	if len(ingress.Spec.TLS) != 1 {
		t.Fatalf("expected 1 TLS entry, got %d", len(ingress.Spec.TLS))
	}
	if tls := ingress.Spec.TLS[0]; tls.SecretName == nil || *tls.SecretName != "my-service-tls" {
		t.Errorf("expected auto-generated secretName 'my-service-tls', got %v", tls.SecretName)
	}
}

func TestIngressForService_TLSWithCertManagerNamespaceIssuer(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.TLS = logicv1.TLSSpec{
			Enabled: true,
			CertManager: &logicv1.CertManagerSpec{
				IssuerRef: logicv1.CertManagerIssuerRef{
					Name: "my-issuer",
					Kind: "Issuer",
				},
			},
		}
	})
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if val, ok := ingress.Annotations[annotationCertManagerNamespaceIssuer]; !ok || val != "my-issuer" {
		t.Errorf("expected cert-manager namespace issuer annotation, got %q", val)
	}
	if _, ok := ingress.Annotations[annotationCertManagerIssuer]; ok {
		t.Error("did not expect cluster-issuer annotation for namespace Issuer")
	}
}

func TestIngressForService_NoTLS(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if len(ingress.Spec.TLS) != 0 {
		t.Errorf("expected no TLS entries, got %d", len(ingress.Spec.TLS))
	}
}

func TestIngressForService_OwnerReference(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if len(ingress.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(ingress.OwnerReferences))
	}
	ref := ingress.OwnerReferences[0]
	if ref.Kind == nil || *ref.Kind != logicv1.LogicFlowServiceKind {
		t.Errorf("expected owner kind %q, got %v", logicv1.LogicFlowServiceKind, ref.Kind)
	}
	if ref.Name == nil || *ref.Name != "my-service" {
		t.Errorf("expected owner name 'my-service', got %v", ref.Name)
	}
}

func TestIngressForService_Labels(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := ingressForService(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if ingress.Labels[testLabelKeyName] != "my-service" {
		t.Errorf("expected label %s='my-service', got %q", testLabelKeyName, ingress.Labels[testLabelKeyName])
	}
	if ingress.Labels[testLabelKeyManagedBy] != LabelManagedBy {
		t.Errorf("expected label %s=%q, got %q", testLabelKeyManagedBy, LabelManagedBy, ingress.Labels[testLabelKeyManagedBy])
	}
}

// --- canaryIngressForService tests ---

func TestCanaryIngress_Annotations(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := canaryIngressForService(svc, rt, testWfNamespace, testWfName, "2.0.0", 10)

	if ingress.Name == nil || *ingress.Name != "my-service-canary" {
		t.Errorf("expected name 'my-service-canary', got %v", ingress.Name)
	}
	if val := ingress.Annotations[annotationNginxCanary]; val != "true" {
		t.Errorf("expected canary=true, got %q", val)
	}
	if val := ingress.Annotations[annotationNginxCanaryWeight]; val != "10" {
		t.Errorf("expected canary-weight=10, got %q", val)
	}
	expectedRewrite := "/q/flow/exec/payments/payment/2.0.0"
	if val := ingress.Annotations[annotationNginxRewriteTarget]; val != expectedRewrite {
		t.Errorf("expected rewrite-target %q, got %q", expectedRewrite, val)
	}
}

func TestCanaryIngress_SameBackend(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := canaryIngressForService(svc, rt, testWfNamespace, testWfName, "2.0.0", 10)

	path := ingress.Spec.Rules[0].HTTP.Paths[0]
	if path.Backend.Service.Name == nil || *path.Backend.Service.Name != testRuntimeName {
		t.Errorf("expected backend service %q, got %v", testRuntimeName, path.Backend.Service.Name)
	}
}

// --- directVersionIngress tests ---

func TestDirectVersionIngress_RegexPath(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	ingress := directVersionIngress(svc, rt, testWfNamespace, testWfName)

	if ingress.Name == nil || *ingress.Name != "my-service-direct" {
		t.Errorf("expected name 'my-service-direct', got %v", ingress.Name)
	}
	if val := ingress.Annotations[annotationNginxUseRegex]; val != "true" {
		t.Errorf("expected use-regex=true, got %q", val)
	}
	expectedRewrite := fmt.Sprintf("/q/flow/exec/%s/%s/$1", testWfNamespace, testWfName)
	if val := ingress.Annotations[annotationNginxRewriteTarget]; val != expectedRewrite {
		t.Errorf("expected rewrite-target %q, got %q", expectedRewrite, val)
	}

	path := ingress.Spec.Rules[0].HTTP.Paths[0]
	if path.Path == nil || *path.Path != "/v/(.+)" {
		t.Errorf("expected path '/v/(.+)', got %v", path.Path)
	}
}

// --- routeForDefault tests ---

func TestRouteForDefault_Basic(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Name != "my-service" {
		t.Errorf("expected route name 'my-service', got %q", route.Name)
	}
	if route.Namespace != testNamespace {
		t.Errorf("expected namespace %q, got %q", testNamespace, route.Namespace)
	}
	if route.Spec.Path != "/" {
		t.Errorf("expected path '/', got %q", route.Spec.Path)
	}
	expectedRewrite := fmt.Sprintf("/q/flow/exec/%s/%s/%s", testWfNamespace, testWfName, testWfVersion)
	if route.Annotations[annotationHAProxyRewriteTarget] != expectedRewrite {
		t.Errorf("expected haproxy rewrite %q, got %q", expectedRewrite, route.Annotations[annotationHAProxyRewriteTarget])
	}
	if route.Spec.To.Kind != "Service" {
		t.Errorf("expected target kind 'Service', got %q", route.Spec.To.Kind)
	}
	if route.Spec.To.Name != testRuntimeName {
		t.Errorf("expected target service %q, got %q", testRuntimeName, route.Spec.To.Name)
	}
	if route.Spec.To.Weight == nil || *route.Spec.To.Weight != 100 {
		t.Errorf("expected weight 100, got %v", route.Spec.To.Weight)
	}
	if route.Spec.Host != "workflows.example.com" {
		t.Errorf("expected host 'workflows.example.com', got %q", route.Spec.Host)
	}
}

func TestRouteForDefault_NoHost(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.Host = ""
	})
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Spec.Host != "" {
		t.Errorf("expected empty host, got %q", route.Spec.Host)
	}
}

func TestRouteForDefault_TLSEnabled(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.TLS = logicv1.TLSSpec{Enabled: true}
	})
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Spec.TLS == nil {
		t.Fatal("expected TLS config on route")
	}
	if route.Spec.TLS.Termination != "edge" {
		t.Errorf("expected edge termination, got %q", route.Spec.TLS.Termination)
	}
}

func TestRouteForDefault_NoTLS(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Spec.TLS != nil {
		t.Error("expected no TLS config on route when TLS disabled")
	}
}

func TestRouteForDefault_UserAnnotations(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.Annotations = map[string]string{
			"haproxy.router.openshift.io/timeout": "30s",
		}
	})
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Annotations["haproxy.router.openshift.io/timeout"] != "30s" {
		t.Error("expected user annotation on route")
	}
	if _, ok := route.Annotations[annotationHAProxyRewriteTarget]; !ok {
		t.Error("expected haproxy rewrite-target annotation")
	}
}

func TestRouteForDefault_OwnerReference(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if len(route.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(route.OwnerReferences))
	}
	ref := route.OwnerReferences[0]
	if ref.Kind != logicv1.LogicFlowServiceKind {
		t.Errorf("expected owner kind %q, got %q", logicv1.LogicFlowServiceKind, ref.Kind)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("expected controller=true on owner reference")
	}
}

func TestRouteForDefault_PortTargetsServicePort(t *testing.T) {
	svc := newTestService()
	rt := newTestRuntime()

	route := routeForDefault(svc, rt, testWfNamespace, testWfName, testWfVersion)

	if route.Spec.Port == nil {
		t.Fatal("expected port on route")
	}
	if route.Spec.Port.TargetPort.IntValue() != defaultPort {
		t.Errorf("expected target port %d, got %d", defaultPort, route.Spec.Port.TargetPort.IntValue())
	}
}

// --- httpRouteForService tests ---

func TestHTTPRoute_SingleTarget(t *testing.T) {
	gwName := "my-gateway"
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.GatewayRef = &logicv1.GatewayRef{Name: gwName}
	})
	rt := newTestRuntime()
	targets := []trafficTarget{{version: "1.0.0", weight: 100}}

	hr := httpRouteForService(svc, rt, testWfNamespace, testWfName, targets)

	if hr.Name == nil || *hr.Name != "my-service" {
		t.Errorf("expected name 'my-service', got %v", hr.Name)
	}
	if len(hr.Spec.ParentRefs) != 1 {
		t.Fatalf("expected 1 parent ref, got %d", len(hr.Spec.ParentRefs))
	}
	if hr.Spec.ParentRefs[0].Name == nil || string(*hr.Spec.ParentRefs[0].Name) != gwName {
		t.Errorf("expected parent ref name %q, got %v", gwName, hr.Spec.ParentRefs[0].Name)
	}
	if len(hr.Spec.Hostnames) != 1 || string(hr.Spec.Hostnames[0]) != "workflows.example.com" {
		t.Errorf("expected hostname 'workflows.example.com', got %v", hr.Spec.Hostnames)
	}
	if len(hr.Spec.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(hr.Spec.Rules))
	}
}

func TestHTTPRoute_WeightedTargets(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.GatewayRef = &logicv1.GatewayRef{Name: "my-gw"}
	})
	rt := newTestRuntime()
	targets := []trafficTarget{
		{version: "1.0.0", weight: 90},
		{version: "2.0.0", weight: 10},
	}

	hr := httpRouteForService(svc, rt, testWfNamespace, testWfName, targets)

	weightedRule := hr.Spec.Rules[1]
	if len(weightedRule.BackendRefs) != 2 {
		t.Fatalf("expected 2 backend refs in weighted rule, got %d", len(weightedRule.BackendRefs))
	}
	if *weightedRule.BackendRefs[0].Weight != 90 {
		t.Errorf("expected first backend weight 90, got %d", *weightedRule.BackendRefs[0].Weight)
	}
	if *weightedRule.BackendRefs[1].Weight != 10 {
		t.Errorf("expected second backend weight 10, got %d", *weightedRule.BackendRefs[1].Weight)
	}

	for i, ref := range weightedRule.BackendRefs {
		if len(ref.Filters) != 1 {
			t.Fatalf("expected 1 filter on backend ref %d, got %d", i, len(ref.Filters))
		}
		if ref.Filters[0].URLRewrite == nil || ref.Filters[0].URLRewrite.Path == nil {
			t.Fatalf("expected URL rewrite filter on backend ref %d", i)
		}
	}

	rewrite0 := *weightedRule.BackendRefs[0].Filters[0].URLRewrite.Path.ReplacePrefixMatch
	expected0 := fmt.Sprintf("/q/flow/exec/%s/%s/1.0.0", testWfNamespace, testWfName)
	if rewrite0 != expected0 {
		t.Errorf("expected rewrite %q, got %q", expected0, rewrite0)
	}

	rewrite1 := *weightedRule.BackendRefs[1].Filters[0].URLRewrite.Path.ReplacePrefixMatch
	expected1 := fmt.Sprintf("/q/flow/exec/%s/%s/2.0.0", testWfNamespace, testWfName)
	if rewrite1 != expected1 {
		t.Errorf("expected rewrite %q, got %q", expected1, rewrite1)
	}
}

func TestHTTPRoute_DirectVersionRule(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.GatewayRef = &logicv1.GatewayRef{Name: "my-gw"}
	})
	rt := newTestRuntime()
	targets := []trafficTarget{{version: "1.0.0", weight: 100}}

	hr := httpRouteForService(svc, rt, testWfNamespace, testWfName, targets)

	directRule := hr.Spec.Rules[0]
	if len(directRule.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(directRule.Matches))
	}
	if *directRule.Matches[0].Path.Value != "/v/" {
		t.Errorf("expected path '/v/', got %q", *directRule.Matches[0].Path.Value)
	}
	expectedRewrite := fmt.Sprintf("/q/flow/exec/%s/%s", testWfNamespace, testWfName)
	rewrite := *directRule.BackendRefs[0].Filters[0].URLRewrite.Path.ReplacePrefixMatch
	if rewrite != expectedRewrite {
		t.Errorf("expected direct rewrite %q, got %q", expectedRewrite, rewrite)
	}
}

func TestHTTPRoute_GatewayRefWithNamespace(t *testing.T) {
	ns := "infra"
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.GatewayRef = &logicv1.GatewayRef{Name: "shared-gw", Namespace: &ns}
	})
	rt := newTestRuntime()
	targets := []trafficTarget{{version: "1.0.0", weight: 100}}

	hr := httpRouteForService(svc, rt, testWfNamespace, testWfName, targets)

	if hr.Spec.ParentRefs[0].Namespace == nil || string(*hr.Spec.ParentRefs[0].Namespace) != "infra" {
		t.Errorf("expected parent ref namespace 'infra', got %v", hr.Spec.ParentRefs[0].Namespace)
	}
}

func TestHTTPRoute_NoHost(t *testing.T) {
	svc := newTestService(func(s *logicv1.LogicFlowService) {
		s.Spec.Ingress.Host = ""
		s.Spec.Ingress.GatewayRef = &logicv1.GatewayRef{Name: "my-gw"}
	})
	rt := newTestRuntime()
	targets := []trafficTarget{{version: "1.0.0", weight: 100}}

	hr := httpRouteForService(svc, rt, testWfNamespace, testWfName, targets)

	if len(hr.Spec.Hostnames) != 0 {
		t.Errorf("expected no hostnames, got %v", hr.Spec.Hostnames)
	}
}

// --- quarkusFlowPath tests ---

func TestQuarkusFlowPath_WithVersion(t *testing.T) {
	path := quarkusFlowPath("payments", "pay", "1.0.0")
	expected := "/q/flow/exec/payments/pay/1.0.0"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestQuarkusFlowPath_WithoutVersion(t *testing.T) {
	path := quarkusFlowPath("payments", "pay", "")
	expected := "/q/flow/exec/payments/pay"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}
