package controller

import (
	_ "embed"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
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

// ToDaemonSetSpec creates a DaemonSet spec for Vector.
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

// VectorProbes returns container options for Vector readiness and liveness probes.
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

// WithVectorVolumeMounts returns container options for Vector volume mounts.
func WithVectorVolumeMounts() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorConfigVolumeName).WithMountPath(VectorConfigMountPath).WithReadOnly(true))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorDataVolumeName).WithMountPath(VectorDataMountPath))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorVarLogVolumeName).WithMountPath(VectorVarLogPath).WithReadOnly(true))
		c.WithVolumeMounts(corev1ac.VolumeMount().WithName(VectorDockerPathVolumeName).WithMountPath(VectorDockerPath).WithReadOnly(true))
	}
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

// WithGraphQLVars returns container options for GraphQL configuration.
func WithGraphQLVars() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv([]*corev1ac.EnvVarApplyConfiguration{
			envLiteral("QUARKUS_SMALLRYE_GRAPHQL_UI_ENABLE", "true"),
		}...)
	}
}
