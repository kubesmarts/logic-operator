package controller

import (
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

func int32Ptr(i int32) *int32 { return &i }

func TestToDeploymentSpec_MinimalConfig(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Image: "quay.io/kubesmarts/quarkus-flow:2.0.0",
	}
	podLabels := map[string]string{"app.kubernetes.io/name": "my-runtime"}
	selLabels := SelectorLabels("my-runtime")

	spec := ToDeploymentSpec(ContainerNameRunner, app, podLabels, selLabels)

	g.Expect(spec.Selector.MatchLabels).To(gomega.Equal(selLabels))
	g.Expect(spec.Template.Spec.Containers).To(gomega.HaveLen(1))
	g.Expect(*spec.Template.Spec.Containers[0].Name).To(gomega.Equal(ContainerNameRunner))
	g.Expect(*spec.Template.Spec.Containers[0].Image).To(gomega.Equal("quay.io/kubesmarts/quarkus-flow:2.0.0"))
}

func TestToDeploymentSpec_ThreeTierImagePrecedence(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	tests := []struct {
		name     string
		app      logicv1.ApplicationSpec
		expected string
	}{
		{
			name:     "top-level only",
			app:      logicv1.ApplicationSpec{Image: "top:1.0"},
			expected: "top:1.0",
		},
		{
			name: "container overrides top-level",
			app: logicv1.ApplicationSpec{
				Image:     "top:1.0",
				Container: logicv1.ContainerSpec{Image: "container:2.0"},
			},
			expected: "container:2.0",
		},
		{
			name: "podTemplate.container overrides all",
			app: logicv1.ApplicationSpec{
				Image:     "top:1.0",
				Container: logicv1.ContainerSpec{Image: "container:2.0"},
				PodTemplate: logicv1.PodTemplateSpec{
					Container: logicv1.ContainerSpec{Image: "podtemplate:3.0"},
				},
			},
			expected: "podtemplate:3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g = gomega.NewWithT(t)
			spec := ToDeploymentSpec(ContainerNameRunner, &tt.app, sel, sel)
			g.Expect(*spec.Template.Spec.Containers[0].Image).To(gomega.Equal(tt.expected))
		})
	}
}

func TestToDeploymentSpec_ResourcesPrecedence(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image: "test:1.0",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
		PodTemplate: logicv1.PodTemplateSpec{
			Container: logicv1.ContainerSpec{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
			},
		},
	}

	spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
	g.Expect(spec.Template.Spec.Containers[0].Resources.Requests.Memory().String()).To(gomega.Equal("512Mi"))
}

func TestToDeploymentSpec_Replicas(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	t.Run("top-level", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: "test:1.0", Replicas: int32Ptr(3)}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(*spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("podTemplate overrides top-level", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image:    "test:1.0",
			Replicas: int32Ptr(2),
			PodTemplate: logicv1.PodTemplateSpec{
				Replicas: int32Ptr(5),
			},
		}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(*spec.Replicas).To(gomega.Equal(int32(5)))
	})

	t.Run("nil when unset", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: "test:1.0"}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(spec.Replicas).To(gomega.BeNil())
	})
}

func TestToDeploymentSpec_PodLabels(t *testing.T) {
	g := gomega.NewWithT(t)
	podLabels := ChildLabels(&logicv1.LogicFlowRuntime{})
	sel := SelectorLabels("test")

	t.Run("operator labels propagated", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: "test:1.0"}
		spec := ToDeploymentSpec(ContainerNameRunner, app, podLabels, sel)
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
	})

	t.Run("user labels merged from podTemplate.metadata", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image: "test:1.0",
			PodTemplate: logicv1.PodTemplateSpec{
				Metadata: &logicv1.PodTemplateMetadata{
					Labels: map[string]string{"team": "platform", "env": "prod"},
				},
			},
		}
		spec := ToDeploymentSpec(ContainerNameRunner, app, podLabels, sel)
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("team", "platform"))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("env", "prod"))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
	})
}

func TestToDeploymentSpec_Annotations(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image: "test:1.0",
		PodTemplate: logicv1.PodTemplateSpec{
			Metadata: &logicv1.PodTemplateMetadata{
				Annotations: map[string]string{"prometheus.io/scrape": "true"},
			},
		},
	}

	spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
	g.Expect(spec.Template.Annotations).To(gomega.HaveKeyWithValue("prometheus.io/scrape", "true"))
}

