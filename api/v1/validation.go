package v1

import (
	"fmt"
	"regexp"
)

const (
	DefaultPgsqlSecretUserKey     = "POSTGRESQL_USER"
	DefaultPgsqlSecretPasswordKey = "POSTGRESQL_PASSWORD"
)

var imageFormatRegex = regexp.MustCompile(`^[a-z0-9.-]+(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`)

// ValidateImageFormat validates that an image string follows the expected format
func ValidateImageFormat(image string) error {
	if !imageFormatRegex.MatchString(image) {
		return fmt.Errorf("invalid image format: %q", image)
	}
	return nil
}
