package controller

import (
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

func memRequests(q string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(q)}}
}

func TestMergeApplicationDefaults_NilDefaults(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{Image: testImageBase, Replicas: int32Ptr(2)}

	effective, tunings, err := mergeApplicationDefaults(app, nil)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective).To(gomega.Equal(*app))
	g.Expect(tunings).To(gomega.Equal(probeTunings{}))
}

func TestMergeApplicationDefaults_DoesNotModifyInputs(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Image:     testImageBase,
		Container: logicv1.ContainerSpec{Env: []corev1.EnvVar{{Name: "B", Value: "rt"}}},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Container: logicv1.ContainerDefaultsSpec{Env: []logicv1.EnvVarDefault{{Name: "A", Value: "pf"}}},
	}
	appBefore := app.DeepCopy()
	defaultsBefore := defaults.DeepCopy()

	_, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(app).To(gomega.Equal(appBefore))
	g.Expect(defaults).To(gomega.Equal(defaultsBefore))
}

func TestMergeApplicationDefaults_PlatformOnly(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{}
	defaults := &logicv1.ApplicationDefaultsSpec{
		ImagePullPolicy: corev1.PullAlways,
		Resources:       memRequests("256Mi"),
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.ImagePullPolicy).To(gomega.Equal(corev1.PullAlways))
	g.Expect(effective.Resources.Requests.Memory().String()).To(gomega.Equal("256Mi"))
	// replicas are never taken from the platform
	g.Expect(effective.Replicas).To(gomega.BeNil())
}

func TestMergeApplicationDefaults_RuntimeWins(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		ImagePullPolicy: corev1.PullNever,
		Resources:       memRequests("512Mi"),
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		ImagePullPolicy: corev1.PullAlways,
		Resources:       memRequests("256Mi"),
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.ImagePullPolicy).To(gomega.Equal(corev1.PullNever))
	g.Expect(effective.Resources.Requests.Memory().String()).To(gomega.Equal("512Mi"))
}

func TestMergeApplicationDefaults_CrossTierPrecedence(t *testing.T) {
	sel := SelectorLabels("test")

	t.Run("platform container policy does not beat runtime top-level policy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase, ImagePullPolicy: corev1.PullNever}
		defaults := &logicv1.ApplicationDefaultsSpec{
			Container: logicv1.ContainerDefaultsSpec{ImagePullPolicy: corev1.PullAlways},
		}

		effective, _, err := mergeApplicationDefaults(app, defaults)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
		g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(corev1.PullNever))
	})

	t.Run("platform podTemplate.container policy beats platform top-level policy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase}
		defaults := &logicv1.ApplicationDefaultsSpec{
			ImagePullPolicy: corev1.PullNever,
			PodTemplate: logicv1.PodTemplateDefaultsSpec{
				Container: logicv1.ContainerDefaultsSpec{ImagePullPolicy: corev1.PullAlways},
			},
		}

		effective, _, err := mergeApplicationDefaults(app, defaults)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
		g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(corev1.PullAlways))
	})

	t.Run("platform value is used when the runtime has none", func(t *testing.T) {
		g := gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase}
		defaults := &logicv1.ApplicationDefaultsSpec{ImagePullPolicy: corev1.PullAlways}

		effective, _, err := mergeApplicationDefaults(app, defaults)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
		g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(corev1.PullAlways))
	})

	t.Run("operator fallback applies when neither sets a policy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase}
		defaults := &logicv1.ApplicationDefaultsSpec{Resources: memRequests("256Mi")}

		effective, _, err := mergeApplicationDefaults(app, defaults)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
		g.Expect(*spec.Template.Spec.Containers[0].ImagePullPolicy).To(gomega.Equal(DefaultImagePullPolicy))
	})
}

func TestMergeApplicationDefaults_RuntimeReplicasKept(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Replicas:    int32Ptr(2),
		PodTemplate: logicv1.PodTemplateSpec{Replicas: int32Ptr(5)},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{Resources: memRequests("256Mi")}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(*effective.Replicas).To(gomega.Equal(int32(2)))
	g.Expect(effectiveReplicas(&effective)).To(gomega.Equal(int32(5)))
}

func TestMergeApplicationDefaults_EnvMergeByName(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Container: logicv1.ContainerSpec{Env: []corev1.EnvVar{
			{Name: "B", Value: "rt"},
			{Name: "C", Value: "rt"},
		}},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Container: logicv1.ContainerDefaultsSpec{Env: []logicv1.EnvVarDefault{
			{Name: "A", Value: "pf"},
			{Name: "B", Value: "pf"},
		}},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.Container.Env).To(gomega.Equal([]corev1.EnvVar{
		{Name: "A", Value: "pf"},
		{Name: "B", Value: "rt"},
		{Name: "C", Value: "rt"},
	}))
}