func TestToDeploymentSpec_Sidecars(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	t.Run("sidecar included", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image: "test:1.0",
			PodTemplate: logicv1.PodTemplateSpec{
				PodSpec: logicv1.PodSpec{
					Containers: []corev1.Container{
						{Name: "log-forwarder", Image: "fluent/fluent-bit:2.0"},
					},
				},
			},
		}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(spec.Template.Spec.Containers).To(gomega.HaveLen(2))
		g.Expect(*spec.Template.Spec.Containers[0].Name).To(gomega.Equal(ContainerNameRunner))
		g.Expect(*spec.Template.Spec.Containers[1].Name).To(gomega.Equal("log-forwarder"))
	})

	t.Run("duplicate main container name filtered", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image: "test:1.0",
			PodTemplate: logicv1.PodTemplateSpec{
				PodSpec: logicv1.PodSpec{
					Containers: []corev1.Container{
						{Name: ContainerNameRunner, Image: "should-be-filtered"},
						{Name: "valid-sidecar", Image: "sidecar:1.0"},
					},
				},
			},
		}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(spec.Template.Spec.Containers).To(gomega.HaveLen(2))
		g.Expect(*spec.Template.Spec.Containers[0].Image).To(gomega.Equal("test:1.0"))
		g.Expect(*spec.Template.Spec.Containers[1].Name).To(gomega.Equal("valid-sidecar"))
	})
}

func TestToDeploymentSpec_ContainerOptions(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{Image: "test:1.0"}

	spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel,
		WithEnvVars(
			envLiteral("QUARKUS_DATASOURCE_DB_KIND", "postgresql"),
			envLiteral("QUARKUS_FLOW_RUNNER_SECURITY_TYPE", "none"),
		),
	)

	container := spec.Template.Spec.Containers[0]
	g.Expect(container.Env).To(gomega.HaveLen(2))
	g.Expect(*container.Env[0].Name).To(gomega.Equal("QUARKUS_DATASOURCE_DB_KIND"))
	g.Expect(*container.Env[0].Value).To(gomega.Equal("postgresql"))
	g.Expect(*container.Env[1].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SECURITY_TYPE"))
}

func TestToDeploymentSpec_Probes(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image: "test:1.0",
		Container: logicv1.ContainerSpec{
			LivenessProbe: &corev1.Probe{
				ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/q/health/live"}},
				InitialDelaySeconds: 30,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/q/health/ready"}},
			},
		},
	}

	spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
	container := spec.Template.Spec.Containers[0]
	g.Expect(*container.LivenessProbe.HTTPGet.Path).To(gomega.Equal("/q/health/live"))
	g.Expect(*container.LivenessProbe.InitialDelaySeconds).To(gomega.Equal(int32(30)))
	g.Expect(*container.ReadinessProbe.HTTPGet.Path).To(gomega.Equal("/q/health/ready"))
}

func TestToDeploymentSpec_SchedulingConstraints(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image: "test:1.0",
		PodTemplate: logicv1.PodTemplateSpec{
			PodSpec: logicv1.PodSpec{
				NodeSelector:       map[string]string{"disktype": "ssd"},
				ServiceAccountName: "flow-sa",
				Tolerations: []corev1.Toleration{
					{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "flow", Effect: corev1.TaintEffectNoSchedule},
				},
			},
		},
	}

	spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
	g.Expect(spec.Template.Spec.NodeSelector).To(gomega.HaveKeyWithValue("disktype", "ssd"))
	g.Expect(*spec.Template.Spec.ServiceAccountName).To(gomega.Equal("flow-sa"))
	g.Expect(spec.Template.Spec.Tolerations).To(gomega.HaveLen(1))
	g.Expect(*spec.Template.Spec.Tolerations[0].Key).To(gomega.Equal("dedicated"))
}

func TestChildLabels(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("propagates CR labels with operator labels", func(t *testing.T) {
		g = gomega.NewWithT(t)
		rt := &logicv1.LogicFlowRuntime{}
		rt.Name = "my-runtime"
		rt.Labels = map[string]string{"team": "platform"}

		labels := ChildLabels(rt)
		g.Expect(labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/name", "my-runtime"))
		g.Expect(labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
		g.Expect(labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
		g.Expect(labels).To(gomega.HaveKeyWithValue("team", "platform"))
	})

	t.Run("operator labels override CR labels", func(t *testing.T) {
		g = gomega.NewWithT(t)
		rt := &logicv1.LogicFlowRuntime{}
		rt.Name = "my-runtime"
		rt.Labels = map[string]string{"app.kubernetes.io/managed-by": "user-override"}

		labels := ChildLabels(rt)
		g.Expect(labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
	})
}

func TestSelectorLabels(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("my-runtime")
	g.Expect(sel).To(gomega.HaveLen(2))
	g.Expect(sel).To(gomega.HaveKeyWithValue("app.kubernetes.io/name", "my-runtime"))
	g.Expect(sel).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
}
