package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	GatewayContainerName       = "workflow-gateway"
	GatewayComponentName       = "workflow-gateway"
	ClusterRoleWorkflowGateway = "logic-operator-workflow-gateway"
	FinalizerWorkflowGateway   = "logicplatform.logic.kubesmarts.org/workflow-gateway-cleanup"
)

// WorkflowGatewayEnvVars returns environment variable configuration for the Workflow Gateway.
func WorkflowGatewayEnvVars() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv(envFieldRef("POD_NAMESPACE", "metadata.namespace"))
	}
}

// WorkflowGatewayPorts returns port configuration for the Workflow Gateway.
func WorkflowGatewayPorts() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithPorts(corev1ac.ContainerPort().
			WithContainerPort(8080).
			WithName("http").
			WithProtocol(corev1.ProtocolTCP),
			corev1ac.ContainerPort().
				WithContainerPort(logicv1.WorkflowGatewayManagementPort).
				WithName("management").
				WithProtocol(corev1.ProtocolTCP))
	}
}

// WorkflowGatewayProbes returns probe configuration for the Workflow Gateway.
func WorkflowGatewayProbes() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/ready").
				WithPort(intstr.FromString("management"))).
			WithInitialDelaySeconds(10).
			WithPeriodSeconds(5))
		c.WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromString("management"))).
			WithInitialDelaySeconds(30).
			WithPeriodSeconds(10))
	}
}
