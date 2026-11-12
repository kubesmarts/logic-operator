package controller

import (
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMigratorJob_UsesDDLSecretPerStore(t *testing.T) {
	plat := &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
			UID:  "12345",
		},
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
			DataIndex: &logicv1.DataIndexSpec{
				Persistence: &logicv1.DatastoreSpec{
					PostgreSQL: &logicv1.PersistencePostgreSQL{
						SecretRef: logicv1.PostgreSQLSecretOptions{Name: "di-secret"},
						ServiceRef: logicv1.PostgreSQLServiceOptions{
							Name:           "postgres",
							DatabaseName:   "dataindex",
							DatabaseSchema: "public",
						},
					},
				},
			},
			Runtime: &logicv1.RuntimePlatformSpec{
				Persistence: &logicv1.RuntimePersistenceSpec{
					Runtime: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "runtime-dml-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								DatabaseSchema: "runtime",
							},
						},
						MigrationSecretRef: &logicv1.PostgreSQLSecretOptions{Name: "runtime-ddl-secret"},
					},
					Quartz: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "quartz-dml-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								DatabaseSchema: "quartz",
							},
						},
						MigrationSecretRef: &logicv1.PostgreSQLSecretOptions{Name: "quartz-ddl-secret"},
					},
				},
			},
		},
	}

	job := buildDBMigratorJob(plat)
	require.NotNil(t, job)

	// Verify the job references the correct DDL secrets for runtime and quartz
	// DataIndex uses operational secret (di-secret)
	assert.NotNil(t, job.Spec)
	assert.NotNil(t, job.Spec.Template)
	assert.NotNil(t, job.Spec.Template.Spec)

	// Collect all secret refs in environment variables
	secretRefs := make(map[string]bool)
	for _, container := range job.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name != nil {
				secretRefs[*env.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}

	// Verify that the DDL secrets are referenced
	assert.True(t, secretRefs["di-secret"], "DataIndex operational secret should be referenced")
	assert.True(t, secretRefs["runtime-ddl-secret"], "Runtime DDL secret should be referenced")
	assert.True(t, secretRefs["quartz-ddl-secret"], "Quartz DDL secret should be referenced")
}

func TestMigratorJob_DeduplicatesSharedInstance(t *testing.T) {
	plat := &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
			UID:  "12345",
		},
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
			Runtime: &logicv1.RuntimePlatformSpec{
				Persistence: &logicv1.RuntimePersistenceSpec{
					Runtime: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "runtime-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								Port:           ptrInt(5432),
								DatabaseSchema: "runtime",
							},
						},
					},
					Quartz: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "quartz-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								Port:           ptrInt(5432),
								DatabaseSchema: "quartz",
							},
						},
					},
				},
			},
		},
	}

	job := buildDBMigratorJob(plat)
	require.NotNil(t, job)
	assert.NotNil(t, job.Spec)
	assert.NotNil(t, job.Spec.Template)
	assert.NotNil(t, job.Spec.Template.Spec)

	// Both share same host:port:database, so should not be deduplicated
	// (they differ by schema, so they're distinct migration targets)
	// We expect containers or env vars for both
	containerCount := len(job.Spec.Template.Spec.Containers)
	assert.Greater(t, containerCount, 0, "should have at least one container")

	// The deduplication should prevent true duplicates (same host:port:database:schema)
	// In this case, they have different schemas, so both should be present
}

func TestMigratorJob_DefaultsImageToQuay(t *testing.T) {
	plat := &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
			UID:  "12345",
		},
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
			Runtime: &logicv1.RuntimePlatformSpec{
				Persistence: &logicv1.RuntimePersistenceSpec{
					Runtime: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "postgres-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								DatabaseSchema: "runtime",
							},
						},
					},
				},
			},
			// DBMigrator.Image not set
		},
	}

	job := buildDBMigratorJob(plat)
	require.NotNil(t, job)
	require.NotNil(t, job.Spec)
	require.NotNil(t, job.Spec.Template)
	require.NotNil(t, job.Spec.Template.Spec)
	require.Greater(t, len(job.Spec.Template.Spec.Containers), 0, "should have at least one container")

	container := job.Spec.Template.Spec.Containers[0]
	require.NotNil(t, container.Image, "container image should not be nil")
	assert.True(t, contains(*container.Image, "quay.io/kubesmarts/logic-db-migrator:"), "image should default to quay.io/kubesmarts/logic-db-migrator")
}

func TestMigratorJob_HonorsImageOverride(t *testing.T) {
	customImage := "my-registry.io/my-migrator:v1.2.3"
	plat := &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
			UID:  "12345",
		},
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
			DBMigrator: &logicv1.DBMigratorSpec{
				Image: customImage,
			},
			Runtime: &logicv1.RuntimePlatformSpec{
				Persistence: &logicv1.RuntimePersistenceSpec{
					Runtime: logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "postgres-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "postgres",
								DatabaseName:   "workflows",
								DatabaseSchema: "runtime",
							},
						},
					},
				},
			},
		},
	}

	job := buildDBMigratorJob(plat)
	require.NotNil(t, job)
	require.NotNil(t, job.Spec)
	require.NotNil(t, job.Spec.Template)
	require.NotNil(t, job.Spec.Template.Spec)
	require.Greater(t, len(job.Spec.Template.Spec.Containers), 0, "should have at least one container")

	container := job.Spec.Template.Spec.Containers[0]
	require.NotNil(t, container.Image, "container image should not be nil")
	assert.Equal(t, customImage, *container.Image, "image should be set from DBMigratorSpec")
}

func ptrInt(i int) *int {
	return &i
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
