package v1

import (
	"fmt"
	"strings"
)

const (
	FlowRunnerRegistry   = "quay.io/quarkiverse"
	FlowRunnerImage      = "quarkus-flow-runner"
	ImageVariantMinimal  = "minimal"
	ImageVariantStandard = "standard"
)

func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, FlowRunnerRegistry+"/"+FlowRunnerImage+":")
}

func hasPersistence(p *PersistenceOptionsSpec) bool {
	return p != nil && p.PostgreSQL != nil
}

func ValidateRunnerImage(image string, persistence *PersistenceOptionsSpec) error {
	if !isKnownRunnerImage(image) {
		return nil
	}

	if strings.HasSuffix(image, "-"+ImageVariantMinimal) && hasPersistence(persistence) {
		return fmt.Errorf("image %q does not support persistence; use the %s variant or remove persistence config", image, ImageVariantStandard)
	}
	if strings.HasSuffix(image, "-"+ImageVariantStandard) && !hasPersistence(persistence) {
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
		if sec.OIDC.AuthServerUrl == "" {
			return fmt.Errorf("spec.security.oidc.authServerUrl is required when security type is OIDC")
		}
		if sec.OIDC.ClientId == "" {
			return fmt.Errorf("spec.security.oidc.clientId is required when security type is OIDC")
		}
	}
	return nil
}
