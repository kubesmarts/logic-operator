package v1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testClusterLogicPlatformValidator(objs ...runtime.Object) *ClusterLogicPlatformValidator {
	scheme := runtime.NewScheme()
	_ = AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objs) > 0 {
		builder = builder.WithRuntimeObjects(objs...)
	}
	return &ClusterLogicPlatformValidator{Reader: builder.Build()}
}

func TestValidateCreate_RejectsSecondPlatform(t *testing.T) {
	existingPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-platform"},
		Spec:       ClusterLogicPlatformSpec{TargetNamespace: "workflows"},
	}
	validator := testClusterLogicPlatformValidator(existingPlatform)
	newPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "new-platform"},
		Spec:       ClusterLogicPlatformSpec{TargetNamespace: "workflows"},
	}

	_, err := validator.ValidateCreate(context.Background(), newPlatform)
	if err == nil {
		t.Fatal("Expected error when creating second ClusterLogicPlatform, got nil")
	}
}

func TestValidateCreate_AllowsFirstPlatform(t *testing.T) {
	validator := testClusterLogicPlatformValidator()

	platform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "first-platform",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
		},
	}

	_, err := validator.ValidateCreate(context.Background(), platform)
	if err != nil {
		t.Fatalf("Expected no error when creating first ClusterLogicPlatform, got: %v", err)
	}
}

func TestValidate_RejectsEmptyTargetNamespace(t *testing.T) {
	validator := testClusterLogicPlatformValidator()

	platform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "",
		},
	}

	_, err := validator.ValidateCreate(context.Background(), platform)
	if err == nil {
		t.Fatal("Expected error when creating ClusterLogicPlatform with empty targetNamespace, got nil")
	}
}

func TestValidateUpdate_RejectsEmptyTargetNamespace(t *testing.T) {
	existingPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
		},
	}

	validator := testClusterLogicPlatformValidator(existingPlatform)

	updatedPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-platform",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "",
		},
	}

	_, err := validator.ValidateUpdate(context.Background(), existingPlatform, updatedPlatform)
	if err == nil {
		t.Fatal("Expected error when updating ClusterLogicPlatform with empty targetNamespace, got nil")
	}
}

func TestValidateUpdate_RejectsTargetNamespaceChange(t *testing.T) {
	oldPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "test-platform"},
		Spec:       ClusterLogicPlatformSpec{TargetNamespace: "workflows"},
	}
	newPlatform := oldPlatform.DeepCopy()
	newPlatform.Spec.TargetNamespace = "workflows-new"

	validator := testClusterLogicPlatformValidator(oldPlatform)
	_, err := validator.ValidateUpdate(context.Background(), oldPlatform, newPlatform)
	if err == nil {
		t.Fatal("Expected targetNamespace change to be rejected, got nil")
	}
	if got, want := err.Error(), "spec.targetNamespace is immutable; delete and recreate ClusterLogicPlatform to change it"; got != want {
		t.Fatalf("Expected error %q, got %q", want, got)
	}
}

func TestValidateCreate_RequiresDataIndexPostgreSQLPersistence(t *testing.T) {
	tests := []struct {
		name        string
		persistence *DatastoreSpec
		wantError   string
	}{
		{name: "missing persistence", wantError: "spec.dataIndex.persistence.postgresql is required when Data Index is enabled"},
		{name: "missing PostgreSQL", persistence: &DatastoreSpec{}, wantError: "spec.dataIndex.persistence.postgresql is required when Data Index is enabled"},
		{
			name: "PostgreSQL configured",
			persistence: &DatastoreSpec{
				PostgreSQL: validDataIndexPostgreSQL(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := testClusterLogicPlatformValidator()
			platform := &ClusterLogicPlatform{
				ObjectMeta: metav1.ObjectMeta{Name: "test-platform"},
				Spec: ClusterLogicPlatformSpec{
					TargetNamespace: "workflows",
					DataIndex:       &DataIndexSpec{Persistence: tt.persistence},
				},
			}

			_, err := validator.ValidateCreate(context.Background(), platform)
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("Expected error %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Expected configured Data Index persistence to pass validation, got: %v", err)
			}
		})
	}
}

func TestValidateCreate_RequiresDataIndexPostgreSQLReferenceFields(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*PersistencePostgreSQL)
		wantError string
	}{
		{
			name:      "secret name",
			mutate:    func(pg *PersistencePostgreSQL) { pg.SecretRef.Name = "" },
			wantError: "spec.dataIndex.persistence.postgresql.secretRef.name is required",
		},
		{
			name:      "service name",
			mutate:    func(pg *PersistencePostgreSQL) { pg.ServiceRef.Name = "" },
			wantError: "spec.dataIndex.persistence.postgresql.serviceRef.name is required",
		},
		{
			name:      "database name",
			mutate:    func(pg *PersistencePostgreSQL) { pg.ServiceRef.DatabaseName = "" },
			wantError: "spec.dataIndex.persistence.postgresql.serviceRef.databaseName is required",
		},
		{
			name:      "database schema",
			mutate:    func(pg *PersistencePostgreSQL) { pg.ServiceRef.DatabaseSchema = "" },
			wantError: "spec.dataIndex.persistence.postgresql.serviceRef.databaseSchema is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postgresql := validDataIndexPostgreSQL()
			tt.mutate(postgresql)
			platform := &ClusterLogicPlatform{
				ObjectMeta: metav1.ObjectMeta{Name: "test-platform"},
				Spec: ClusterLogicPlatformSpec{
					TargetNamespace: "workflows",
					DataIndex: &DataIndexSpec{Persistence: &DatastoreSpec{
						PostgreSQL: postgresql,
					}},
				},
			}
			validator := testClusterLogicPlatformValidator()
			_, err := validator.ValidateCreate(context.Background(), platform)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("Expected error %q, got %v", tt.wantError, err)
			}
		})
	}
}

func validDataIndexPostgreSQL() *PersistencePostgreSQL {
	return &PersistencePostgreSQL{
		SecretRef: PostgreSQLSecretOptions{Name: "data-index-db"},
		ServiceRef: PostgreSQLServiceOptions{
			Name:           "postgresql",
			DatabaseName:   "logicflow",
			DatabaseSchema: "data_index",
		},
	}
}

func TestValidateCreate_RequiresDataIndexWhenVectorIsEnabled(t *testing.T) {
	validator := testClusterLogicPlatformValidator()
	platform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "test-platform"},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
			Vector:          &VectorSpec{},
		},
	}

	_, err := validator.ValidateCreate(context.Background(), platform)
	if err == nil {
		t.Fatal("Expected Vector without Data Index to be rejected, got nil")
	}
	if got, want := err.Error(), "spec.dataIndex is required when Vector is enabled"; got != want {
		t.Fatalf("Expected error %q, got %q", want, got)
	}
}
