package controller

import (
	"fmt"
	"strings"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	defaultPostgresPort      = 5432
	defaultDatabaseName      = "logicflow"
	defaultSecretUserKey     = "POSTGRESQL_USER"
	defaultSecretPasswordKey = "POSTGRESQL_PASSWORD"
	defaultPort              = 80
)

func runnerImage(variant string) string {
	return fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, variant)
}

func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, QuarkusFlowRegistry+"/"+QuarkusFlowRunner+":")
}

func hasPersistence(p *logicv1.PersistenceOptionsSpec) bool {
	return p != nil && p.PostgreSQL != nil
}

func QuarkusService(owner metav1.Object, ownerKind string) *corev1ac.ServiceApplyConfiguration {
	svc := corev1ac.Service(owner.GetName(), owner.GetNamespace()).
		WithOwnerReferences(OwnerRef(owner, ownerKind)).
		WithLabels(ChildLabels(owner)).
		WithSpec(
			corev1ac.ServiceSpec().
				WithSelector(SelectorLabels(owner.GetName())).
				WithPorts(
					corev1ac.ServicePort().
						WithName("http").
						WithProtocol(corev1.ProtocolTCP).
						WithPort(defaultPort).
						WithTargetPort(intstr.FromInt32(QuarkusPort)),
				),
		)
	return svc
}

// DefaultRunnerImage returns a ContainerOption that sets the runner image when none was
// specified by the user. Auto-selects minimal (no persistence) or standard (with persistence).
func DefaultRunnerImage(persistence *logicv1.PersistenceOptionsSpec) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		if c.Image != nil && *c.Image != "" {
			return
		}
		if hasPersistence(persistence) {
			c.WithImage(runnerImage(ImageVariantStandard))
		} else {
			c.WithImage(runnerImage(ImageVariantMinimal))
		}
	}
}

// ValidateRunnerImage delegates to the api/v1 validation for backward compatibility.
func ValidateRunnerImage(image string, persistence *logicv1.PersistenceOptionsSpec) error {
	return logicv1.ValidateRunnerImage(image, persistence)
}

// DefaultProbes returns a ContainerOption that sets liveness and readiness probes
// using the Quarkus SmallRye Health endpoints. User-specified probes are preserved.
func DefaultProbes() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		if c.LivenessProbe == nil {
			c.WithLivenessProbe(corev1ac.Probe().
				WithHTTPGet(corev1ac.HTTPGetAction().
					WithPath("/q/health/live").
					WithPort(intstr.FromInt32(QuarkusPort))).
				WithInitialDelaySeconds(30).
				WithPeriodSeconds(10).
				WithTimeoutSeconds(3).
				WithFailureThreshold(3))
		}
		if c.ReadinessProbe == nil {
			c.WithReadinessProbe(corev1ac.Probe().
				WithHTTPGet(corev1ac.HTTPGetAction().
					WithPath("/q/health/ready").
					WithPort(intstr.FromInt32(QuarkusPort))).
				WithInitialDelaySeconds(10).
				WithPeriodSeconds(5).
				WithTimeoutSeconds(3).
				WithFailureThreshold(3))
		}
	}
}

// WithPersistenceEnvVars returns a ContainerOption that appends Quarkus datasource environment variables.
// The namespace parameter is the owning CR's namespace, used as fallback when serviceRef.namespace is empty.
func WithPersistenceEnvVars(p *logicv1.PersistenceOptionsSpec, namespace string) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		envs := persistenceEnvVars(p, namespace)
		if len(envs) > 0 {
			c.WithEnv(envs...)
		}
	}
}

