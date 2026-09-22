package controller

import corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

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
