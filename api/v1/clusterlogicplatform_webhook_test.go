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
	// Create a fake client with an existing ClusterLogicPlatform
	existingPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "existing-platform",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
		},
	}

	validator := testClusterLogicPlatformValidator(existingPlatform)

	newPlatform := &ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{
			Name: "new-platform",
			UID:  "new-uid",
		},
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
		},
	}

	_, err := validator.ValidateCreate(context.Background(), newPlatform)
	if err == nil {
		t.Fatal("Expected error when creating second ClusterLogicPlatform, got nil")
	}
}

func TestValidateCreate_AllowsFirstPlatform(t *testing.T) {
	// Create a fake client with no existing ClusterLogicPlatforms
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