func persistenceEnvVars(p *logicv1.PersistenceOptionsSpec, namespace string) []*corev1ac.EnvVarApplyConfiguration {
	if p == nil || p.PostgreSQL == nil {
		return nil
	}

	pg := p.PostgreSQL
	envs := []*corev1ac.EnvVarApplyConfiguration{
		envLiteral("QUARKUS_DATASOURCE_DB_KIND", "postgresql"),
	}

	userKey := pg.SecretRef.UserKey
	if userKey == "" {
		userKey = defaultSecretUserKey
	}
	passwordKey := pg.SecretRef.PasswordKey
	if passwordKey == "" {
		passwordKey = defaultSecretPasswordKey
	}
	envs = append(envs,
		envFromSecret("QUARKUS_DATASOURCE_USERNAME", pg.SecretRef.Name, userKey),
		envFromSecret("QUARKUS_DATASOURCE_PASSWORD", pg.SecretRef.Name, passwordKey),
	)

	jdbcUrl := pg.JdbcUrl
	if jdbcUrl == "" && pg.ServiceRef != nil {
		jdbcUrl = buildJdbcUrl(pg.ServiceRef, namespace)
	}
	if pg.TLS != nil && pg.TLS.Enabled {
		mode := string(pg.TLS.TLSMode)
		if mode == "" {
			mode = string(logicv1.TLSModePrefer)
		}
		if strings.Contains(jdbcUrl, "?") {
			jdbcUrl += "&sslmode=" + mode
		} else {
			jdbcUrl += "?sslmode=" + mode
		}
	}
	if jdbcUrl != "" {
		envs = append(envs, envLiteral("QUARKUS_DATASOURCE_JDBC_URL", jdbcUrl))
	}

	return envs
}

// WithSecurityEnvVars returns a ContainerOption that appends Quarkus Flow runner security environment variables.
func WithSecurityEnvVars(sec logicv1.RuntimeSecuritySpec) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv(securityEnvVars(sec)...)
	}
}

// WithFlowSourcePath returns a ContainerOption that sets the QUARKUS_FLOW_RUNNER_SOURCE_PATH environment variable.
// Filters out any user-provided duplicate before adding the canonical value.
func WithFlowSourcePath() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		filtered := make([]corev1ac.EnvVarApplyConfiguration, 0, len(c.Env))
		for _, e := range c.Env {
			if e.Name != nil && *e.Name == "QUARKUS_FLOW_RUNNER_SOURCE_PATH" {
				continue
			}
			filtered = append(filtered, e)
		}
		c.Env = filtered
		c.WithEnv(envLiteral("QUARKUS_FLOW_RUNNER_SOURCE_PATH", WorkflowMountPath))
	}
}

// WithFlowVolumeMounts returns a ContainerOption that adds a read-only VolumeMount per ConfigMap data key.
// Uses subPath to mount each key as a direct file, avoiding ConfigMap symlink structures
// that cause duplicate detection in the runner's Files.walk() scanner.
// TODO(#22): revert to directory mounts once quarkiverse/quarkus-flow#835 is fixed.
func WithFlowVolumeMounts(configMaps []corev1.ConfigMap) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		for i := range configMaps {
			for key := range configMaps[i].Data {
				c.WithVolumeMounts(corev1ac.VolumeMount().
					WithName(configMaps[i].Name).
					WithMountPath(WorkflowMountPath + "/" + key).
					WithSubPath(key).
					WithReadOnly(true))
			}
		}
	}
}

// WithDurableEnvVars returns a ContainerOption that sets durable lease env vars.
// Filters user-provided duplicates for immutable env vars before adding operator values.
func WithDurableEnvVars(rt *logicv1.LogicFlowRuntime) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		immutable := map[string]bool{
			"QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED": true,
			"QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME":            true,
			"POD_NAME":                                       true,
			"POD_NAMESPACE":                                  true,
		}
		filtered := make([]corev1ac.EnvVarApplyConfiguration, 0, len(c.Env))
		for _, e := range c.Env {
			if e.Name != nil && immutable[*e.Name] {
				continue
			}
			filtered = append(filtered, e)
		}
		c.Env = filtered
		c.WithEnv(
			envLiteral("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED", "false"),
			envLiteral("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME", rt.Name),
			envFieldRef("POD_NAME", "metadata.name"),
			envFieldRef("POD_NAMESPACE", "metadata.namespace"),
		)
	}
}

