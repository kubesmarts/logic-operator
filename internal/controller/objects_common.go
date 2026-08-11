package controller

import (
	"encoding/json"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	ContainerNameRunner     = "logic-runner"
	FieldOwnerLogicOperator = "logic-operator"
	LabelManagedBy          = "logic-operator"
	LabelPartOf             = "logic-platform"

	ConfigMapPrefix      = "lfd-"
	LabelRuntimeRef      = "logic.kubesmarts.org/runtime-ref"
	LabelWorkflowName    = "logic.kubesmarts.org/workflow-name"
	LabelWorkflowVersion = "logic.kubesmarts.org/workflow-version"

	LabelKeyName      = "app.kubernetes.io/name"
	LabelKeyManagedBy = "app.kubernetes.io/managed-by"
)

func ChildLabels(owner metav1.Object) map[string]string {
	labels := make(map[string]string)
	for k, v := range owner.GetLabels() {
		labels[k] = v
	}
	labels[LabelKeyName] = owner.GetName()
	labels[LabelKeyManagedBy] = LabelManagedBy
	labels["app.kubernetes.io/part-of"] = LabelPartOf
	return labels
}

func SelectorLabels(name string) map[string]string {
	return map[string]string{
		LabelKeyName:      name,
		LabelKeyManagedBy: LabelManagedBy,
	}
}

func OwnerRef(owner metav1.Object, kind string) *metav1ac.OwnerReferenceApplyConfiguration {
	return metav1ac.OwnerReference().
		WithAPIVersion(logicv1.GroupVersion.String()).
		WithKind(kind).
		WithName(owner.GetName()).
		WithUID(owner.GetUID()).
		WithBlockOwnerDeletion(true).
		WithController(true)
}

// ContainerOption decorates the main container before it's placed in the pod spec.
type ContainerOption func(*corev1ac.ContainerApplyConfiguration)

// WithEnvVars appends environment variables to the main container.
func WithEnvVars(envs ...*corev1ac.EnvVarApplyConfiguration) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		c.WithEnv(envs...)
	}
}

// ToDeploymentSpec converts an ApplicationSpec into a DeploymentSpecApplyConfiguration.
// Handles the 3-tier configuration precedence: podTemplate.container > container > top-level shortcuts.
// ContainerOptions are applied after precedence resolution, allowing callers to inject
// env vars, volume mounts, or other container-level config without modifying this function.
func ToDeploymentSpec(
	containerName string,
	app *logicv1.ApplicationSpec,
	podLabels map[string]string,
	selLabels map[string]string,
	opts ...ContainerOption,
) *appsv1ac.DeploymentSpecApplyConfiguration {
	container := resolveContainerAC(containerName, app)
	for _, opt := range opts {
		opt(container)
	}
	podSpec := toPodSpecAC(&app.PodTemplate, container)

	templateLabels := MergeMaps(podLabels)
	if app.PodTemplate.Metadata != nil {
		templateLabels = MergeMaps(podLabels, app.PodTemplate.Metadata.Labels)
	}

	podTemplate := corev1ac.PodTemplateSpec().
		WithLabels(templateLabels).
		WithSpec(podSpec)

	if app.PodTemplate.Metadata != nil && len(app.PodTemplate.Metadata.Annotations) > 0 {
		podTemplate = podTemplate.WithAnnotations(app.PodTemplate.Metadata.Annotations)
	}

	spec := appsv1ac.DeploymentSpec().
		WithSelector(metav1ac.LabelSelector().WithMatchLabels(selLabels)).
		WithTemplate(podTemplate)

	replicas := app.Replicas
	if app.PodTemplate.Replicas != nil {
		replicas = app.PodTemplate.Replicas
	}
	if replicas != nil {
		spec = spec.WithReplicas(*replicas)
	}

	return spec
}

// resolveContainerAC resolves the 3-tier container precedence into a single ContainerApplyConfiguration.
// Precedence: podTemplate.container > container > top-level shortcuts.
// Each layer overrides non-zero fields from the previous layer.
func resolveContainerAC(containerName string, app *logicv1.ApplicationSpec) *corev1ac.ContainerApplyConfiguration {
	c := corev1ac.Container().WithName(containerName).
		WithSecurityContext(restrictedContainerSecurity())

	applyContainerFields(c, &logicv1.ContainerSpec{
		Image:           app.Image,
		ImagePullPolicy: app.ImagePullPolicy,
		Resources:       app.Resources,
	})
	applyContainerFields(c, &app.Container)
	applyContainerFields(c, &app.PodTemplate.Container)

	return c
}

