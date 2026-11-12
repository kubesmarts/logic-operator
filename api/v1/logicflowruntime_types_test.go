package v1

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestLogicFlowRuntime_PlatformRefCanBeSet(t *testing.T) {
	g := gomega.NewWithT(t)

	// Test that PlatformReference selects a platform by name
	ref := &PlatformReference{
		Name: "my-platform",
	}
	g.Expect(ref.Name).To(gomega.Equal("my-platform"))

	// Test that a runtime can hold a PlatformRef
	rt := &LogicFlowRuntime{
		Spec: LogicFlowRuntimeSpec{
			PlatformRef: ref,
		},
	}
	g.Expect(rt.Spec.PlatformRef).NotTo(gomega.BeNil())
	g.Expect(rt.Spec.PlatformRef.Name).To(gomega.Equal("my-platform"))
}

func TestLogicFlowRuntime_IsEphemeral(t *testing.T) {
	g := gomega.NewWithT(t)

	// Test 1: When PersistenceMode is Ephemeral, IsEphemeral returns true
	mode := PersistenceModeEphemeral
	rt := &LogicFlowRuntime{
		Spec: LogicFlowRuntimeSpec{
			PersistenceMode: &mode,
		},
	}
	g.Expect(IsEphemeral(rt)).To(gomega.BeTrue())

	// Test 2: When PersistenceMode is PlatformDB, IsEphemeral returns false
	mode2 := PersistenceModePlatformDB
	rt2 := &LogicFlowRuntime{
		Spec: LogicFlowRuntimeSpec{
			PersistenceMode: &mode2,
		},
	}
	g.Expect(IsEphemeral(rt2)).To(gomega.BeFalse())

	// Test 3: When PersistenceMode is nil, IsEphemeral returns false (nil is not Ephemeral)
	rt3 := &LogicFlowRuntime{
		Spec: LogicFlowRuntimeSpec{
			PersistenceMode: nil,
		},
	}
	g.Expect(IsEphemeral(rt3)).To(gomega.BeFalse())

	// Test 4: When runtime is nil, IsEphemeral returns false
	g.Expect(IsEphemeral(nil)).To(gomega.BeFalse())
}
