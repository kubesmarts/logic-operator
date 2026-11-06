package controller

import (
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
	return func(_ *corev1ac.ContainerApplyConfiguration) {
		// Gateway-specific environment variables can be added here
	}
}

// WorkflowGatewayPorts returns port configuration for the Workflow Gateway.
func WorkflowGatewayPorts() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithPorts(corev1ac.ContainerPort().
			WithContainerPort(8080).
			WithName("http").
			WithProtocol(corev1.ProtocolTCP))
	}
}

// WorkflowGatewayProbes returns probe configuration for the Workflow Gateway.
func WorkflowGatewayProbes() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromInt(8080))).
			WithInitialDelaySeconds(10).
			WithPeriodSeconds(5))
		c.WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().
				WithPath("/health").
				WithPort(intstr.FromInt(8080))).
			WithInitialDelaySeconds(30).
			WithPeriodSeconds(10))
	}
}
