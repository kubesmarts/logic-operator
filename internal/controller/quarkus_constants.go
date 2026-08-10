package controller

const (
	QuarkusFlowRegistry = "quay.io/quarkiverse"
	QuarkusFlowRunner   = "quarkus-flow-runner"
	QuarkusFlowVersion  = "0.15.1"

	ImageVariantMinimal  = "minimal"
	ImageVariantStandard = "standard"

	QuarkusPort = int32(8080)

	WorkflowMountPath = "/deployments/workflows"

	LeaseMemberNameFmt = "flow-pool-member-%s-%02d"
	LeaseDuration      = int32(30)

	LabelDurablePool     = "io.quarkiverse.flow.durable.k8s/pool"
	LabelDurableIsLeader = "io.quarkiverse.flow.durable.k8s/is-leader"

	DurableComponentValue = "durable"
	DurableManagedByValue = "quarkus-flow"

	ClusterRoleDurable = "logic-flow-runtime-durable"
)