// applyContainerFields applies non-zero fields from a ContainerSpec onto a ContainerApplyConfiguration.
func applyContainerFields(c *corev1ac.ContainerApplyConfiguration, cs *logicv1.ContainerSpec) {
	if cs.Image != "" {
		c.WithImage(cs.Image)
	}
	if cs.ImagePullPolicy != "" {
		c.WithImagePullPolicy(cs.ImagePullPolicy)
	}
	if cs.Resources.Requests != nil || cs.Resources.Limits != nil {
		c.WithResources(convertTo[corev1ac.ResourceRequirementsApplyConfiguration](cs.Resources))
	}
	if cs.Command != nil {
		c.WithCommand(cs.Command...)
	}
	if cs.Args != nil {
		c.WithArgs(cs.Args...)
	}
	if cs.WorkingDir != "" {
		c.WithWorkingDir(cs.WorkingDir)
	}
	if cs.Env != nil {
		c.WithEnv(convertSliceTo[corev1.EnvVar, corev1ac.EnvVarApplyConfiguration](cs.Env)...)
	}
	if cs.EnvFrom != nil {
		c.WithEnvFrom(convertSliceTo[corev1.EnvFromSource, corev1ac.EnvFromSourceApplyConfiguration](cs.EnvFrom)...)
	}
	if cs.VolumeMounts != nil {
		c.WithVolumeMounts(convertSliceTo[corev1.VolumeMount, corev1ac.VolumeMountApplyConfiguration](cs.VolumeMounts)...)
	}
	if cs.SecurityContext != nil {
		c.WithSecurityContext(convertTo[corev1ac.SecurityContextApplyConfiguration](cs.SecurityContext))
	}
	if cs.LivenessProbe != nil {
		c.WithLivenessProbe(convertTo[corev1ac.ProbeApplyConfiguration](cs.LivenessProbe))
	}
	if cs.ReadinessProbe != nil {
		c.WithReadinessProbe(convertTo[corev1ac.ProbeApplyConfiguration](cs.ReadinessProbe))
	}
	if cs.StartupProbe != nil {
		c.WithStartupProbe(convertTo[corev1ac.ProbeApplyConfiguration](cs.StartupProbe))
	}
	if cs.Lifecycle != nil {
		c.WithLifecycle(convertTo[corev1ac.LifecycleApplyConfiguration](cs.Lifecycle))
	}
}

// toPodSpecAC converts a PodTemplateSpec into a PodSpecApplyConfiguration.
// The main container is passed separately since it's resolved from the 3-tier precedence.
func toPodSpecAC(pt *logicv1.PodTemplateSpec, mainContainer *corev1ac.ContainerApplyConfiguration) *corev1ac.PodSpecApplyConfiguration {
	mainName := ""
	if mainContainer.Name != nil {
		mainName = *mainContainer.Name
	}
	ps := corev1ac.PodSpec().
		WithContainers(mainContainer).
		WithSecurityContext(restrictedPodSecurity())

	if pt.NodeSelector != nil {
		ps.WithNodeSelector(pt.NodeSelector)
	}
	if pt.Affinity != nil {
		ps.WithAffinity(convertTo[corev1ac.AffinityApplyConfiguration](pt.Affinity))
	}
	if pt.Tolerations != nil {
		ps.WithTolerations(convertSliceTo[corev1.Toleration, corev1ac.TolerationApplyConfiguration](pt.Tolerations)...)
	}
	if pt.TopologySpreadConstraints != nil {
		ps.WithTopologySpreadConstraints(convertSliceTo[corev1.TopologySpreadConstraint, corev1ac.TopologySpreadConstraintApplyConfiguration](pt.TopologySpreadConstraints)...)
	}
	if pt.PriorityClassName != "" {
		ps.WithPriorityClassName(pt.PriorityClassName)
	}
	if pt.SchedulerName != "" {
		ps.WithSchedulerName(pt.SchedulerName)
	}
	if pt.SecurityContext != nil {
		ps.WithSecurityContext(convertTo[corev1ac.PodSecurityContextApplyConfiguration](pt.SecurityContext))
	}
	if pt.ServiceAccountName != "" {
		ps.WithServiceAccountName(pt.ServiceAccountName)
	}
	if pt.ImagePullSecrets != nil {
		ps.WithImagePullSecrets(convertSliceTo[corev1.LocalObjectReference, corev1ac.LocalObjectReferenceApplyConfiguration](pt.ImagePullSecrets)...)
	}
	if pt.Volumes != nil {
		ps.WithVolumes(convertSliceTo[corev1.Volume, corev1ac.VolumeApplyConfiguration](pt.Volumes)...)
	}
	if pt.InitContainers != nil {
		ps.WithInitContainers(convertSliceTo[corev1.Container, corev1ac.ContainerApplyConfiguration](pt.InitContainers)...)
	}
	if pt.Containers != nil {
		sidecars := filterSidecars(pt.Containers, mainName)
		if len(sidecars) > 0 {
			ps.WithContainers(convertSliceTo[corev1.Container, corev1ac.ContainerApplyConfiguration](sidecars)...)
		}
	}
	if pt.HostAliases != nil {
		ps.WithHostAliases(convertSliceTo[corev1.HostAlias, corev1ac.HostAliasApplyConfiguration](pt.HostAliases)...)
	}
	if pt.RestartPolicy != "" {
		ps.WithRestartPolicy(pt.RestartPolicy)
	}
	if pt.TerminationGracePeriodSeconds != nil {
		ps.WithTerminationGracePeriodSeconds(*pt.TerminationGracePeriodSeconds)
	}
	if pt.DNSPolicy != "" {
		ps.WithDNSPolicy(pt.DNSPolicy)
	}
	if pt.DNSConfig != nil {
		ps.WithDNSConfig(convertTo[corev1ac.PodDNSConfigApplyConfiguration](pt.DNSConfig))
	}

	return ps
}

