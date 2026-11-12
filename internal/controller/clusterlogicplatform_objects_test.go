package controller

import (
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

func TestVectorUsesDataIndexPersistenceAndConfiguredWatchNamespace(t *testing.T) {
	platform := &logicv1.ClusterLogicPlatform{
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: "platform-ns",
			DataIndex: &logicv1.DataIndexSpec{
				Persistence: &logicv1.DatastoreSpec{PostgreSQL: &logicv1.PersistencePostgreSQL{
					SecretRef: logicv1.PostgreSQLSecretOptions{
						Name:        "data-index-secret",
						UserKey:     "DB_USER",
						PasswordKey: "DB_PASSWORD",
					},
					ServiceRef: logicv1.PostgreSQLServiceOptions{
						Name:           "data-index-db",
						Namespace:      "database-ns",
						DatabaseName:   "logicflow",
						DatabaseSchema: "data_index",
					},
				}},
			},
			Runtime: &logicv1.RuntimePlatformSpec{Persistence: &logicv1.RuntimePersistenceSpec{
				Runtime: logicv1.DatastoreSpec{PostgreSQL: &logicv1.PersistencePostgreSQL{
					SecretRef: logicv1.PostgreSQLSecretOptions{Name: "runtime-secret"},
					ServiceRef: logicv1.PostgreSQLServiceOptions{
						Name: "runtime-db", Namespace: "runtime-db-ns", DatabaseSchema: "runtime",
					},
				}},
			}},
			Vector: &logicv1.VectorSpec{WatchNamespaces: []string{"workflow-ns"}},
		},
	}

	container := corev1ac.Container().WithName(ContainerNameVector)
	WithVectorEnvVarsCLP(platform)(container)

	env := make(map[string]corev1ac.EnvVarApplyConfiguration, len(container.Env))
	for _, entry := range container.Env {
		env[*entry.Name] = entry
	}

	assertEnvValue := func(name, want string) {
		t.Helper()
		entry, found := env[name]
		if !found || entry.Value == nil || *entry.Value != want {
			got := "<missing>"
			if found && entry.Value != nil {
				got = *entry.Value
			}
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	assertEnvValue("WORKFLOW_NAMESPACE", "workflow-ns")
	assertEnvValue("POSTGRES_HOST", "data-index-db.database-ns.svc.cluster.local")
	assertEnvValue("POSTGRES_DB", "logicflow")
	assertEnvValue("POSTGRES_SCHEMA", "data_index")

	for name, key := range map[string]string{"POSTGRES_USER": "DB_USER", "POSTGRES_PASSWORD": "DB_PASSWORD"} {
		entry, found := env[name]
		if !found || entry.ValueFrom == nil || entry.ValueFrom.SecretKeyRef == nil {
			t.Errorf("%s is not sourced from the Data Index credentials secret", name)
			continue
		}
		if got := *entry.ValueFrom.SecretKeyRef.Name; got != "data-index-secret" {
			t.Errorf("%s secret = %q, want data-index-secret", name, got)
		}
		if got := *entry.ValueFrom.SecretKeyRef.Key; got != key {
			t.Errorf("%s key = %q, want %q", name, got, key)
		}
	}

	volumes := VectorVolumesCLP(platform)
	if len(volumes) == 0 || volumes[len(volumes)-1].Name == nil || *volumes[len(volumes)-1].Name != VectorConfigVolumeName {
		t.Fatal("Vector config ConfigMap volume was not mounted for Data Index persistence")
	}
}
