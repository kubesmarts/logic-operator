package v1

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

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
