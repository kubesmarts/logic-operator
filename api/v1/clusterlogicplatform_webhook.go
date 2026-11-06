package v1

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-clusterlogicplatform,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=clusterlogicplatforms,verbs=create;update,versions=v1,name=vclusterlogicplatform-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type ClusterLogicPlatformValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*ClusterLogicPlatform] = &ClusterLogicPlatformValidator{}

func (v *ClusterLogicPlatformValidator) ValidateCreate(ctx context.Context, obj *ClusterLogicPlatform) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *ClusterLogicPlatformValidator) ValidateUpdate(ctx context.Context, _ /* oldObj */, newObj *ClusterLogicPlatform) (admission.Warnings, error) {
	return nil, v.validate(ctx, newObj)
}

func (v *ClusterLogicPlatformValidator) ValidateDelete(_ context.Context, _ *ClusterLogicPlatform) (admission.Warnings, error) {
	return nil, nil
}

func (v *ClusterLogicPlatformValidator) validate(ctx context.Context, obj *ClusterLogicPlatform) error {
	// Validate targetNamespace is not empty
	if obj.Spec.TargetNamespace == "" {
		return fmt.Errorf("spec.targetNamespace is required")
	}

	// ClusterLogicPlatform must be singleton (only one per cluster)
	if err := v.validateSingleton(ctx, obj); err != nil {
		return err
	}

	return nil
}

func (v *ClusterLogicPlatformValidator) validateSingleton(ctx context.Context, obj *ClusterLogicPlatform) error {
	// Check if another ClusterLogicPlatform already exists
	var platforms ClusterLogicPlatformList
	err := v.Reader.List(ctx, &platforms)
	if err != nil {
		return err
	}

	// Count existing platforms (excluding self during updates)
	for _, plat := range platforms.Items {
		if plat.Name != obj.Name {
			return fmt.Errorf("only one ClusterLogicPlatform per cluster is allowed; %s already exists", plat.Name)
		}
	}
	return nil
}