func TestMergeApplicationDefaults_EnvRuntimePodTemplateOverridesPlatform(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		PodTemplate: logicv1.PodTemplateSpec{
			Container: logicv1.ContainerSpec{Env: []corev1.EnvVar{{Name: "B", Value: "rt"}}},
		},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Container: logicv1.ContainerDefaultsSpec{Env: []logicv1.EnvVarDefault{
			{Name: "A", Value: "pf"},
			{Name: "B", Value: "pf"},
		}},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.Container.Env).To(gomega.Equal([]corev1.EnvVar{{Name: "A", Value: "pf"}}))
	g.Expect(effective.PodTemplate.Container.Env).To(gomega.Equal([]corev1.EnvVar{{Name: "B", Value: "rt"}}))
}

func TestMergeApplicationDefaults_EnvReachesDeploymentWithoutDuplicates(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")
	app := &logicv1.ApplicationSpec{
		Image:     testImageBase,
		Container: logicv1.ContainerSpec{Env: []corev1.EnvVar{{Name: "B", Value: "rt"}}},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		PodTemplate: logicv1.PodTemplateDefaultsSpec{
			Container: logicv1.ContainerDefaultsSpec{Env: []logicv1.EnvVarDefault{
				{Name: "A", Value: "pf"},
				{Name: "B", Value: "pf"},
			}},
		},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
	names := map[string]string{}
	count := 0
	for _, e := range spec.Template.Spec.Containers[0].Env {
		names[*e.Name] = *e.Value
		count++
	}
	g.Expect(count).To(gomega.Equal(2))
	g.Expect(names).To(gomega.Equal(map[string]string{"A": "pf", "B": "rt"}))
}

func TestMergeApplicationDefaults_ResourcesReplacedWhole(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Container: logicv1.ContainerSpec{Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		}},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	sel := SelectorLabels("test")
	effective.Image = testImageBase
	res := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel).Template.Spec.Containers[0].Resources
	g.Expect(res.Limits.Memory().String()).To(gomega.Equal("1Gi"))
	g.Expect(res.Requests).To(gomega.BeNil())
}

func TestMergeApplicationDefaults_ResourcesWithOnlyClaimsCountAsSet(t *testing.T) {
	g := gomega.NewWithT(t)
	claims := []corev1.ResourceClaim{{Name: "gpu"}}
	app := &logicv1.ApplicationSpec{
		Container: logicv1.ContainerSpec{Resources: corev1.ResourceRequirements{Claims: claims}},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{Resources: memRequests("256Mi")}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.Resources.Requests).To(gomega.BeNil())
	g.Expect(effective.Container.Resources.Claims).To(gomega.Equal(claims))

	sel := SelectorLabels("test")
	effective.Image = testImageBase
	res := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel).Template.Spec.Containers[0].Resources
	g.Expect(res.Claims).To(gomega.HaveLen(1))
	g.Expect(*res.Claims[0].Name).To(gomega.Equal("gpu"))
}

func TestMergeApplicationDefaults_PlatformResourcesWithOnlyClaimsAreApplied(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{Image: testImageBase}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Resources: corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "gpu"}}},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	sel := SelectorLabels("test")
	res := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel).Template.Spec.Containers[0].Resources
	g.Expect(res.Claims).To(gomega.HaveLen(1))
}

func TestMergeApplicationDefaults_PodMetadataMergesAsMaps(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		PodTemplate: logicv1.PodTemplateSpec{
			Metadata: &logicv1.PodTemplateMetadata{
				Labels:      map[string]string{"team": "rt", "rt-only": "x"},
				Annotations: map[string]string{"note": "rt"},
			},
		},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		PodTemplate: logicv1.PodTemplateDefaultsSpec{
			Metadata: &logicv1.PodTemplateMetadata{
				Labels:      map[string]string{"team": "pf", "pf-only": "y"},
				Annotations: map[string]string{"prometheus.io/scrape": "true"},
			},
		},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.PodTemplate.Metadata.Labels).To(gomega.Equal(map[string]string{
		"team": "rt", "rt-only": "x", "pf-only": "y",
	}))
	g.Expect(effective.PodTemplate.Metadata.Annotations).To(gomega.Equal(map[string]string{
		"note": "rt", "prometheus.io/scrape": "true",
	}))
	// the runtime spec is untouched
	g.Expect(app.PodTemplate.Metadata.Labels).To(gomega.Equal(map[string]string{"team": "rt", "rt-only": "x"}))
}

func TestMergeApplicationDefaults_PodMetadataPlatformOnly(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{}
	defaults := &logicv1.ApplicationDefaultsSpec{
		PodTemplate: logicv1.PodTemplateDefaultsSpec{
			Metadata: &logicv1.PodTemplateMetadata{Labels: map[string]string{"team": "pf"}},
		},
	}

	effective, _, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.PodTemplate.Metadata.Labels).To(gomega.Equal(map[string]string{"team": "pf"}))
	g.Expect(effective.PodTemplate.Metadata.Annotations).To(gomega.BeNil())
}