func securityEnvVars(sec logicv1.RuntimeSecuritySpec) []*corev1ac.EnvVarApplyConfiguration {
	switch sec.Type {
	case logicv1.RuntimeSecurityAPIKey:
		return apiKeyEnvVars(sec.APIKey)
	case logicv1.RuntimeSecurityOIDC:
		return oidcEnvVars(sec.OIDC)
	default:
		return []*corev1ac.EnvVarApplyConfiguration{
			envLiteral("QUARKUS_FLOW_RUNNER_SECURITY_TYPE", "none"),
		}
	}
}

func apiKeyEnvVars(ak *logicv1.APIKeyAuthSpec) []*corev1ac.EnvVarApplyConfiguration {
	envs := []*corev1ac.EnvVarApplyConfiguration{
		envLiteral("QUARKUS_FLOW_RUNNER_SECURITY_TYPE", "api-key"),
	}
	if ak == nil {
		return envs
	}
	for _, key := range ak.Keys {
		prefix := fmt.Sprintf(`QUARKUS_FLOW_RUNNER_SECURITY_API_KEYS__"%s"`, key.Name)

		secretKey := key.SecretRef.Key
		if secretKey == "" {
			secretKey = "value"
		}
		envs = append(envs,
			envFromSecret(prefix+"__SECRET", key.SecretRef.Name, secretKey),
		)

		roles := make([]string, len(key.Roles))
		for i, r := range key.Roles {
			roles[i] = string(r)
		}
		envs = append(envs,
			envLiteral(prefix+"__ROLES", strings.Join(roles, ",")),
		)
	}
	return envs
}

func oidcEnvVars(oidc *logicv1.OIDCAuthSpec) []*corev1ac.EnvVarApplyConfiguration {
	envs := []*corev1ac.EnvVarApplyConfiguration{
		envLiteral("QUARKUS_FLOW_RUNNER_SECURITY_TYPE", "oidc"),
	}
	if oidc == nil {
		return envs
	}

	secretKey := oidc.ClientSecret.Key
	if secretKey == "" {
		secretKey = "value"
	}

	envs = append(envs,
		envLiteral("QUARKUS_OIDC_AUTH_SERVER_URL", oidc.AuthServerUrl),
		envLiteral("QUARKUS_OIDC_CLIENT_ID", oidc.ClientId),
		envFromSecret("QUARKUS_OIDC_CREDENTIALS_SECRET", oidc.ClientSecret.Name, secretKey),
	)

	if oidc.RolesClaim != "" && oidc.RolesClaim != "roles" {
		envs = append(envs, envLiteral("QUARKUS_OIDC_ROLES_CLAIM", oidc.RolesClaim))
	}

	return envs
}

func buildJdbcUrl(ref *logicv1.PostgreSQLServiceOptions, fallbackNamespace string) string {
	if ref.SQLServiceOptions == nil {
		return ""
	}
	ns := ref.Namespace
	if ns == "" {
		ns = fallbackNamespace
	}
	port := defaultPostgresPort
	if ref.Port != nil {
		port = *ref.Port
	}
	dbName := ref.DatabaseName
	if dbName == "" {
		dbName = defaultDatabaseName
	}
	url := fmt.Sprintf("jdbc:postgresql://%s.%s.svc:%d/%s", ref.Name, ns, port, dbName)
	if ref.DatabaseSchema != "" {
		url += "?currentSchema=" + ref.DatabaseSchema
	}
	return url
}

func envLiteral(name, value string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().WithName(name).WithValue(value)
}

func envFieldRef(name, fieldPath string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().
		WithName(name).
		WithValueFrom(corev1ac.EnvVarSource().
			WithFieldRef(corev1ac.ObjectFieldSelector().
				WithFieldPath(fieldPath)))
}

func envFromSecret(name, secretName, key string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().
		WithName(name).
		WithValueFrom(corev1ac.EnvVarSource().
			WithSecretKeyRef(corev1ac.SecretKeySelector().
				WithName(secretName).
				WithKey(key)))
}
