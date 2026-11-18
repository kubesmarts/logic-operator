package v1

import (
	"context"
	"fmt"

	"github.com/kubesmarts/logic-operator/utils"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const NginxIngress = "nginx"

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicflowservice,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowservices,verbs=create;update,versions=v1,name=mlogicflowservice-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowServiceDefaulter struct{}

var _ admission.Defaulter[*LogicFlowService] = &LogicFlowServiceDefaulter{}

func (d *LogicFlowServiceDefaulter) Default(_ context.Context, obj *LogicFlowService) error {
	ApplyLogicFlowServiceDefaults(obj)
	return nil
}

func ApplyLogicFlowServiceDefaults(obj *LogicFlowService) {
	if obj.Spec.Ingress.GatewayRef == nil && obj.Spec.Ingress.IngressClassName == nil {
		obj.Spec.Ingress.IngressClassName = new(NginxIngress)
	}
}

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowservice,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowservices,verbs=create;update,versions=v1,name=vlogicflowservice-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicFlowServiceValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*LogicFlowService] = &LogicFlowServiceValidator{}

func (v *LogicFlowServiceValidator) ValidateCreate(ctx context.Context, obj *LogicFlowService) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *LogicFlowServiceValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *LogicFlowService) (admission.Warnings, error) {
	if errs := v.validateGatewayRefImmutable(oldObj, newObj); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, v.validate(ctx, newObj)
}

func (v *LogicFlowServiceValidator) ValidateDelete(_ context.Context, _ *LogicFlowService) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowServiceValidator) validate(ctx context.Context, obj *LogicFlowService) error {
	allErrs := v.validateTrafficConfig(obj)
	allErrs = append(allErrs, v.validateIngress(obj)...)
	allErrs = append(allErrs, v.validateTLS(obj)...)
	if len(allErrs) > 0 {
		return allErrs.ToAggregate()
	}
	return v.validateDefinitions(ctx, obj).ToAggregate()
}

func (v *LogicFlowServiceValidator) validateTrafficConfig(obj *LogicFlowService) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")
	hasTraffic := len(obj.Spec.Traffic) > 0
	hasDefault := obj.Spec.DefaultDefinition != nil && obj.Spec.DefaultDefinition.Name != ""

	if hasTraffic && hasDefault {
		return append(allErrs, field.Forbidden(specPath.Child("defaultDefinition"), "may not be specified together with spec.traffic, only one of these fields must be specified"))
	}
	if !hasTraffic && !hasDefault {
		return append(allErrs, field.Required(specPath, "one of traffic or defaultDefinition must be specified"))
	}
	if hasDefault && len(validation.IsDNS1123Label(obj.Spec.DefaultDefinition.Name)) > 0 {
		return append(allErrs, field.Invalid(specPath.Child("defaultDefinition"), obj.Spec.DefaultDefinition.Name, "does not conform to a valid logicflowdefinition name"))
	}
	if hasTraffic {
		trafficFieldPath := specPath.Child("traffic")
		if utils.IsOpenShift() && obj.Spec.Ingress.GatewayRef == nil {
			return append(allErrs, field.Forbidden(trafficFieldPath, "OpenShift clusters only support traffic splitting when you specify a spec.ingress.gatewayRef"))
		}
		for i, t := range obj.Spec.Traffic {
			trafficItemPath := trafficFieldPath.Index(i)
			if len(validation.IsDNS1123Label(t.DefinitionRef.Name)) > 0 {
				allErrs = append(allErrs, field.Invalid(trafficItemPath.Child("definitionRef", "name"), t.DefinitionRef.Name, "does not conform to a valid logicflowdefinition name"))
			}
			if t.Weight < 0 || t.Weight > 100 {
				allErrs = append(allErrs, field.Invalid(trafficItemPath.Child("weight"), t.Weight, "must be between 0 and 100"))
			}
		}
		if obj.Spec.Ingress.GatewayRef == nil && len(obj.Spec.Traffic) > 2 && isNginxIngressClassName(&obj.Spec.Ingress) {
			allErrs = append(allErrs, field.Forbidden(trafficFieldPath, fmt.Sprintf("%d paths were found; nginx traffic splitting only supports two paths", len(obj.Spec.Traffic))))
		}
		if total := obj.Spec.TotalWeight(); total != 100 {
			allErrs = append(allErrs, field.Invalid(specPath.Child("traffic"), total, fmt.Sprintf("weights must sum to 100, got %d", total)))
		}
	}
	return allErrs
}

func isNginxIngressClassName(ingress *IngressSpec) bool {
	return ingress.IngressClassName != nil && *ingress.IngressClassName == NginxIngress
}

func (v *LogicFlowServiceValidator) validateIngress(obj *LogicFlowService) field.ErrorList {
	allErrs := field.ErrorList{}
	fieldPath := field.NewPath("spec", "ingress")

	if obj.Spec.Ingress.GatewayRef == nil {
		if !utils.IsOpenShift() && obj.Spec.Ingress.Host == "" {
			allErrs = append(allErrs, field.Required(fieldPath.Child("host"), "must be specified for nginx ingress mode"))
		}
		if obj.Spec.Ingress.IngressClassName != nil && !isNginxIngressClassName(&obj.Spec.Ingress) {
			allErrs = append(allErrs, field.Invalid(fieldPath.Child("ingressClassName"), *obj.Spec.Ingress.IngressClassName, "only supports \"nginx\""))
		}
	} else {
		if !utils.IsGatewayAPIAvailable() {
			allErrs = append(allErrs, field.Forbidden(fieldPath.Child("gatewayRef"), "GatewayAPI is not available in the cluster"))
		}
		if len(validation.IsDNS1123Label(obj.Spec.Ingress.GatewayRef.Name)) > 0 {
			allErrs = append(allErrs, field.Invalid(fieldPath.Child("gatewayRef", "name"), obj.Spec.Ingress.GatewayRef.Name, "does not conform to a valid kubernetes resource name"))
		}
		if obj.Spec.Ingress.GatewayRef.Namespace != nil && len(validation.IsDNS1123Label(*obj.Spec.Ingress.GatewayRef.Namespace)) > 0 {
			allErrs = append(allErrs, field.Invalid(fieldPath.Child("gatewayRef", "namespace"), obj.Spec.Ingress.GatewayRef.Namespace, "does not conform to a valid namespace name"))
		}
	}
	return allErrs
}

