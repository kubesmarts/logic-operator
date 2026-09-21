package v1

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicplatform,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicplatforms,verbs=create;update,versions=v1,name=mlogicplatform-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicPlatformDefaulter struct{}

var _ admission.Defaulter[*LogicPlatform] = &LogicPlatformDefaulter{}

func (d *LogicPlatformDefaulter) Default(_ context.Context, plat *LogicPlatform) error {

	// Note: DataIndex.Enabled defaults to true via +kubebuilder:default=true annotation
	// The API server applies this default, so we don't need to handle it in the webhook

	if plat.Spec.DataIndex.Enabled {
		// Apply image default if Data Index is enabled and image is not set
		if plat.Spec.DataIndex.Application.Image == "" {
			plat.Spec.DataIndex.Application.Image = DefaultDataIndexImage()
		}

		// Apply replicas default if not set
		if plat.Spec.DataIndex.Application.Replicas == nil {
			defaultReplicas := int32(1)
			plat.Spec.DataIndex.Application.Replicas = &defaultReplicas
		}

		// Apply resource defaults if not set (based on helm values)
		if plat.Spec.DataIndex.Application.Resources.Requests == nil &&
			plat.Spec.DataIndex.Application.Resources.Limits == nil {
			plat.Spec.DataIndex.Application.Resources = DefaultDataIndexResources()
		}
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicplatform,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicplatforms,verbs=create;update,versions=v1,name=vlogicplatform-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicPlatformValidator struct {
	Reader client.Reader
}

var _ admission.Validator[*LogicPlatform] = &LogicPlatformValidator{}

func (v *LogicPlatformValidator) ValidateCreate(ctx context.Context, obj *LogicPlatform) (admission.Warnings, error) {
	return nil, v.validate(ctx, obj)
}

func (v *LogicPlatformValidator) ValidateUpdate(ctx context.Context, _ /* oldObj */, newObj *LogicPlatform) (admission.Warnings, error) {
	return nil, v.validate(ctx, newObj)
}

func (v *LogicPlatformValidator) ValidateDelete(_ context.Context, _ *LogicPlatform) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicPlatformValidator) validate(_ context.Context, obj *LogicPlatform) error {
	if !obj.Spec.DataIndex.Enabled {
		// If Data Index is disabled, no validation needed
		return nil
	}

	// Validate PostgreSQL persistence configuration
	if err := v.validatePostgreSQLPersistence(obj.Spec.DataIndex.Persistence); err != nil {
		return err
	}

	// Validate image format if provided
	if obj.Spec.DataIndex.Application.Image != "" {
		if err := ValidateImageFormat(obj.Spec.DataIndex.Application.Image); err != nil {
			return err
		}
	}

	// Validate ingress configuration if provided
	if err := v.validateIngress(obj.Spec.DataIndex.Ingress); err != nil {
		return err
	}

	return nil
}

func (v *LogicPlatformValidator) validateIngress(ingress *DataIndexIngressSpec) error {
	if ingress == nil {
		return nil
	}

	// If ingress is enabled, host is required
	if ingress.Enabled && ingress.Host == "" {
		return fmt.Errorf("spec.dataIndex.ingress.host is required when ingress is enabled")
	}

	// Validate TLS configuration
	if ingress.TLS.Enabled {
		hasSecret := ingress.TLS.SecretRef.Name != ""
		hasCertManager := ingress.TLS.CertManager != nil

		if hasSecret && hasCertManager {
			return fmt.Errorf("spec.dataIndex.ingress.tls.secretRef and spec.dataIndex.ingress.tls.certManager are mutually exclusive")
		}

		if hasCertManager && ingress.TLS.CertManager.IssuerRef.Name == "" {
			return fmt.Errorf("spec.dataIndex.ingress.tls.certManager.issuerRef.name is required")
		}
	}

	return nil
}

func (v *LogicPlatformValidator) validatePostgreSQLPersistence(persistence *PersistenceOptionsSpec) error {
	if persistence == nil || persistence.PostgreSQL == nil {
		return fmt.Errorf("spec.dataIndex.persistence is required when Data Index is enabled")
	}

	pg := persistence.PostgreSQL

	// Validate secret ref
	if pg.SecretRef.Name == "" {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.secretRef.name is required")
	}

	// Ensure exactly one of serviceRef or jdbcURL is provided
	hasServiceRef := pg.ServiceRef != nil
	hasJdbcURL := pg.JdbcURL != ""

	if !hasServiceRef && !hasJdbcURL {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql must have either serviceRef or jdbcURL")
	}

	if hasServiceRef && hasJdbcURL {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef and jdbcURL are mutually exclusive")
	}

	// Validate service ref fields if provided
	if hasServiceRef {
		if pg.ServiceRef.Name == "" {
			return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef.name is required")
		}
		if pg.ServiceRef.DatabaseSchema == "" {
			return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef.databaseSchema is required")
		}
	}

	return nil
}
