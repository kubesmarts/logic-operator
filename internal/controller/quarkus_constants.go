package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

const (
	QuarkusFlowRegistry = logicv1.FlowRunnerRegistry
	QuarkusFlowRunner   = logicv1.FlowRunnerImage
	QuarkusFlowVersion  = "1.0.0"

	ImageVariantMinimal  = logicv1.ImageVariantMinimal
	ImageVariantStandard = logicv1.ImageVariantStandard

	QuarkusPort = int32(8080)

	WorkflowMountPath = "/deployments/workflows"

	LeaseMemberNameFmt = "flow-pool-member-%s-%02d"
	LeaseDuration      = int32(30)

	LabelDurablePool     = "io.quarkiverse.flow.durable.k8s/pool"
	LabelDurableIsLeader = "io.quarkiverse.flow.durable.k8s/is-leader"

	DurableComponentValue = "durable"
	DurableManagedByValue = "quarkus-flow"

	ClusterRoleDurable = "logic-operator-logic-flow-runtime-durable"
)
