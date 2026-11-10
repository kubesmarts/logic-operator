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

func TestValidateCreate_RequiresDataIndexPostgreSQLPersistence(t *testing.T) {
	tests := []struct {
		name        string
		persistence *DatastoreSpec
		wantError   bool
	}{
		{name: "missing persistence", wantError: true},
		{name: "missing PostgreSQL", persistence: &DatastoreSpec{}, wantError: true},
		{
			name: "PostgreSQL configured",
			persistence: &DatastoreSpec{
				PostgreSQL: &PersistencePostgreSQL{},
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
			if tt.wantError {
				if err == nil {
					t.Fatal("Expected Data Index persistence validation error, got nil")
				}
				if got, want := err.Error(), "spec.dataIndex.persistence.postgresql is required when Data Index is enabled"; got != want {
					t.Fatalf("Expected error %q, got %q", want, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Expected configured Data Index persistence to pass validation, got: %v", err)
			}
		})
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
