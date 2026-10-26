package lease

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolverPodFor(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, coordinationv1.AddToScheme(scheme))

	t.Run("lease exists with holder identity", func(t *testing.T) {
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-lease",
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: stringPtr("pod-1"),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(lease).Build()
		resolver := NewResolver(c)

		pod, found, err := resolver.PodFor(context.Background(), "default", "test-lease")

		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "pod-1", pod)
	})

	t.Run("lease does not exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		resolver := NewResolver(c)

		pod, found, err := resolver.PodFor(context.Background(), "default", "nonexistent")

		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, "", pod)
	})

	t.Run("lease exists but holder identity is nil", func(t *testing.T) {
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-lease",
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: nil,
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(lease).Build()
		resolver := NewResolver(c)

		pod, found, err := resolver.PodFor(context.Background(), "default", "test-lease")

		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, "", pod)
	})
}

func stringPtr(s string) *string {
	return &s
}
