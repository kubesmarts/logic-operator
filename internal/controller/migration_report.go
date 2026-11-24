package controller

import (
	"encoding/json"
	"fmt"
	"strings"
)

// migrationReport is the JSON db-migrator writes to the container's termination message.
type migrationReport struct {
	Streams []migrationStreamReport `json:"streams,omitempty"`
	Error   string                  `json:"error,omitempty"`
}

type migrationStreamReport struct {
	Name           string `json:"name"`
	AppliedVersion string `json:"appliedVersion,omitempty"`
	AppliedCount   int32  `json:"appliedCount,omitempty"`
}

// parseMigrationReport parses the termination message. An empty message yields a zero report,
// since the container may exit before writing one (e.g. OOMKilled).
func parseMigrationReport(message string) (migrationReport, error) {
	var report migrationReport
	if strings.TrimSpace(message) == "" {
		return report, nil
	}
	if err := json.Unmarshal([]byte(message), &report); err != nil {
		return migrationReport{}, fmt.Errorf("invalid migration report: %w", err)
	}
	return report, nil
}