func (v *LogicFlowServiceValidator) validateTLS(obj *LogicFlowService) field.ErrorList {
	allErrs := field.ErrorList{}
	fieldPath := field.NewPath("spec", "ingress", "tls")
	tls := obj.Spec.Ingress.TLS
	if !tls.Enabled {
		return allErrs
	}
	hasSecret := tls.SecretRef.Name != ""
	hasCM := tls.CertManager != nil
	if !hasSecret && !hasCM {
		return append(allErrs, field.Required(fieldPath, "one of secretRef or certManager must be specified when TLS is enabled"))
	}
	if hasSecret && hasCM {
		return append(allErrs, field.Forbidden(fieldPath.Child("secretRef"), "may not be specified together with spec.ingress.tls.certManager, only one of these fields must be specified when TLS is enabled"))
	}
	if hasSecret && len(validation.IsDNS1123Subdomain(tls.SecretRef.Name)) > 0 {
		return append(allErrs, field.Invalid(fieldPath.Child("secretRef", "name"), tls.SecretRef.Name, "does not conform to a valid secret name"))
	}
	if hasCM && len(validation.IsDNS1123Label(tls.CertManager.IssuerRef.Name)) > 0 {
		return append(allErrs, field.Invalid(fieldPath.Child("certManager", "issuerRef", "name"), tls.CertManager.IssuerRef.Name, "does not conform to a valid kubernetes resource name"))
	}
	return allErrs
}

func (v *LogicFlowServiceValidator) validateDefinitions(ctx context.Context, obj *LogicFlowService) field.ErrorList {
	allErrs := field.ErrorList{}
	refs := v.resolveDefinitionRefs(obj)
	if len(refs) == 0 {
		return allErrs
	}

	specPath := field.NewPath("spec")
	defaultDefPath := specPath.Child("defaultDefinition")
	trafficPath := specPath.Child("traffic")
	var runtimeName string
	var workflowName string

	for i, ref := range refs {
		var def LogicFlowDefinition
		if err := v.Reader.Get(ctx, client.ObjectKey{Name: ref, Namespace: obj.Namespace}, &def); err != nil {
			if len(refs) == 1 {
				return append(allErrs, field.InternalError(defaultDefPath, fmt.Errorf("LogicFlowDefinition: %s not found: %w", ref, err)))
			}
			return append(allErrs, field.InternalError(trafficPath.Index(i).Child("definitionRef"), fmt.Errorf("LogicFlowDefinition: %s not found: %w", ref, err)))
		}
		if def.Status.FlowServiceRef != nil && def.Status.FlowServiceRef.Name != obj.Name {
			if len(refs) == 1 {
				return append(allErrs, field.Forbidden(defaultDefPath, fmt.Sprintf("definition %q is already exposed by the LogicFlowService %q", ref, def.Status.FlowServiceRef.Name)))
			}
			return append(allErrs, field.Forbidden(trafficPath.Index(i).Child("definitionRef"), fmt.Sprintf("definition %q is already exposed by the LogicFlowService %q", ref, def.Status.FlowServiceRef.Name)))
		}
		if i == 0 {
			runtimeName = def.Spec.RuntimeRef.Name
		} else if def.Spec.RuntimeRef.Name != runtimeName {
			return append(allErrs, field.Forbidden(trafficPath.Index(i).Child("definitionRef"), fmt.Sprintf(
				"definition %q targets runtime %q, expected %q; all definitions must target the same runtime", ref, def.Spec.RuntimeRef.Name, runtimeName)))
		}

		wfName := v.resolveWorkflowName(&def)
		if wfName != "" {
			if workflowName == "" {
				workflowName = wfName
			} else if wfName != workflowName {
				return append(allErrs, field.Forbidden(specPath.Child("traffic").Index(i).Child("definitionRef"), fmt.Sprintf(
					"definition %q has workflow name %q, expected %q; all definitions must be versions of the same workflow",
					ref, wfName, workflowName)))
			}
		}
	}
	return allErrs
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

func (v *LogicFlowServiceValidator) validateGatewayRefImmutable(oldObj, newObj *LogicFlowService) field.ErrorList {
	allErrs := field.ErrorList{}
	hadGatewayRef := oldObj.Spec.Ingress.GatewayRef != nil
	hasGatewayRef := newObj.Spec.Ingress.GatewayRef != nil
	if hadGatewayRef != hasGatewayRef {
		newName := "nil"
		if newObj.Spec.Ingress.GatewayRef != nil {
			newName = newObj.Spec.Ingress.GatewayRef.Name
		}
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "ingress", "gatewayRef"), newName, "cannot be added or removed after creation; create a new LogicFlowService to change networking mode"))
	}
	return allErrs
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
