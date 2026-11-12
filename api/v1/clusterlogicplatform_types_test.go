package v1

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestClusterLogicPlatform_DeepCopyObjectRoundTrip(t *testing.T) {
	g := gomega.NewWithT(t)

	// Create a ClusterLogicPlatform with sample data
	original := &ClusterLogicPlatform{
		Spec: ClusterLogicPlatformSpec{
			TargetNamespace: "workflows",
		},
	}
	original.Name = "test-platform"

	// Test round-trip through DeepCopyObject (runtime.Object interface)
	copied := original.DeepCopyObject()
	g.Expect(copied).NotTo(gomega.BeNil())
	g.Expect(copied).To(gomega.BeAssignableToTypeOf(&ClusterLogicPlatform{}))

	copiedCLP := copied.(*ClusterLogicPlatform)
	g.Expect(copiedCLP.Name).To(gomega.Equal(original.Name))
	g.Expect(copiedCLP.Spec.TargetNamespace).To(gomega.Equal(original.Spec.TargetNamespace))

	// Verify it's a deep copy, not a shallow one
	copiedCLP.Spec.TargetNamespace = "modified"
	g.Expect(original.Spec.TargetNamespace).To(gomega.Equal("workflows"))
	g.Expect(copiedCLP.Spec.TargetNamespace).To(gomega.Equal("modified"))
}
