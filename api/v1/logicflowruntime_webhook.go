package v1

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicflowruntime,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=create;update,versions=v1,name=mlogicflowruntime-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowRuntimeDefaulter struct{}

var _ admission.Defaulter[*LogicFlowRuntime] = &LogicFlowRuntimeDefaulter{}

// ApplyLogicFlowRuntimeDefaults applies runtime-specific defaults used by both
// the admission webhook and controller. ApplicationSpec is also used by
// platform components, where replicas must remain unset to allow HPA
// management, so this default is conditional on the resource kind.
func ApplyLogicFlowRuntimeDefaults(obj *LogicFlowRuntime) {
	if obj.Spec.Replicas == nil {
		one := int32(1)
		obj.Spec.Replicas = &one
	}
	if obj.Spec.PersistenceMode == nil {
		// Default to Ephemeral, but override for known runner images with specific variants
		mode := PersistenceModeEphemeral
		if isKnownRunnerImage(obj.Spec.Image) {
			// Known runner image: check variant suffix
			if strings.HasSuffix(obj.Spec.Image, "-"+ImageVariantStandard) {
				mode = PersistenceModePlatformDB
			} else if strings.HasSuffix(obj.Spec.Image, "-"+ImageVariantMinimal) {
				mode = PersistenceModeEphemeral
			}
		}
		obj.Spec.PersistenceMode = &mode
	}
}

func (d *LogicFlowRuntimeDefaulter) Default(_ context.Context, obj *LogicFlowRuntime) error {
	ApplyLogicFlowRuntimeDefaults(obj)
	return nil
}

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowruntime,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=create;update,versions=v1,name=vlogicflowruntime-v1.kb.io,admissionReviewVersions=v1

type LogicFlowRuntimeValidator struct{}

var _ admission.Validator[*LogicFlowRuntime] = &LogicFlowRuntimeValidator{}

func (v *LogicFlowRuntimeValidator) ValidateCreate(_ context.Context, obj *LogicFlowRuntime) (admission.Warnings, error) {
	return v.validate(obj)
}

func (v *LogicFlowRuntimeValidator) ValidateUpdate(_ context.Context, _, newObj *LogicFlowRuntime) (admission.Warnings, error) {
	return v.validate(newObj)
}

func (v *LogicFlowRuntimeValidator) ValidateDelete(_ context.Context, _ *LogicFlowRuntime) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowRuntimeValidator) validate(obj *LogicFlowRuntime) (admission.Warnings, error) {
	if err := ValidateSecuritySpec(obj.Spec.Security); err != nil {
		return nil, err
	}
	if obj.Spec.Image != "" {
		var effP *DatastoreSpec
		if !IsEphemeral(obj) {
			effP = &DatastoreSpec{PostgreSQL: &PersistencePostgreSQL{}}
		}
		if err := ValidateRunnerImage(obj.Spec.Image, effP); err != nil {
			return nil, err
		}
	}
	if err := v.validatePlatformRef(obj.Spec.PlatformRef); err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *LogicFlowRuntimeValidator) validatePlatformRef(platformRef *PlatformReference) error {
	// If platformRef is omitted, auto-discovery is used (allowed)
	if platformRef == nil {
		return nil
	}

	// If Kind is set, it must equal ClusterLogicPlatform
	if platformRef.Kind != "" && platformRef.Kind != "ClusterLogicPlatform" {
		return fmt.Errorf("spec.platformRef.kind must be 'ClusterLogicPlatform', got %q", platformRef.Kind)
	}

	// Name must be non-empty when platformRef is present
	if platformRef.Name == "" {
		return fmt.Errorf("spec.platformRef.name is required when platformRef is set")
	}

	return nil
}