func TestMergeApplicationDefaults_PodLabelsEndToEnd(t *testing.T) {
	g := gomega.NewWithT(t)
	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = testRuntimeName
	rt.Labels = map[string]string{
		testLabelKeyName:            "runtime-cr-value",
		testLabelKeyManagedBy:       "runtime-cr-value",
		"app.kubernetes.io/part-of": "runtime-cr-value",
		"cr-top-level":              "runtime-cr-value",
		"cr-top-level-only-in-cr":   "runtime-cr-value",
	}
	rt.Spec.PodTemplate.Metadata = &logicv1.PodTemplateMetadata{
		Labels: map[string]string{"cr-pod-template": "runtime-cr-value"},
	}
	defaults := &logicv1.ApplicationDefaultsSpec{
		PodTemplate: logicv1.PodTemplateDefaultsSpec{
			Metadata: &logicv1.PodTemplateMetadata{
				Labels: map[string]string{
					"platform-only":             "platform-value",
					testLabelKeyName:            "platform-value",
					testLabelKeyManagedBy:       "platform-value",
					"app.kubernetes.io/part-of": "platform-value",
					"cr-top-level":              "platform-value",
					"cr-pod-template":           "platform-value",
				},
			},
		},
	}

	effective, _, err := mergeApplicationDefaults(&rt.Spec.ApplicationSpec, defaults)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	spec := ToDeploymentSpec(ContainerNameRunner, &effective, ChildLabels(rt), SelectorLabels(rt.Name))

	g.Expect(spec.Template.Labels).To(gomega.Equal(map[string]string{
		testLabelKeyName:            testRuntimeName,
		testLabelKeyManagedBy:       LabelManagedBy,
		"app.kubernetes.io/part-of": LabelPartOf,
		"platform-only":             "platform-value",
		"cr-top-level":              "runtime-cr-value",
		"cr-top-level-only-in-cr":   "runtime-cr-value",
		"cr-pod-template":           "runtime-cr-value",
	}))
}

func TestMergeApplicationDefaults_RunnerImageNeverFromPlatform(t *testing.T) {
	g := gomega.NewWithT(t)
	sel := SelectorLabels("test")

	t.Run("runtime without image stays without image", func(t *testing.T) {
		g = gomega.NewWithT(t)
		defaults := &logicv1.ApplicationDefaultsSpec{
			ImagePullPolicy: corev1.PullAlways,
			Resources:       memRequests("256Mi"),
		}

		effective, _, err := mergeApplicationDefaults(&logicv1.ApplicationSpec{}, defaults)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(effective.Image).To(gomega.BeEmpty())
		g.Expect(effective.Container.Image).To(gomega.BeEmpty())
		g.Expect(effective.PodTemplate.Container.Image).To(gomega.BeEmpty())
		spec := ToDeploymentSpec(ContainerNameRunner, &effective, sel, sel)
		g.Expect(spec.Template.Spec.Containers[0].Image).To(gomega.BeNil())
	})

	t.Run("runtime image is kept", func(t *testing.T) {
		g = gomega.NewWithT(t)
		app := &logicv1.ApplicationSpec{Image: testImageBase}

		effective, _, err := mergeApplicationDefaults(app, &logicv1.ApplicationDefaultsSpec{})

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(effective.Image).To(gomega.Equal(testImageBase))
	})
}

func TestMergeApplicationDefaults_ProbeTuningsReturnedNotApplied(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{}
	defaults := &logicv1.ApplicationDefaultsSpec{
		Container: logicv1.ContainerDefaultsSpec{
			LivenessProbe: &logicv1.ProbeTuning{PeriodSeconds: int32Ptr(20)},
			StartupProbe:  &logicv1.ProbeTuning{PeriodSeconds: int32Ptr(5), FailureThreshold: int32Ptr(30)},
		},
		PodTemplate: logicv1.PodTemplateDefaultsSpec{
			Container: logicv1.ContainerDefaultsSpec{
				StartupProbe:   &logicv1.ProbeTuning{PeriodSeconds: int32Ptr(10)},
				ReadinessProbe: &logicv1.ProbeTuning{TimeoutSeconds: int32Ptr(4)},
			},
		},
	}

	effective, tunings, err := mergeApplicationDefaults(app, defaults)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(effective.Container.LivenessProbe).To(gomega.BeNil())
	g.Expect(effective.Container.StartupProbe).To(gomega.BeNil())
	g.Expect(effective.PodTemplate.Container.StartupProbe).To(gomega.BeNil())
	g.Expect(effective.PodTemplate.Container.ReadinessProbe).To(gomega.BeNil())
	g.Expect(*tunings.Liveness.PeriodSeconds).To(gomega.Equal(int32(20)))
	g.Expect(*tunings.Readiness.TimeoutSeconds).To(gomega.Equal(int32(4)))
	// podTemplate.container replaces the container tier whole
	g.Expect(*tunings.Startup.PeriodSeconds).To(gomega.Equal(int32(10)))
	g.Expect(tunings.Startup.FailureThreshold).To(gomega.BeNil())
}
