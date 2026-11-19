package v1

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-workflow-specification/sdk-go/v4/model"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicflowdefinition,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=create;update,versions=v1,name=mlogicflowdefinition-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowDefinitionDefaulter struct{}

var _ admission.Defaulter[*LogicFlowDefinition] = &LogicFlowDefinitionDefaulter{}

func (d *LogicFlowDefinitionDefaulter) Default(_ context.Context, obj *LogicFlowDefinition) error {
	wf, err := obj.Spec.ParseFlow()
	if err != nil {
		// Let the validating webhook report malformed flow documents.
		return nil
	}
	_, err = NormalizeLogicFlowDefinitionNamespace(obj, wf)
	return err
}

// NormalizeLogicFlowDefinitionNamespace makes the CR's Kubernetes namespace
// authoritative for the workflow document and stores the normalized document
// back in spec.flow. The parsed workflow is updated too so reconcilers can use
// the same value without reparsing.
func NormalizeLogicFlowDefinitionNamespace(obj *LogicFlowDefinition, wf *model.Workflow) (bool, error) {
	if wf.Document.Namespace == obj.Namespace {
		return false, nil
	}
	wf.Document.Namespace = obj.Namespace
	raw, err := json.Marshal(wf)
	if err != nil {
		return false, fmt.Errorf("marshal normalized spec.flow: %w", err)
	}
	obj.Spec.Flow.Raw = raw
	obj.Spec.Flow.Object = nil
	return true, nil
}

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowdefinition,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=create;update,versions=v1,name=vlogicflowdefinition-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowDefinitionValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*LogicFlowDefinition] = &LogicFlowDefinitionValidator{}

func (v *LogicFlowDefinitionValidator) ValidateCreate(ctx context.Context, obj *LogicFlowDefinition) (admission.Warnings, error) {
	if obj.Spec.RuntimeRef.Name == "" {
		return nil, fmt.Errorf("spec.runtimeRef.name is required")
	}
	wf, err := obj.Spec.ParseFlow()
	if err != nil {
		return nil, fmt.Errorf("spec.flow: %w", err)
	}
	if err := v.validateSameRuntime(ctx, obj, wf.Document.Name); err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateUpdate(_ context.Context, oldObj, newObj *LogicFlowDefinition) (admission.Warnings, error) {
	if oldObj.Spec.RuntimeRef.Name != newObj.Spec.RuntimeRef.Name {
		return nil, fmt.Errorf("spec.runtimeRef is immutable; delete and recreate to change the target runtime")
	}
	newWf, err := newObj.Spec.ParseFlow()
	if err != nil {
		return nil, fmt.Errorf("spec.flow: %w", err)
	}
	oldWf, err := oldObj.Spec.ParseFlow()
	if err != nil {
		return nil, fmt.Errorf("failed to parse old workflow object: %w", err)
	}
	if oldWf.Document.Name != newWf.Document.Name {
		return nil, fmt.Errorf("flow document name is immutable")
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateDelete(_ context.Context, _ *LogicFlowDefinition) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) validateSameRuntime(ctx context.Context, obj *LogicFlowDefinition, workflowName string) error {
	var existing LogicFlowDefinitionList
	if err := v.Reader.List(ctx, &existing,
		client.InNamespace(obj.Namespace),
		client.MatchingLabels{
			LabelWorkflowName:      workflowName,
			LabelWorkflowNamespace: obj.Namespace,
		},
	); err != nil {
		return fmt.Errorf("failed to list existing definitions: %w", err)
	}
	for i := range existing.Items {
		if existing.Items[i].Name == obj.Name {
			continue
		}
		if existing.Items[i].Spec.RuntimeRef.Name != obj.Spec.RuntimeRef.Name {
			return fmt.Errorf(
				"workflow %s/%s is already deployed on runtime %q; all versions must share the same runtime",
				obj.Namespace, workflowName, existing.Items[i].Spec.RuntimeRef.Name,
			)
		}
	}
	return nil
}
