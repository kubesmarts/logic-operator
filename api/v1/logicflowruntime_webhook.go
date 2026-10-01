package v1

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicflowruntime,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=create;update,versions=v1,name=mlogicflowruntime-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowRuntimeDefaulter struct{}

var _ admission.Defaulter[*LogicFlowRuntime] = &LogicFlowRuntimeDefaulter{}

// Default sets Replicas to 1 if not specified.
// We use a webhook instead of +kubebuilder:default=1 because ApplicationSpec is also used by LogicPlatform.DataIndex,
// which needs to support HPA management without operator enforcement. The webhook allows us to apply the default
// conditionally only for LogicFlowRuntime, leaving DataIndex.Application.Replicas nil for HPA to manage freely.
func (d *LogicFlowRuntimeDefaulter) Default(_ context.Context, obj *LogicFlowRuntime) error {
	if obj.Spec.Replicas == nil {
		one := int32(1)
		obj.Spec.Replicas = &one
	}
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
		if err := ValidateRunnerImage(obj.Spec.Image, obj.Spec.Persistence); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
