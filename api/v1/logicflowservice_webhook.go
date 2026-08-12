package v1

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowservices,verbs=create;update,versions=v1,name=vlogicflowservice-v1.kb.io,admissionReviewVersions=v1

type LogicFlowServiceValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*LogicFlowService] = &LogicFlowServiceValidator{}

func (v *LogicFlowServiceValidator) ValidateCreate(ctx context.Context, obj *LogicFlowService) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *LogicFlowServiceValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *LogicFlowService) (admission.Warnings, error) {
	oldType := oldObj.Spec.Ingress.ControllerType
	newType := newObj.Spec.Ingress.ControllerType
	if oldType != newType && (oldType == "openshift" || newType == "openshift") {
		return nil, fmt.Errorf("spec.ingress.controllerType: cannot switch between openshift and other controller types")
	}
	return nil, v.validate(ctx, newObj)
}

func (v *LogicFlowServiceValidator) ValidateDelete(_ context.Context, _ *LogicFlowService) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowServiceValidator) validate(ctx context.Context, obj *LogicFlowService) error {
	if err := v.validateTrafficConfig(obj); err != nil {
		return err
	}
	if err := v.validateIngress(obj); err != nil {
		return err
	}
	if err := v.validateTLS(obj); err != nil {
		return err
	}
	return v.validateDefinitions(ctx, obj)
}

func (v *LogicFlowServiceValidator) validateTrafficConfig(obj *LogicFlowService) error {
	hasTraffic := len(obj.Spec.Traffic) > 0
	hasDefault := obj.Spec.DefaultDefinition != nil && obj.Spec.DefaultDefinition.Name != ""

	if hasTraffic && hasDefault {
		return fmt.Errorf("spec.traffic and spec.defaultDefinition are mutually exclusive")
	}
	if !hasTraffic && !hasDefault {
		return fmt.Errorf("one of spec.traffic or spec.defaultDefinition is required")
	}
	if hasTraffic {
		if total := obj.Spec.TotalWeight(); total != 100 {
			return fmt.Errorf("spec.traffic weights must sum to 100, got %d", total)
		}
	}
	return nil
}

func (v *LogicFlowServiceValidator) validateIngress(obj *LogicFlowService) error {
	if obj.Spec.Ingress.Enabled && obj.Spec.Ingress.Host == "" {
		return fmt.Errorf("spec.ingress.host is required when ingress is enabled")
	}
	return nil
}

func (v *LogicFlowServiceValidator) validateTLS(obj *LogicFlowService) error {
	tls := obj.Spec.Ingress.TLS
	if !tls.Enabled {
		return nil
	}
	hasSecret := tls.SecretRef.Name != ""
	hasCM := tls.CertManager != nil
	if hasSecret && hasCM {
		return fmt.Errorf("spec.ingress.tls.secretRef and spec.ingress.tls.certManager are mutually exclusive")
	}
	if hasCM && tls.CertManager.IssuerRef.Name == "" {
		return fmt.Errorf("spec.ingress.tls.certManager.issuerRef.name is required")
	}
	return nil
}

func (v *LogicFlowServiceValidator) validateDefinitions(ctx context.Context, obj *LogicFlowService) error {
	refs := v.resolveDefinitionRefs(obj)
	if len(refs) == 0 {
		return nil
	}

	var runtimeName string
	var workflowName string

	for i, ref := range refs {
		var def LogicFlowDefinition
		if err := v.Reader.Get(ctx, client.ObjectKey{Name: ref, Namespace: obj.Namespace}, &def); err != nil {
			return fmt.Errorf("definition %q not found", ref)
		}

		if i == 0 {
			runtimeName = def.Spec.RuntimeRef.Name
		} else if def.Spec.RuntimeRef.Name != runtimeName {
			return fmt.Errorf(
				"definition %q targets runtime %q, expected %q; all definitions must target the same runtime",
				ref, def.Spec.RuntimeRef.Name, runtimeName)
		}

		wfName := v.resolveWorkflowName(&def)
		if wfName != "" {
			if workflowName == "" {
				workflowName = wfName
			} else if wfName != workflowName {
				return fmt.Errorf(
					"definition %q has workflow name %q, expected %q; all definitions must be versions of the same workflow",
					ref, wfName, workflowName)
			}
		}
	}
	return nil
}

func (v *LogicFlowServiceValidator) resolveDefinitionRefs(obj *LogicFlowService) []string {
	if obj.Spec.DefaultDefinition != nil && obj.Spec.DefaultDefinition.Name != "" {
		return []string{obj.Spec.DefaultDefinition.Name}
	}
	refs := make([]string, 0, len(obj.Spec.Traffic))
	for _, t := range obj.Spec.Traffic {
		refs = append(refs, t.DefinitionRef.Name)
	}
	return refs
}

func (v *LogicFlowServiceValidator) resolveWorkflowName(def *LogicFlowDefinition) string {
	if name := def.Labels[LabelWorkflowName]; name != "" {
		return name
	}
	wf, err := def.Spec.ParseFlow()
	if err != nil {
		return ""
	}
	return wf.Document.Name
}
