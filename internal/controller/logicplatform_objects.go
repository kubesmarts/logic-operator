package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
)

const (
	ContainerNameDataIndex = "logic-data-index"
)

// ingressForDataIndex creates a standard Ingress for Data Index GraphQL API.
func ingressForDataIndex(plat *logicv1.LogicPlatform) *networkingv1ac.IngressApplyConfiguration {
	pathType := networkingv1.PathTypePrefix
	ingress := plat.Spec.DataIndex.Ingress

	// Base annotations
	annotations := make(map[string]string)
	for k, v := range ingress.Annotations {
		annotations[k] = v
	}

	// Apply cert-manager annotations if using certManager
	if ingress.TLS.Enabled && ingress.TLS.CertManager != nil {
		kind := ingress.TLS.CertManager.IssuerRef.Kind
		if kind == "" || kind == "ClusterIssuer" {
			annotations[annotationCertManagerIssuer] = ingress.TLS.CertManager.IssuerRef.Name
		} else {
			annotations[annotationCertManagerNamespaceIssuer] = ingress.TLS.CertManager.IssuerRef.Name
		}
	}

	// Create ingress rule
	rule := networkingv1ac.IngressRule().
		WithHost(ingress.Host).
		WithHTTP(networkingv1ac.HTTPIngressRuleValue().
			WithPaths(networkingv1ac.HTTPIngressPath().
				WithPath("/").
				WithPathType(pathType).
				WithBackend(networkingv1ac.IngressBackend().
					WithService(networkingv1ac.IngressServiceBackend().
						WithName(plat.Name).
						WithPort(networkingv1ac.ServiceBackendPort().
							WithNumber(defaultPort))))))

	spec := networkingv1ac.IngressSpec().WithRules(rule)

	// Set ingress class
	if ingress.IngressClassName != nil {
		spec = spec.WithIngressClassName(*ingress.IngressClassName)
	}

	// Apply TLS configuration
	if ingress.TLS.Enabled {
		tlsConfig := networkingv1ac.IngressTLS().
			WithHosts(ingress.Host)

		// Use existing secret or generate via cert-manager
		if ingress.TLS.SecretRef.Name != "" {
			tlsConfig = tlsConfig.WithSecretName(ingress.TLS.SecretRef.Name)
		} else if ingress.TLS.CertManager != nil {
			// cert-manager will create a secret with the same name as the ingress
			tlsConfig = tlsConfig.WithSecretName(plat.Name + "-tls")
		}

		spec = spec.WithTLS(tlsConfig)
	}

	return networkingv1ac.Ingress(plat.Name, plat.Namespace).
		WithLabels(ChildLabels(plat)).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)
}

// routeForDataIndex creates an OpenShift Route for Data Index GraphQL API.
func routeForDataIndex(plat *logicv1.LogicPlatform) *routev1.Route {
	weight := int32(100)
	ingress := plat.Spec.DataIndex.Ingress

	// Base annotations
	annotations := make(map[string]string)
	for k, v := range ingress.Annotations {
		annotations[k] = v
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        plat.Name,
			Namespace:   plat.Namespace,
			Labels:      ChildLabels(plat),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				OwnerRefStandard(plat, logicv1.LogicPlatformKind),
			},
		},
		Spec: routev1.RouteSpec{
			Path: "/",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   plat.Name,
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt32(defaultPort),
			},
		},
	}

	// Set host if provided
	if ingress.Host != "" {
		route.Spec.Host = ingress.Host
	}

	// Apply TLS configuration
	if ingress.TLS.Enabled {
		termination := routev1.TLSTerminationEdge
		// TODO: If ingress.TLS.SecretRef.Name is set, read secret and populate
		// Certificate/Key/CACertificate. For now, rely on OpenShift's default certificate.
		route.Spec.TLS = &routev1.TLSConfig{
			Termination:                   termination,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}
	}

	return route
}
