package v1

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
		obj.Spec.Replicas = new(int32(1))
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
	runnerErrs := validateRunnerImage(obj)
	securityErrs := validateSecuritySpec(obj)
	platformErrs := validatePlatformRef(obj)
	allErrs := make(field.ErrorList, 0, len(runnerErrs)+len(securityErrs)+len(platformErrs))
	allErrs = append(allErrs, runnerErrs...)
	allErrs = append(allErrs, securityErrs...)
	allErrs = append(allErrs, platformErrs...)
	return nil, allErrs.ToAggregate()
}

func HasPersistence(p *DatastoreSpec) bool {
	return p != nil && p.PostgreSQL != nil
}

func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, FlowRunnerRegistry+"/"+FlowRunnerImage+":")
}

func validateRunnerImage(obj *LogicFlowRuntime) field.ErrorList {
	allErrs := field.ErrorList{}
	if obj.Spec.Image != "" {
		if !isKnownRunnerImage(obj.Spec.Image) {
			return allErrs
		}
		fieldPath := field.NewPath("spec", "image")
		if strings.HasSuffix(obj.Spec.Image, "-"+ImageVariantMinimal) && !IsEphemeral(obj) {
			return field.ErrorList{field.Invalid(fieldPath, obj.Spec.Image, "configured image is the minimal variant and does not support persistence; set spec.persistenceMode=Ephemeral or use the standard image")}
		}
		if strings.HasSuffix(obj.Spec.Image, "-"+ImageVariantStandard) && IsEphemeral(obj) {
			return field.ErrorList{field.Invalid(fieldPath, obj.Spec.Image, "configured image is the standard variant and requires persistence; set spec.persistenceMode=PlatformDB or use the minimal image")}
		}
	}
	return allErrs
}

func validateSecuritySpec(flow *LogicFlowRuntime) field.ErrorList {
	allErrs := field.ErrorList{}
	sec := flow.Spec.Security
	fieldPath := field.NewPath("spec", "security")
	if sec.Type != RuntimeSecurityAPIKey && sec.APIKey != nil {
		allErrs = append(allErrs, field.Forbidden(fieldPath.Child("apiKey"), fmt.Sprintf("must not be set when security type is %s", securityTypeOrNone(sec.Type))))
	}
	if sec.Type != RuntimeSecurityOIDC && sec.OIDC != nil {
		allErrs = append(allErrs, field.Forbidden(fieldPath.Child("oidc"), fmt.Sprintf("must not be set when security type is %s", securityTypeOrNone(sec.Type))))
	}
	switch sec.Type {
	case RuntimeSecurityAPIKey:
		if sec.APIKey == nil || len(sec.APIKey.Keys) == 0 {
			allErrs = append(allErrs, field.Required(fieldPath.Child("apiKey", "keys"), "must be specified when security type is API_KEY"))
		}
	case RuntimeSecurityOIDC:
		if sec.OIDC == nil {
			allErrs = append(allErrs, field.Required(fieldPath.Child("oidc"), "must be specified when security type is OIDC"))
		} else {
			if sec.OIDC.AuthServerURL == "" {
				allErrs = append(allErrs, field.Required(fieldPath.Child("oidc", "authServerUrl"), "must be specified when security type is OIDC"))
			}
			if sec.OIDC.ClientID == "" {
				allErrs = append(allErrs, field.Required(fieldPath.Child("oidc", "clientId"), "must be specified when security type is OIDC"))
			}
			if sec.OIDC.ClientSecret.Name == "" {
				allErrs = append(allErrs, field.Required(fieldPath.Child("oidc", "clientSecret", "name"), "must be specified when security type is OIDC"))
			} else if len(validation.IsDNS1123Subdomain(sec.OIDC.ClientSecret.Name)) > 0 {
				allErrs = append(allErrs, field.Invalid(fieldPath.Child("oidc", "clientSecret", "name"), sec.OIDC.ClientSecret.Name, "does not conform to a valid secret name"))
			}
		}
	}
	return allErrs
}

func securityTypeOrNone(t RuntimeSecurityType) RuntimeSecurityType {
	if t == "" {
		return RuntimeSecurityNone
	}
	return t
}

func validatePlatformRef(obj *LogicFlowRuntime) field.ErrorList {
	allErrs := field.ErrorList{}
	// If platformRef is omitted, auto-discovery is used (allowed)
	if obj.Spec.PlatformRef == nil {
		return allErrs
	}

	// Name must be non-empty when platformRef is present
	if obj.Spec.PlatformRef.Name == "" {
		return append(allErrs, field.Required(field.NewPath("spec", "platformRef", "name"), "is required when platformRef is set"))
	}

	return allErrs
}
