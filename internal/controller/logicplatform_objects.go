package controller

import (
	_ "embed"
	"maps"
	"strconv"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Embed Vector configuration from synced configs
//
//go:embed configs/vector/mode1-postgresql/vector.yaml
var vectorPostgreSQLConfig string

const (
	ContainerNameDataIndex = "logic-data-index"
	ContainerNameVector    = "vector"

	// Vector DaemonSet
	VectorPort    = int32(9598) // Prometheus metrics
	VectorAPIPort = int32(8686) // Health/readiness API

	// Vector configuration
	VectorPgSQLConfigMapName = "logic-vector-pgsql-configmap"

	VectorConfigVolumeName     = "config"
	VectorDataVolumeName       = "data"
	VectorVarLogVolumeName     = "var-log"
	VectorDockerPathVolumeName = "var-lib-docker-containers"

	VectorConfigMountPath = "/etc/vector"
	VectorDataMountPath   = "/tmp/vector"
	VectorVarLogPath      = "/var/log"
	VectorDockerPath      = "/var/lib/docker/containers"

	ClusterRoleVector = "logic-operator-vector"
)

func VectorPgSQLConfigMapAC(ns string) *corev1ac.ConfigMapApplyConfiguration {
	return corev1ac.ConfigMap(VectorPgSQLConfigMapName, ns).WithData(map[string]string{"vector.yaml": vectorPostgreSQLConfig})
}

func VectorVolumes(plat *logicv1.LogicPlatform) []*corev1ac.VolumeApplyConfiguration {
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

	if plat.Spec.DataIndex.Persistence != nil && plat.Spec.DataIndex.Persistence.PostgreSQL != nil {
		vols = append(vols, corev1ac.Volume().WithName(VectorConfigVolumeName).WithConfigMap(corev1ac.ConfigMapVolumeSource().WithName(vectorName(plat))))
	}

	return vols
}

func WithVectorVolumeMounts() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorConfigVolumeName).WithMountPath(VectorConfigMountPath).WithReadOnly(true))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorDataVolumeName).WithMountPath(VectorDataMountPath))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorVarLogVolumeName).WithMountPath(VectorVarLogPath).WithReadOnly(true))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorDockerPathVolumeName).WithMountPath(VectorDockerPath).WithReadOnly(true))
	}
}

func VectorProbes() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		// Readiness probe checks Vector API health endpoint
		c.WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromInt32(VectorAPIPort))).
			WithInitialDelaySeconds(5).
			WithPeriodSeconds(10).
			WithTimeoutSeconds(3).
			WithFailureThreshold(3))

		// Liveness probe checks Vector API health endpoint
		c.WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromInt32(VectorAPIPort))).
			WithInitialDelaySeconds(30).
			WithPeriodSeconds(30).
			WithTimeoutSeconds(3).
			WithFailureThreshold(3))
	}
}

func WithVectorEnvVars(p *logicv1.PersistenceOptionsSpec) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		envs := []*corev1ac.EnvVarApplyConfiguration{
			envFieldRef("VECTOR_SELF_NODE_NAME", "spec.nodeName"),
			envFieldRef("VECTOR_SELF_POD_NAME", "metadata.name"),
			envFieldRef("VECTOR_SELF_POD_NAMESPACE", "metadata.namespace"),
			envFieldRef("NODE_NAME", "spec.nodeName"),
		}
		if p != nil && p.PostgreSQL != nil {
			port := defaultPostgresPort
			if p.PostgreSQL.ServiceRef.Port != nil {
				port = *p.PostgreSQL.ServiceRef.Port
			}
			userKey := p.PostgreSQL.SecretRef.UserKey
			if userKey == "" {
				userKey = logicv1.DefaultPgsqlSecretUserKey
			}
			passwordKey := p.PostgreSQL.SecretRef.PasswordKey
			if passwordKey == "" {
				passwordKey = logicv1.DefaultPgsqlSecretPasswordKey
			}
			envs = append(envs,
				envFromSecret("POSTGRES_USER", p.PostgreSQL.SecretRef.Name, userKey),
				envFromSecret("POSTGRES_PASSWORD", p.PostgreSQL.SecretRef.Name, passwordKey),
				envLiteral("POSTGRES_PORT", strconv.Itoa(port)),
				envLiteral("POSTGRES_DB", p.PostgreSQL.ServiceRef.DatabaseSchema),
				envLiteral("POSTGRES_HOST", DefaultSvcAddress(p.PostgreSQL.ServiceRef.Name, p.PostgreSQL.ServiceRef.Namespace, 0)),
			)
		}
		c.WithEnv(envs...)
	}
}

func ToDaemonSetSpec(containerName string,
	app *logicv1.ApplicationSpec,
	podLabels map[string]string,
	selLabels map[string]string,
	containerOpts []ContainerOption,
	podOpts ...PodSpecOption) *appsv1ac.DaemonSetSpecApplyConfiguration {
	podTemplate := toPodTemplateSpecACWithPodOpts(containerName, app, podLabels, containerOpts, podOpts...)
	spec := appsv1ac.DaemonSetSpec().
		WithSelector(metav1ac.LabelSelector().WithMatchLabels(selLabels)).
		WithTemplate(podTemplate)

	return spec
}

// WithFlywayPersistenceVars adds Flyway migration env props to Data Index containers for PgSQL persistence.
// TODO: move this to DbMigration once we have this component set.
func WithFlywayPersistenceVars() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv([]*corev1ac.EnvVarApplyConfiguration{
			envLiteral("QUARKUS_FLYWAY_MIGRATE_AT_START", "true"),
			envLiteral("QUARKUS_FLYWAY_BASELINE_ON_MIGRATE", "true"),
			envLiteral("QUARKUS_FLYWAY_BASELINE_VERSION", "0"),
		}...)
	}
}

func WithGraphQLVars() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv([]*corev1ac.EnvVarApplyConfiguration{
			envLiteral("QUARKUS_SMALLRYE_GRAPHQL_UI_ENABLE", "true"),
		}...)
	}
}

// ingressForDataIndex creates a standard Ingress for Data Index GraphQL API.
func ingressForDataIndex(plat *logicv1.LogicPlatform) *networkingv1ac.IngressApplyConfiguration {
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

	return networkingv1ac.Ingress(dataIndexName(plat), plat.Namespace).
		WithLabels(ChildLabelsInstance(plat, dataIndexName(plat))).
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
			Name:        dataIndexName(plat),
			Namespace:   plat.Namespace,
			Labels:      ChildLabelsInstance(plat, dataIndexName(plat)),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				OwnerRefStandard(plat, logicv1.LogicPlatformKind),
			},
		},
		Spec: routev1.RouteSpec{
			Path: "/",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   dataIndexName(plat),
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

func dataIndexName(plat *logicv1.LogicPlatform) string {
	return plat.Name + "-data-index"
}

func vectorName(plat *logicv1.LogicPlatform) string {
	return plat.Name + "-vector"
}

func objectKeyDataIndex(plat *logicv1.LogicPlatform) client.ObjectKey {
	return client.ObjectKey{Namespace: plat.Namespace, Name: dataIndexName(plat)}
}
