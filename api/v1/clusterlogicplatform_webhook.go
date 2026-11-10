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
	if obj.Spec.Vector != nil && obj.Spec.DataIndex == nil {
		return fmt.Errorf("spec.dataIndex is required when Vector is enabled")
	}
	if obj.Spec.DataIndex != nil &&
		(obj.Spec.DataIndex.Persistence == nil || obj.Spec.DataIndex.Persistence.PostgreSQL == nil) {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql is required when Data Index is enabled")
	}

	// ClusterLogicPlatform must be singleton (only one per cluster).
	if err := v.validateSingleton(ctx, obj); err != nil {
		return err
	}

	return nil
}

func (v *ClusterLogicPlatformValidator) validateSingleton(ctx context.Context, obj *ClusterLogicPlatform) error {
	var platforms ClusterLogicPlatformList
	if err := v.Reader.List(ctx, &platforms); err != nil {
		return err
	}

	for _, plat := range platforms.Items {
		if plat.Name != obj.Name {
			return fmt.Errorf("only one ClusterLogicPlatform per cluster is allowed; %s already exists", plat.Name)
		}
	}
	return nil
}
