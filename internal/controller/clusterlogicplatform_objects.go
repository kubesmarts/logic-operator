package controller

import (
	_ "embed"
	"maps"
	"strconv"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Helper functions for ClusterLogicPlatform resources

func dataIndexNameCLP(plat *logicv1.ClusterLogicPlatform) string {
	return plat.Name + "-data-index"
}

func vectorNameCLP(plat *logicv1.ClusterLogicPlatform) string {
	return plat.Name + "-vector"
}

func objectKeyDataIndexCLP(plat *logicv1.ClusterLogicPlatform) client.ObjectKey {
	return client.ObjectKey{Namespace: plat.Spec.TargetNamespace, Name: dataIndexNameCLP(plat)}
}

func VectorVolumesCLP(plat *logicv1.ClusterLogicPlatform) []*corev1ac.VolumeApplyConfiguration {
	vols := []*corev1ac.VolumeApplyConfiguration{
		corev1ac.Volume().
			WithName(VectorDataVolumeName).
			WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
		corev1ac.Volume().
			WithName(VectorVarLogVolumeName).
			WithHostPath(corev1ac.HostPathVolumeSource().WithPath(VectorVarLogPath)),
		corev1ac.Volume().
			WithName(VectorDockerPathVolumeName).
			WithHostPath(corev1ac.HostPathVolumeSource().WithPath(VectorDockerPath)),
	}

	// Vector writes events to the Data Index database.
	if dataIndexPersistence := platformDataIndexPersistence(plat); dataIndexPersistence != nil {
		vols = append(vols, corev1ac.Volume().WithName(VectorConfigVolumeName).WithConfigMap(corev1ac.ConfigMapVolumeSource().WithName(vectorNameCLP(plat))))
	}

	return vols
}

func WithVectorEnvVarsCLP(plat *logicv1.ClusterLogicPlatform) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		envs := []*corev1ac.EnvVarApplyConfiguration{
			envFieldRef("VECTOR_SELF_NODE_NAME", "spec.nodeName"),
			envFieldRef("VECTOR_SELF_POD_NAME", "metadata.name"),
			envFieldRef("VECTOR_SELF_POD_NAMESPACE", "metadata.namespace"),
			envFieldRef("NODE_NAME", "spec.nodeName"),
		}
		// TODO: change the config to fetch for multiple namespaces
		workflowNamespace := plat.Spec.TargetNamespace
		if plat.Spec.Vector != nil && len(plat.Spec.Vector.WatchNamespaces) > 0 {
			workflowNamespace = plat.Spec.Vector.WatchNamespaces[0]
		}
		envs = append(envs, envLiteral("WORKFLOW_NAMESPACE", workflowNamespace))
		if plat.Spec.Vector != nil && plat.Spec.Vector.DebugEvents {
			envs = append(envs, envLiteral("DEBUG_EVENTS", "true"))
		}
		// Vector writes workflow events to the Data Index datastore.
		if dataIndexPersistence := platformDataIndexPersistence(plat); dataIndexPersistence != nil && dataIndexPersistence.PostgreSQL != nil {
			pg := dataIndexPersistence.PostgreSQL
			port := defaultPostgresPort
			if pg.ServiceRef.Port != nil {
				port = *pg.ServiceRef.Port
			}
			operationalSecret := logicv1.OperationalSecret(*dataIndexPersistence)
			userKey := operationalSecret.UserKey
			if userKey == "" {
				userKey = logicv1.DefaultPgsqlSecretUserKey
			}
			passwordKey := operationalSecret.PasswordKey
			if passwordKey == "" {
				passwordKey = logicv1.DefaultPgsqlSecretPasswordKey
			}
			ns := pg.ServiceRef.Namespace
			if ns == "" {
				ns = plat.Spec.TargetNamespace
			}
			envs = append(envs,
				envFromSecret("POSTGRES_USER", operationalSecret.Name, userKey),
				envFromSecret("POSTGRES_PASSWORD", operationalSecret.Name, passwordKey),
				envLiteral("POSTGRES_PORT", strconv.Itoa(port)),
				envLiteral("POSTGRES_DB", pg.ServiceRef.DatabaseName),
				envLiteral("POSTGRES_SCHEMA", pg.ServiceRef.DatabaseSchema),
				envLiteral("POSTGRES_HOST", BuildPostgresAddress(pg.ServiceRef.Name, ns, 0)),
			)
		}
		c.WithEnv(envs...)
	}
}

// ingressForDataIndexCLP creates a standard Ingress for Data Index GraphQL API.
func ingressForDataIndexCLP(plat *logicv1.ClusterLogicPlatform) *networkingv1ac.IngressApplyConfiguration {
	pathType := networkingv1.PathTypePrefix
	ingress := plat.Spec.DataIndex.Ingress

	// Base annotations
	annotations := make(map[string]string)
	maps.Copy(annotations, ingress.Annotations)

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
						WithName(dataIndexNameCLP(plat)).
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
			tlsConfig = tlsConfig.WithSecretName(dataIndexNameCLP(plat) + "-tls")
		}

		spec = spec.WithTLS(tlsConfig)
	}

	return networkingv1ac.Ingress(dataIndexNameCLP(plat), plat.Spec.TargetNamespace).
		WithLabels(ChildLabelsInstance(plat, dataIndexNameCLP(plat))).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind)).
		WithSpec(spec)
}