func filterSidecars(containers []corev1.Container, mainName string) []corev1.Container {
	filtered := make([]corev1.Container, 0, len(containers))
	for _, c := range containers {
		if c.Name != mainName {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// convertTo converts a Kubernetes API type to its apply configuration counterpart via JSON round-trip.
// This works because both types share the same JSON schema (identical json tags).
// Used for complex nested types (Affinity, Probes, SecurityContext, etc.) where manual
// field-by-field conversion would be hundreds of lines of mechanical code.
func convertTo[T any](src any) *T {
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	dst := new(T)
	if err := json.Unmarshal(data, dst); err != nil {
		return nil
	}
	return dst
}

// convertSliceTo converts a slice of Kubernetes API types to their apply configuration counterparts.
func convertSliceTo[S any, D any](src []S) []*D {
	if len(src) == 0 {
		return nil
	}
	result := make([]*D, 0, len(src))
	for i := range src {
		if d := convertTo[D](src[i]); d != nil {
			result = append(result, d)
		}
	}
	return result
}

func MergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

func effectiveReplicas(app *logicv1.ApplicationSpec) int32 {
	if app.PodTemplate.Replicas != nil {
		return *app.PodTemplate.Replicas
	}
	if app.Replicas != nil {
		return *app.Replicas
	}
	return 1
}

func durableDeploymentStrategy(replicas int32) *appsv1ac.DeploymentStrategyApplyConfiguration {
	if replicas <= 1 {
		return appsv1ac.DeploymentStrategy().
			WithType(appsv1.RecreateDeploymentStrategyType)
	}
	one := intstr.FromInt32(1)
	return appsv1ac.DeploymentStrategy().
		WithType(appsv1.RollingUpdateDeploymentStrategyType).
		WithRollingUpdate(appsv1ac.RollingUpdateDeployment().
			WithMaxUnavailable(one).
			WithMaxSurge(one))
}

func restrictedContainerSecurity() *corev1ac.SecurityContextApplyConfiguration {
	return corev1ac.SecurityContext().
		WithAllowPrivilegeEscalation(false).
		WithCapabilities(corev1ac.Capabilities().
			WithDrop("ALL"))
}

func restrictedPodSecurity() *corev1ac.PodSecurityContextApplyConfiguration {
	return corev1ac.PodSecurityContext().
		WithRunAsNonRoot(true).
		WithSeccompProfile(corev1ac.SeccompProfile().
			WithType(corev1.SeccompProfileTypeRuntimeDefault))
}

func memberLeaseLabels(poolName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": DurableManagedByValue,
		"app.kubernetes.io/component":  DurableComponentValue,
		LabelDurablePool:               poolName,
		LabelDurableIsLeader:           "false",
	}
}

func newMemberLease(name, namespace, poolName string, dep *appsv1.Deployment) *coordinationv1.Lease {
	duration := LeaseDuration
	controller := false
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    memberLeaseLabels(poolName),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       dep.Name,
					UID:        dep.UID,
					Controller: &controller,
				},
			},
		},
		Spec: coordinationv1.LeaseSpec{
			LeaseDurationSeconds: &duration,
		},
	}
}

// FlowVolumes returns pod-level Volume entries for ConfigMaps.
func FlowVolumes(configMaps []corev1.ConfigMap) []*corev1ac.VolumeApplyConfiguration {
	vols := make([]*corev1ac.VolumeApplyConfiguration, 0, len(configMaps))
	for i := range configMaps {
		vols = append(vols, corev1ac.Volume().
			WithName(configMaps[i].Name).
			WithConfigMap(corev1ac.ConfigMapVolumeSource().
				WithName(configMaps[i].Name)))
	}
	return vols
}
