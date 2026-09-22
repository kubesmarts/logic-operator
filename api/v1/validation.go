package v1

import (
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	FlowRunnerRegistry   = "quay.io/quarkiverse"
	FlowRunnerImage      = "quarkus-flow-runner"
	ImageVariantMinimal  = "minimal"
	ImageVariantStandard = "standard"

	DataIndexRegistry = "quay.io/kubesmarts"
	DataIndexImage    = "data-index-service"
	DataIndexVersion  = "2.0.0-SNAPSHOT"
	DataIndexVariant  = "postgresql"
)

func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, FlowRunnerRegistry+"/"+FlowRunnerImage+":")
}

func HasPersistence(p *PersistenceOptionsSpec) bool {
	return p != nil && p.PostgreSQL != nil
}

func ValidateRunnerImage(image string, persistence *PersistenceOptionsSpec) error {
	if !isKnownRunnerImage(image) {
		return nil
	}

	if strings.HasSuffix(image, "-"+ImageVariantMinimal) && HasPersistence(persistence) {
		return fmt.Errorf("image %q does not support persistence; use the %s variant or remove persistence config", image, ImageVariantStandard)
	}
	if strings.HasSuffix(image, "-"+ImageVariantStandard) && !HasPersistence(persistence) {
		return fmt.Errorf("image %q requires persistence configuration; set spec.persistence or use the %s variant", image, ImageVariantMinimal)
	}

	return nil
}

func ValidateSecuritySpec(sec RuntimeSecuritySpec) error {
	switch sec.Type {
	case RuntimeSecurityAPIKey:
		if sec.APIKey == nil || len(sec.APIKey.Keys) == 0 {
			return fmt.Errorf("spec.security.apiKey.keys is required when security type is API_KEY")
		}
	case RuntimeSecurityOIDC:
		if sec.OIDC == nil {
			return fmt.Errorf("spec.security.oidc is required when security type is OIDC")
		}
		if sec.OIDC.AuthServerURL == "" {
			return fmt.Errorf("spec.security.oidc.authServerUrl is required when security type is OIDC")
		}
		if sec.OIDC.ClientID == "" {
			return fmt.Errorf("spec.security.oidc.clientId is required when security type is OIDC")
		}
	}
	return nil
}

// DefaultDataIndexImage returns the default Data Index service image
func DefaultDataIndexImage() string {
	return fmt.Sprintf("%s/%s:%s-%s", DataIndexRegistry, DataIndexImage, DataIndexVersion, DataIndexVariant)
}

var imageFormatRegex = regexp.MustCompile(`^[a-z0-9.-]+(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`)

// ValidateImageFormat validates that an image string follows the expected format
func ValidateImageFormat(image string) error {
	if !imageFormatRegex.MatchString(image) {
		return fmt.Errorf("invalid image format: %q", image)
	}
	return nil
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
