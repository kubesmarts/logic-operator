package controller

import (
	"github.com/kubesmarts/logic-operator/api/version"
)

const (
	WorkflowMountPath = "/deployments/workflows"

	LeaseMemberNameFmt = "flow-pool-member-%s-%02d"
	LeaseDuration      = int32(30)
	// LeaseRenewSchedulingInterval is ISO-8601 format
	LeaseRenewSchedulingInterval = "10s"
	// LeaseAcquireTimeoutDuration is Java duration
	LeaseAcquireTimeoutDuration = "30S"

	LabelDurablePool     = "io.quarkiverse.flow.durable.k8s/pool"
	LabelDurableIsLeader = "io.quarkiverse.flow.durable.k8s/is-leader"

	DurableComponentValue = "durable"
	DurableManagedByValue = "quarkus-flow"

	ClusterRoleDurable = "logic-operator-logic-flow-runtime-durable"
)

// QuarkusFlowVersion is the runner image version selected for this operator
// build. It is kept as a package variable for callers that compose image tags.
var QuarkusFlowVersion = version.GetQuarkusFlowRunnerVersion()