// routeForDataIndexCLP creates an OpenShift Route for Data Index GraphQL API.
func routeForDataIndexCLP(plat *logicv1.ClusterLogicPlatform) *routev1.Route {
	weight := int32(100)
	ingress := plat.Spec.DataIndex.Ingress

	// Base annotations
	annotations := make(map[string]string)
	for k, v := range ingress.Annotations {
		annotations[k] = v
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dataIndexNameCLP(plat),
			Namespace:   plat.Spec.TargetNamespace,
			Labels:      ChildLabelsInstance(plat, dataIndexNameCLP(plat)),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				OwnerRefStandard(plat, logicv1.ClusterLogicPlatformKind),
			},
		},
		Spec: routev1.RouteSpec{
			Path: "/",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   dataIndexNameCLP(plat),
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
		route.Spec.TLS = &routev1.TLSConfig{
			Termination:                   termination,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}
	}

	return route
}

// workflowGatewayNameCLP returns the deterministic name for gateway namespaced resources.
func workflowGatewayNameCLP(plat *logicv1.ClusterLogicPlatform) string {
	base := plat.Name + "-" + GatewayComponentName
	if len(base) > 63 {
		base = base[:63]
	}
	return base
}

// workflowGatewayClusterRoleBindingNameCLP returns the deterministic name for the ClusterRoleBinding.
func workflowGatewayClusterRoleBindingNameCLP(plat *logicv1.ClusterLogicPlatform) string {
	base := plat.Spec.TargetNamespace + "-" + plat.Name + "-" + GatewayComponentName
	if len(base) > 63 {
		base = base[:63]
	}
	return base
}

// serviceForGatewayCLP creates a Service for the workflow gateway.
func serviceForGatewayCLP(plat *logicv1.ClusterLogicPlatform) *corev1ac.ServiceApplyConfiguration {
	gwName := workflowGatewayNameCLP(plat)
	return corev1ac.Service(gwName, plat.Spec.TargetNamespace).
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

// serviceAccountForGatewayCLP creates a ServiceAccount for the workflow gateway.
func serviceAccountForGatewayCLP(plat *logicv1.ClusterLogicPlatform) *corev1ac.ServiceAccountApplyConfiguration {
	gwName := workflowGatewayNameCLP(plat)
	childLabels := ChildLabelsInstance(plat, gwName)
	return corev1ac.ServiceAccount(gwName, plat.Spec.TargetNamespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind))
}

// ingressForGatewayCLP creates a Kubernetes Ingress for the workflow gateway.
func ingressForGatewayCLP(plat *logicv1.ClusterLogicPlatform) *networkingv1ac.IngressApplyConfiguration {
	gwName := workflowGatewayNameCLP(plat)
	childLabels := ChildLabelsInstance(plat, gwName)
	ingress := plat.Spec.Gateway.Ingress

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
			annotations["cert-manager.io/issuer"] = ingress.TLS.CertManager.IssuerRef.Name
			if ingress.TLS.CertManager.IssuerRef.Kind != "" {
				annotations["cert-manager.io/issuer-kind"] = ingress.TLS.CertManager.IssuerRef.Kind
			}
			generatedSecretName := gwName + "-tls-cert"
			tlsCfg.SecretName = &generatedSecretName
		}
	}

	ingressObj := networkingv1ac.Ingress(gwName, plat.Spec.TargetNamespace).
		WithLabels(childLabels).
		WithAnnotations(annotations).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind)).
		WithSpec(spec)

	return ingressObj
}

// routeForGatewayCLP creates an OpenShift Route for the workflow gateway.
func routeForGatewayCLP(plat *logicv1.ClusterLogicPlatform) *routev1.Route {
	gwName := workflowGatewayNameCLP(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: plat.Spec.TargetNamespace,
			Labels:    childLabels,
			OwnerReferences: []metav1.OwnerReference{
				OwnerRefStandard(plat, logicv1.ClusterLogicPlatformKind),
			},
		},
		Spec: routev1.RouteSpec{
			Host: plat.Spec.Gateway.Ingress.Host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: gwName,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(80),
			},
		},
	}

	// Add TLS configuration if enabled
	ingress := plat.Spec.Gateway.Ingress
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

// hasFinalizerWorkflowGatewayCLP checks if the gateway finalizer is present.
func hasFinalizerWorkflowGatewayCLP(plat *logicv1.ClusterLogicPlatform) bool {
	for _, f := range plat.Finalizers {
		if f == FinalizerWorkflowGateway {
			return true
		}
	}
	return false
}

// removeFinalizerWorkflowGatewayCLP removes the gateway finalizer.
func removeFinalizerWorkflowGatewayCLP(plat *logicv1.ClusterLogicPlatform) {
	var newFinalizers []string
	for _, f := range plat.Finalizers {
		if f != FinalizerWorkflowGateway {
			newFinalizers = append(newFinalizers, f)
		}
	}
	plat.Finalizers = newFinalizers
}
