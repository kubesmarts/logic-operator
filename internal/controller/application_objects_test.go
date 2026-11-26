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
	podLabels := map[string]string{testLabelKeyName: testRuntimeName}
	selLabels := SelectorLabels(testRuntimeName)

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
			app:      logicv1.ApplicationSpec{Image: testImageTop},
			expected: testImageTop,
		},
		{
			name: "container overrides top-level",
			app: logicv1.ApplicationSpec{
				Image:     testImageTop,
				Container: logicv1.ContainerSpec{Image: testImageContainer},
			},
			expected: testImageContainer,
		},
		{
			name: "podTemplate.container overrides all",
			app: logicv1.ApplicationSpec{
				Image:     testImageTop,
				Container: logicv1.ContainerSpec{Image: testImageContainer},
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
		Image: testImageBase,
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

func TestToDeploymentSpec_ImagePullPolicy(t *testing.T) {
	sel := SelectorLabels("test")

	tests := []struct {
		name     string
		app      logicv1.ApplicationSpec
		expected corev1.PullPolicy
	}{
		{
			name:     "fallback when no tier sets it",
			app:      logicv1.ApplicationSpec{Image: testImageBase},
			expected: DefaultImagePullPolicy,
		},
		{
			name:     "top-level explicit is kept",
			app:      logicv1.ApplicationSpec{Image: testImageBase, ImagePullPolicy: corev1.PullAlways},
			expected: corev1.PullAlways,
		},
		{
			name: "container overrides top-level",
			app: logicv1.ApplicationSpec{
				Image:           testImageBase,
				ImagePullPolicy: corev1.PullAlways,
				Container:       logicv1.ContainerSpec{ImagePullPolicy: corev1.PullNever},
			},
			expected: corev1.PullNever,
		},
		{
			name: "podTemplate.container overrides all",
			app: logicv1.ApplicationSpec{
				Image:           testImageBase,
				ImagePullPolicy: corev1.PullAlways,
				Container:       logicv1.ContainerSpec{ImagePullPolicy: corev1.PullNever},
				PodTemplate: logicv1.PodTemplateSpec{
					Container: logicv1.ContainerSpec{ImagePullPolicy: corev1.PullIfNotPresent},
				},
			},
			expected: corev1.PullIfNotPresent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			spec := ToDeploymentSpec(ContainerNameRunner, &tt.app, sel, sel)
			g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(tt.expected))
		})
	}
}

func TestToDaemonSetSpec_ImagePullPolicyFallback(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	spec := ToDaemonSetSpec(ContainerNameRunner, &logicv1.ApplicationSpec{Image: testImageBase}, sel, sel, nil)

	g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(DefaultImagePullPolicy))
}

func TestToDeploymentSpec_Replicas(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	t.Run("top-level", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase, Replicas: int32Ptr(3)}
		spec := ToDeploymentSpec(ContainerNameRunner, app, sel, sel)
		g.Expect(*spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("podTemplate overrides top-level", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image:    testImageBase,
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
		app := &logicv1.ApplicationSpec{Image: testImageBase}
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
		app := &logicv1.ApplicationSpec{Image: testImageBase}
		spec := ToDeploymentSpec(ContainerNameRunner, app, podLabels, sel)
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
	})

	t.Run("user labels merged from podTemplate.metadata", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image: testImageBase,
			PodTemplate: logicv1.PodTemplateSpec{
				Metadata: &logicv1.PodTemplateMetadata{
					Labels: map[string]string{testLabelTeam: testPlatformLabel, "env": "prod"},
				},
			},
		}
		spec := ToDeploymentSpec(ContainerNameRunner, app, podLabels, sel)
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelTeam, testPlatformLabel))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("env", "prod"))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
	})

	t.Run("operator labels win over colliding podTemplate labels", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{
			Image: testImageBase,
			PodTemplate: logicv1.PodTemplateSpec{
				Metadata: &logicv1.PodTemplateMetadata{
					Labels: map[string]string{
						testLabelKeyManagedBy:       "user-value",
						"app.kubernetes.io/part-of": "user-value",
						testLabelKeyName:            "user-value",
						testLabelTeam:               testPlatformLabel,
					},
				},
			},
		}
		rt := &logicv1.LogicFlowRuntime{}
		rt.Name = testRuntimeName
		spec := ToDeploymentSpec(ContainerNameRunner, app, ChildLabels(rt), sel)
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelKeyName, testRuntimeName))
		g.Expect(spec.Template.Labels).To(gomega.HaveKeyWithValue(testLabelTeam, testPlatformLabel))
	})
}

func TestToDeploymentSpec_Annotations(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image: testImageBase,
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
			Image: testImageBase,
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
			Image: testImageBase,
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
		g.Expect(*spec.Template.Spec.Containers[0].Image).To(gomega.Equal(testImageBase))
		g.Expect(*spec.Template.Spec.Containers[1].Name).To(gomega.Equal("valid-sidecar"))
	})
}

func TestToDeploymentSpec_ContainerOptions(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{Image: testImageBase}

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
		Image: testImageBase,
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
		Image: testImageBase,
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
