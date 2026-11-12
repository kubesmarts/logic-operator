package v1

import (
	"fmt"

	"github.com/kubesmarts/logic-operator/api/version"
)

// DefaultDataIndexImage returns the default Data Index service image
func DefaultDataIndexImage() string {
	return fmt.Sprintf("quay.io/kubesmarts/data-index-service:%s", version.GetDataIndexVersion())
}

// DefaultVectorImage returns the default Vector service image
func DefaultVectorImage() string {
	return fmt.Sprintf("timberio/vector:%s", version.VectorVersion)
}

// DefaultGatewayImage returns the workflow-gateway image version bundled with
// this operator release.
func DefaultGatewayImage() string {
	return fmt.Sprintf("quay.io/kubesmarts/workflow-gateway:%s", version.GetWorkflowGatewayVersion())
}

// DefaultDBMigratorImage returns the default DBMigrator image bundled with
// this operator release.
// TODO: add the default DBMigrator version to version.go file
func DefaultDBMigratorImage() string {
	return fmt.Sprintf("quay.io/kubesmarts/logic-db-migrator:%s", version.GetOperatorVersion())
}
