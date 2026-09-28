package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-logic-kubesmarts-org-v1-logicplatform,mutating=true,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicplatforms,verbs=create;update,versions=v1,name=mlogicplatform-v1.kb.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false
type LogicPlatformDefaulter struct{}

var _ admission.Defaulter[*LogicPlatform] = &LogicPlatformDefaulter{}

const (
	DataIndexRegistry = "quay.io/kubesmarts"
	DataIndexImage    = "data-index-service"
	DataIndexVersion  = "2.0.0-SNAPSHOT"
	DataIndexVariant  = "postgresql"

	VectorRegistry       = "timberio"
	VectorImage          = "vector"
	VectorVersion        = "0.54.0-distroless-libc"
	VectorServiceAccount = "vector"
)

// DefaultDataIndexImage returns the default Data Index service image
func DefaultDataIndexImage() string {
	return fmt.Sprintf("%s/%s:%s-%s", DataIndexRegistry, DataIndexImage, DataIndexVersion, DataIndexVariant)
}

// DefaultVectorImage returns the default Vector service image
func DefaultVectorImage() string {
	return fmt.Sprintf("%s/%s:%s", VectorRegistry, VectorImage, VectorVersion)
}

// DefaultDataIndexResources returns default resource requirements for Data Index based on helm values.
// Resources align with logic-apps/data-index/helm/data-index/values.yaml:
//
//	requests: cpu: 250m, memory: 512Mi
//	limits: cpu: 1000m, memory: 1Gi
func DefaultDataIndexResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}
}

// DefaultVectorResources returns default resource requirements for Vector.
// Resources are based on Vector DaemonSet requirements:
//
//	requests: cpu: 100m, memory: 128Mi
//	limits: cpu: 500m, memory: 256Mi
func DefaultVectorResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

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

		if plat.Spec.DataIndex.Persistence != nil && plat.Spec.DataIndex.Persistence.PostgreSQL != nil {
			if plat.Spec.DataIndex.Persistence.PostgreSQL.SecretRef.UserKey == "" {
				plat.Spec.DataIndex.Persistence.PostgreSQL.SecretRef.UserKey = DefaultPgsqlSecretUserKey
			}
			if plat.Spec.DataIndex.Persistence.PostgreSQL.SecretRef.PasswordKey == "" {
				plat.Spec.DataIndex.Persistence.PostgreSQL.SecretRef.PasswordKey = DefaultPgsqlSecretPasswordKey
			}
		}
	}

	if plat.Spec.DataIndex.Vector != nil && plat.Spec.DataIndex.Vector.Enabled {
		if plat.Spec.DataIndex.Vector.Application.Image == "" {
			plat.Spec.DataIndex.Vector.Application.Image = DefaultVectorImage()
		}
		if plat.Spec.DataIndex.Vector.Application.Resources.Requests == nil &&
			plat.Spec.DataIndex.Vector.Application.Resources.Limits == nil {
			plat.Spec.DataIndex.Vector.Application.Resources = DefaultVectorResources()
		}
		if plat.Spec.DataIndex.Vector.WatchNamespaces == nil {
			plat.Spec.DataIndex.Vector.WatchNamespaces = []string{plat.Namespace}
		}
		// users can't change this since we always create the same SA wherever Vector is deployed.
		plat.Spec.DataIndex.Vector.Application.PodTemplate.ServiceAccountName = VectorServiceAccount
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

func (v *LogicPlatformValidator) validate(ctx context.Context, obj *LogicPlatform) error {
	// LogicPlatform must be singleton per namespace
	if err := v.validateSingleton(ctx, obj); err != nil {
		return err
	}

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

func (v *LogicPlatformValidator) validateSingleton(ctx context.Context, obj *LogicPlatform) error {
	// Check if another LogicPlatform already exists in this namespace
	var platforms LogicPlatformList
	err := v.Reader.List(ctx, &platforms, client.InNamespace(obj.Namespace))
	if err != nil {
		return err
	}

	// Count existing platforms (excluding self during updates)
	for _, plat := range platforms.Items {
		if plat.Name != obj.Name {
			return fmt.Errorf("only one LogicPlatform per namespace is allowed; %s already exists in %s", plat.Name, obj.Namespace)
		}
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
		// TODO: change this when we introduce support to ES
		return fmt.Errorf("spec.dataIndex.persistence is required when Data Index is enabled")
	}

	pg := persistence.PostgreSQL

	// Validate secret ref
	if pg.SecretRef.Name == "" {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.secretRef.name is required")
	}

	// Ensure serviceRef is provided
	if pg.ServiceRef == nil {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef is required")
	}

	// Validate service ref fields
	if pg.ServiceRef.Name == "" {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef.name is required")
	}
	if pg.ServiceRef.DatabaseSchema == "" {
		return fmt.Errorf("spec.dataIndex.persistence.postgresql.serviceRef.databaseSchema is required")
	}

	return nil
}
