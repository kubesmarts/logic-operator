// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// PodSpec is a curated subset of corev1.PodSpec for operator-managed workloads, excluding host-level access.
type PodSpec struct {
	// NodeSelector constrains the pod to nodes with matching labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity specifies the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations allow the pod to schedule onto nodes with matching taints.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints control how pods spread across topology domains.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// PriorityClassName indicates the pod's priority.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// SchedulerName is the scheduler used for this pod; defaults to the cluster default scheduler.
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`

	// SecurityContext holds pod-level security attributes.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// ServiceAccountName is the ServiceAccount used to run this pod.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ImagePullSecrets lists secrets in the same namespace used to pull images.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Volumes that can be mounted by the pod's containers.
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// InitContainers run in order before the main containers start.
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// Containers are sidecars running alongside the main container (configured via ContainerSpec).
	// +optional
	Containers []corev1.Container `json:"containers,omitempty"`

	// HostAliases are entries injected into the pod's hosts file.
	// +optional
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`

	// RestartPolicy for the pod's containers.
	// +optional
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;OnFailure;Never
	RestartPolicy corev1.RestartPolicy `json:"restartPolicy,omitempty"`

	// TerminationGracePeriodSeconds is the time the pod has to shut down gracefully.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// DNSPolicy sets the DNS policy for the pod.
	// +optional
	// +kubebuilder:default=ClusterFirst
	// +kubebuilder:validation:Enum=ClusterFirstWithHostNet;ClusterFirst;Default;None
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// DNSConfig is merged into the generated DNS configuration.
	// +optional
	DNSConfig *corev1.PodDNSConfig `json:"dnsConfig,omitempty"`
}

// ToPodSpec converts the custom PodSpec to a Kubernetes corev1.PodSpec.
// This is used internally by the operator when creating Application/StatefulSet objects.
func (f *PodSpec) ToPodSpec() corev1.PodSpec {
	return corev1.PodSpec{
		Volumes:                       f.Volumes,
		InitContainers:                f.InitContainers,
		Containers:                    f.Containers,
		RestartPolicy:                 f.RestartPolicy,
		TerminationGracePeriodSeconds: f.TerminationGracePeriodSeconds,
		DNSPolicy:                     f.DNSPolicy,
		NodeSelector:                  f.NodeSelector,
		ServiceAccountName:            f.ServiceAccountName,
		SecurityContext:               f.SecurityContext,
		ImagePullSecrets:              f.ImagePullSecrets,
		Affinity:                      f.Affinity,
		SchedulerName:                 f.SchedulerName,
		Tolerations:                   f.Tolerations,
		HostAliases:                   f.HostAliases,
		PriorityClassName:             f.PriorityClassName,
		DNSConfig:                     f.DNSConfig,
		TopologySpreadConstraints:     f.TopologySpreadConstraints,
	}
}

// ContainerSpec configures the main container; its name is managed by the operator.
type ContainerSpec struct {
	// Image is the fully qualified container image reference.
	// +optional
	Image string `json:"image,omitempty"`

	// Command overrides the container entrypoint.
	// +optional
	Command []string `json:"command,omitempty"`

	// Args overrides the container cmd.
	// +optional
	Args []string `json:"args,omitempty"`

	// Env lists environment variables to set in the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom lists ConfigMaps/Secrets to populate environment variables from.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Resources specifies compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// VolumeMounts mounts volumes defined in PodSpec.Volumes.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// SecurityContext defines container-level security options.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// LivenessProbe restarts the container when it fails.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe removes the pod from Service endpoints when it fails.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// StartupProbe delays other probes until the container has started.
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`

	// Lifecycle defines actions taken on container lifecycle events.
	// +optional
	Lifecycle *corev1.Lifecycle `json:"lifecycle,omitempty"`

	// ImagePullPolicy describes when to pull the container image.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// WorkingDir is the container's working directory.
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`
}

// ToContainer converts ContainerSpec to a Kubernetes corev1.Container.
// The container name is injected by the operator to ensure consistent naming.
//
// This method is used internally by the operator when building pod specifications.
func (f *ContainerSpec) ToContainer(name string) corev1.Container {
	return corev1.Container{
		Name:            name, // Injected by operator
		Image:           f.Image,
		Command:         f.Command,
		Args:            f.Args,
		Env:             f.Env,
		EnvFrom:         f.EnvFrom,
		Resources:       f.Resources,
		VolumeMounts:    f.VolumeMounts,
		SecurityContext: f.SecurityContext,
		LivenessProbe:   f.LivenessProbe,
		ReadinessProbe:  f.ReadinessProbe,
		StartupProbe:    f.StartupProbe,
		Lifecycle:       f.Lifecycle,
		ImagePullPolicy: f.ImagePullPolicy,
		WorkingDir:      f.WorkingDir,
	}
}

// PodDisruptionBudgetSpec limits voluntary disruptions for operator-managed workloads.
type PodDisruptionBudgetSpec struct {
	// MinAvailable is the minimum number or percentage of pods that must remain available.
	// Mutually exclusive with MaxUnavailable.
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty" protobuf:"bytes,1,opt,name=minAvailable"`

	// MaxUnavailable is the maximum number or percentage of pods that can be unavailable.
	// Mutually exclusive with MinAvailable.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty" protobuf:"bytes,3,opt,name=maxUnavailable"`
}

// PodTemplateSpec customizes the pod for operator-managed workloads; fields here override operator defaults.
type PodTemplateSpec struct {
	// Metadata adds labels and annotations to pods, merged with operator-managed ones.
	// +optional
	Metadata *PodTemplateMetadata `json:"metadata,omitempty"`

	// Container configures the main application container.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// PodSpec defines pod-level configuration (scheduling, security, volumes, sidecars).
	// +optional
	PodSpec `json:",inline"`

	// Replicas is the desired number of pod replicas.
	// Ignored when HorizontalPodAutoscaler is configured.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// PodDisruptionBudget creates a PodDisruptionBudget for this workload when set.
	// +optional
	PodDisruptionBudget *PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
}

// PodTemplateMetadata defines labels and annotations applied to pods.
type PodTemplateMetadata struct {
	// Labels are merged with operator-managed pod labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are merged with operator-managed pod annotations.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}
