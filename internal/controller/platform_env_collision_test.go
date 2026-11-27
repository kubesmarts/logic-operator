package controller

import (
	"testing"

	"github.com/onsi/gomega"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

// Platform component options must replace a same-named variable set through the component's ApplicationSpec.
func TestPlatformComponentOptions_NeverDuplicateExistingEnv(t *testing.T) {
	g := gomega.NewWithT(t)
	plat := &logicv1.ClusterLogicPlatform{}
	plat.Spec.TargetNamespace = "workflows"
	opts := []ContainerOption{
		WithFlywayPersistenceVars(),
		WithGraphQLVars(),
		WithVectorEnvVarsCLP(plat),
		WorkflowGatewayEnvVars(),
	}

	probe := corev1ac.Container().WithName("test")
	for _, opt := range opts {
		opt(probe)
	}
	g.Expect(probe.Env).NotTo(gomega.BeEmpty())

	c := corev1ac.Container().WithName("test")
	for _, e := range probe.Env {
		c.WithEnv(corev1ac.EnvVar().WithName(*e.Name).WithValue("user"))
	}
	for _, opt := range opts {
		opt(c)
	}

	g.Expect(c.Env).To(gomega.HaveLen(len(probe.Env)))
	for _, e := range probe.Env {
		count, _ := envCount(c, *e.Name)
		g.Expect(count).To(gomega.Equal(1), "env var %s duplicated", *e.Name)
	}
}
