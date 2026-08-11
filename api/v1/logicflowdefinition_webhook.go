package v1

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowdefinition,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=create;update,versions=v1,name=vlogicflowdefinition-v1.kb.io,admissionReviewVersions=v1

type LogicFlowDefinitionValidator struct{}

var _ admission.Validator[*LogicFlowDefinition] = &LogicFlowDefinitionValidator{}

func (v *LogicFlowDefinitionValidator) ValidateCreate(_ context.Context, obj *LogicFlowDefinition) (admission.Warnings, error) {
	if obj.Spec.RuntimeRef.Name == "" {
		return nil, fmt.Errorf("spec.runtimeRef.name is required")
	}
	if _, err := obj.Spec.ParseFlow(); err != nil {
		return nil, fmt.Errorf("spec.flow: %w", err)
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateUpdate(_ context.Context, oldObj, newObj *LogicFlowDefinition) (admission.Warnings, error) {
	if oldObj.Spec.RuntimeRef.Name != newObj.Spec.RuntimeRef.Name {
		return nil, fmt.Errorf("spec.runtimeRef is immutable; delete and recreate to change the target runtime")
	}
	if _, err := newObj.Spec.ParseFlow(); err != nil {
		return nil, fmt.Errorf("spec.flow: %w", err)
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateDelete(_ context.Context, _ *LogicFlowDefinition) (admission.Warnings, error) {
	return nil, nil
}
