package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
)

const (
	GatewayComponentName     = "workflow-gateway"
	GatewayContainerName     = "workflow-gateway"
	FinalizerWorkflowGateway = "logicplatform.logic.kubesmarts.org/workflow-gateway-cleanup"
)

// workflowGatewayName returns the deterministic name for gateway namespaced resources.
func workflowGatewayName(plat *logicv1.LogicPlatform) string {
	base := plat.Name + "-" + GatewayComponentName
	if len(base) > 63 {
		base = base[:63]
	}
	return base
}

// workflowGatewayClusterRoleBindingName returns the deterministic name for the ClusterRoleBinding.
func workflowGatewayClusterRoleBindingName(plat *logicv1.LogicPlatform) string {
	base := plat.Namespace + "-" + plat.Name + "-" + GatewayComponentName
	if len(base) > 63 {
		base = base[:63]
	}
	return base
}

// serviceForGateway creates a Service for the workflow gateway.
func serviceForGateway(plat *logicv1.LogicPlatform) *corev1ac.ServiceApplyConfiguration {
	gwName := workflowGatewayName(plat)
	return corev1ac.Service(gwName, plat.Namespace).
		WithSpec(corev1ac.ServiceSpec().
			WithType(corev1.ServiceTypeClusterIP).
			WithSelector(SelectorLabels(gwName)).
			WithPorts(
				corev1ac.ServicePort().
					WithName("http").
					WithPort(logicv1.WorkflowGatewayServicePort).
					WithTargetPort(intstr.FromInt(int(logicv1.WorkflowGatewayHTTPPort))).
					WithProtocol(corev1.ProtocolTCP),
			))
}

// serviceAccountForGateway creates a ServiceAccount for the workflow gateway.
func serviceAccountForGateway(plat *logicv1.LogicPlatform) *corev1ac.ServiceAccountApplyConfiguration {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)
	return corev1ac.ServiceAccount(gwName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind))
}

// ingressForGateway creates a Kubernetes Ingress for the workflow gateway.
func ingressForGateway(plat *logicv1.LogicPlatform) *networkingv1ac.IngressApplyConfiguration {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)
	ingress := plat.Spec.WorkflowGateway.Ingress

	// Build annotations
	annotations := make(map[string]string)
	if ingress.Annotations != nil {
		for k, v := range ingress.Annotations {
			annotations[k] = v
		}
	}

	backendPort := networkingv1ac.ServiceBackendPort().WithNumber(logicv1.WorkflowGatewayServicePort)
	backend := networkingv1ac.IngressBackend().WithService(
		networkingv1ac.IngressServiceBackend().WithName(gwName).WithPort(backendPort),
	)
	path := networkingv1ac.HTTPIngressPath().
		WithPath("/").
		WithPathType(networkingv1.PathTypePrefix).
		WithBackend(backend)

	httpRule := networkingv1ac.HTTPIngressRuleValue().WithPaths(path)
	rule := networkingv1ac.IngressRule().
		WithHost(ingress.Host).
		WithHTTP(httpRule)

	spec := networkingv1ac.IngressSpec().WithRules(rule)

	// Add IngressClassName if specified
	if ingress.IngressClassName != nil {
		spec.IngressClassName = ingress.IngressClassName
	}

	// Add TLS configuration if enabled
	if ingress.TLS.Enabled {
		tlsCfg := networkingv1ac.IngressTLS().WithHosts(ingress.Host)
		if ingress.TLS.SecretRef.Name != "" {
			secretName := ingress.TLS.SecretRef.Name
			tlsCfg.SecretName = &secretName
		}
		spec.WithTLS(tlsCfg)

		// Add cert-manager annotations and set generated secret name if issuer configured
		if ingress.TLS.CertManager != nil && ingress.TLS.CertManager.IssuerRef.Name != "" {
			// Standard cert-manager annotations for automatic certificate provisioning
			annotations["cert-manager.io/issuer"] = ingress.TLS.CertManager.IssuerRef.Name
			if ingress.TLS.CertManager.IssuerRef.Kind != "" {
				annotations["cert-manager.io/issuer-kind"] = ingress.TLS.CertManager.IssuerRef.Kind
			}
			// Use deterministic secret name for generated cert
			generatedSecretName := gwName + "-tls-cert"
			tlsCfg.SecretName = &generatedSecretName
		}
	}

	ingressObj := networkingv1ac.Ingress(gwName, plat.Namespace).
		WithLabels(childLabels).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return ingressObj
}

// routeForGateway creates an OpenShift Route for the workflow gateway.
func routeForGateway(plat *logicv1.LogicPlatform) *routev1.Route {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: plat.Namespace,
			Labels:    childLabels,
			OwnerReferences: []metav1.OwnerReference{
				OwnerRefStandard(plat, logicv1.LogicPlatformKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: plat.Spec.WorkflowGateway.Ingress.Host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: gwName,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(80), // Gateway service port (HTTP)
			},
		},
	}

	// Add TLS configuration if enabled
	ingress := plat.Spec.WorkflowGateway.Ingress
	if ingress.TLS.Enabled {
		tlsTermination := routev1.TLSTerminationEdge
		route.Spec.TLS = &routev1.TLSConfig{
			Termination: tlsTermination,
		}
		if ingress.TLS.SecretRef.Name != "" {
			route.Spec.TLS.Certificate = ingress.TLS.SecretRef.Name
		}
	}

	return route
}

// hasFinalizerWorkflowGateway checks if the gateway finalizer is present.
func hasFinalizerWorkflowGateway(plat *logicv1.LogicPlatform) bool {
	for _, f := range plat.Finalizers {
		if f == FinalizerWorkflowGateway {
			return true
		}
	}
	return false
}

// removeFinalizerWorkflowGateway removes the gateway finalizer.
func removeFinalizerWorkflowGateway(plat *logicv1.LogicPlatform) {
	var newFinalizers []string
	for _, f := range plat.Finalizers {
		if f != FinalizerWorkflowGateway {
			newFinalizers = append(newFinalizers, f)
		}
	}
	plat.Finalizers = newFinalizers
}

// WorkflowGatewayEnvVars returns a ContainerOption that sets POD_NAMESPACE.
func WorkflowGatewayEnvVars() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv(
			corev1ac.EnvVar().
				WithName("POD_NAMESPACE").
				WithValueFrom(corev1ac.EnvVarSource().
					WithFieldRef(corev1ac.ObjectFieldSelector().
						WithFieldPath("metadata.namespace"))),
		)
	}
}

// WorkflowGatewayPorts returns a ContainerOption that sets container ports.
func WorkflowGatewayPorts() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithPorts(
			corev1ac.ContainerPort().
				WithName("http").
				WithContainerPort(logicv1.WorkflowGatewayHTTPPort).
				WithProtocol(corev1.ProtocolTCP),
			corev1ac.ContainerPort().
				WithName("management").
				WithContainerPort(logicv1.WorkflowGatewayManagementPort).
				WithProtocol(corev1.ProtocolTCP),
		)
	}
}

// WorkflowGatewayProbes returns a ContainerOption that sets liveness and readiness probes.
func WorkflowGatewayProbes() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/ready").
				WithPort(intstr.FromInt32(logicv1.WorkflowGatewayManagementPort))).
			WithInitialDelaySeconds(10).
			WithPeriodSeconds(10).
			WithTimeoutSeconds(3).
			WithFailureThreshold(3))

		c.WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromInt32(logicv1.WorkflowGatewayManagementPort))).
			WithInitialDelaySeconds(10).
			WithPeriodSeconds(30).
			WithTimeoutSeconds(3).
			WithFailureThreshold(3))
	}
}
