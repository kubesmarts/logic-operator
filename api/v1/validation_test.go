package v1

import (
	"fmt"
	"testing"
)

func TestValidateRunnerImage(t *testing.T) {
	version := "0.15.1"
	minimalImage := fmt.Sprintf("%s/%s:%s-%s", FlowRunnerRegistry, FlowRunnerImage, version, ImageVariantMinimal)
	standardImage := fmt.Sprintf("%s/%s:%s-%s", FlowRunnerRegistry, FlowRunnerImage, version, ImageVariantStandard)
	persistence := &PersistenceOptionsSpec{PostgreSQL: &PersistencePostgreSQL{}}

	t.Run("minimal with persistence errors", func(t *testing.T) {
		err := ValidateRunnerImage(minimalImage, persistence)
		if err == nil {
			t.Fatal("expected error for minimal image with persistence")
		}
	})

	t.Run("standard without persistence errors", func(t *testing.T) {
		err := ValidateRunnerImage(standardImage, nil)
		if err == nil {
			t.Fatal("expected error for standard image without persistence")
		}
	})

	t.Run("minimal without persistence is valid", func(t *testing.T) {
		if err := ValidateRunnerImage(minimalImage, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("standard with persistence is valid", func(t *testing.T) {
		if err := ValidateRunnerImage(standardImage, persistence); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("custom image skips validation", func(t *testing.T) {
		if err := ValidateRunnerImage("my-registry/custom-runner:1.0", persistence); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := ValidateRunnerImage("my-registry/custom-runner:1.0", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
