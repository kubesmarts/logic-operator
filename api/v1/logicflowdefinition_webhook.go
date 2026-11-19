package v1

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-workflow-specification/sdk-go/v4/model"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
	allErrs := field.ErrorList{}
	specFieldPath := field.NewPath("spec")
	if obj.Spec.RuntimeRef.Name == "" {
		allErrs = append(allErrs, field.Required(specFieldPath.Child("runtimeRef", "name"), "must be specified"))
	} else if len(validation.IsDNS1123Label(obj.Spec.RuntimeRef.Name)) > 0 {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("runtimeRef", "name"), obj.Spec.RuntimeRef.Name, "does not conform to a valid logicflowruntime name"))
	}
	if len(obj.Spec.Flow.Raw) == 0 {
		return nil, append(allErrs, field.Required(specFieldPath.Child("flow"), "must be specified")).ToAggregate()
	}
	wf, err := obj.Spec.ParseFlow()
	if err != nil {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("flow"), "<value omitted>", fmt.Sprintf("failed to parse workflow: %v", err)))
	}
	if len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}
	return nil, v.validateSameRuntime(ctx, obj, wf.Document.Name).ToAggregate()
}

func (v *LogicFlowDefinitionValidator) ValidateUpdate(_ context.Context, oldObj, newObj *LogicFlowDefinition) (admission.Warnings, error) {
	allErrs := field.ErrorList{}
	specFieldPath := field.NewPath("spec")
	if oldObj.Spec.RuntimeRef.Name != newObj.Spec.RuntimeRef.Name {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("runtimeRef", "name"), newObj.Spec.RuntimeRef.Name, "field is immutable; delete and recreate to change the target runtime"))
	}
	newWf, err := newObj.Spec.ParseFlow()
	if err != nil {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("flow"), "<new value omitted>", fmt.Sprintf("failed to parse workflow: %v", err)))
		return nil, allErrs.ToAggregate()
	}
	oldWf, err := oldObj.Spec.ParseFlow()
	if err != nil {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("flow"), "<old value omitted>", fmt.Sprintf("failed to parse old workflow: %v", err)))
		return nil, allErrs.ToAggregate()
	}
	if oldWf.Document.Name != newWf.Document.Name {
		allErrs = append(allErrs, field.Invalid(specFieldPath.Child("flow", "document", "name"), newWf.Document.Name, "field is immutable"))
	}
	return nil, allErrs.ToAggregate()
}

func (v *LogicFlowDefinitionValidator) ValidateDelete(_ context.Context, _ *LogicFlowDefinition) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) validateSameRuntime(ctx context.Context, obj *LogicFlowDefinition, workflowName string) field.ErrorList {
	allErrs := field.ErrorList{}
	fieldPath := field.NewPath("spec", "runtimeRef", "name")
	var existing LogicFlowDefinitionList
	if err := v.Reader.List(ctx, &existing,
		client.InNamespace(obj.Namespace),
		client.MatchingLabels{
			LabelWorkflowName:      workflowName,
			LabelWorkflowNamespace: obj.Namespace,
		},
	); err != nil {
		return append(allErrs, field.InternalError(fieldPath, fmt.Errorf("failed to list existing definitions: %w", err)))
	}
	for i := range existing.Items {
		if existing.Items[i].Name == obj.Name {
			continue
		}
		if existing.Items[i].Spec.RuntimeRef.Name != obj.Spec.RuntimeRef.Name {
			allErrs = append(allErrs, field.Invalid(
				fieldPath,
				obj.Spec.RuntimeRef.Name,
				fmt.Sprintf("workflow %s/%s is already deployed on runtime %q; all versions must share the same runtime",
					obj.Namespace, workflowName, existing.Items[i].Spec.RuntimeRef.Name),
			))
		}
	}
	return allErrs
}
