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

// PodSpec defines pod-level configuration for Logic Operator managed workloads.
// It provides a curated subset of Kubernetes PodSpec fields focused on production
// use cases while excluding dangerous host-level access.
//
// For sidecar containers, use the Containers field.
// For init containers, use the InitContainers field.
type PodSpec struct {
	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity specifies the pod's scheduling constraints.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations allow the pod to schedule onto nodes with matching taints.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints describes how a group of pods ought to spread across topology domains.
	// Scheduler will schedule pods in a way which abides by the constraints.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// PriorityClassName indicates the pod's priority. "system-node-critical" and "system-cluster-critical"
	// are two special keywords which indicate the highest priorities with the former being the highest priority.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// SchedulerName is the name of the scheduler to be used to schedule this pod.
	// If not specified, the default scheduler will be used.
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`

	// SecurityContext holds pod-level security attributes and common container settings.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run this pod.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ImagePullSecrets is a list of references to secrets in the same namespace to use for pulling images.
	// More info: https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Volumes is the list of volumes that can be mounted by containers belonging to the pod.
	// More info: https://kubernetes.io/docs/concepts/storage/volumes
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// InitContainers is the list of initialization containers belonging to the pod.
	// Init containers are executed in order prior to containers being started.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// Containers is the list of sidecar containers to run alongside the main container.
	// The main container is configured via ContainerSpec.
	//
	// Common use cases:
	//   - Log forwarding (fluent-bit, filebeat)
	//   - Application mesh proxies (Istio, Linkerd)
	//   - Monitoring agents (Prometheus exporter)
	//
	// Example:
	//   containers:
	//   - name: log-forwarder
	//     image: fluent/fluent-bit:2.0
	// +optional
	Containers []corev1.Container `json:"containers,omitempty"`

	// HostAliases is an optional list of hosts and IPs that will be injected into the pod's hosts file.
	// This is only valid for non-hostNetwork pods.
	// +optional
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`

	// RestartPolicy describes how the container should be restarted.
	// Only one of the following restart policies may be specified.
	// If none of the following policies is specified, the default one is Always.
	// +optional
	// +kubebuilder:default=Always
	// +kubebuilder:validation:Enum=Always;OnFailure;Never
	RestartPolicy corev1.RestartPolicy `json:"restartPolicy,omitempty"`

	// TerminationGracePeriodSeconds is the optional duration in seconds the pod needs to terminate gracefully.
	// May be decreased in delete request. Value must be non-negative integer.
	// The value zero indicates stop immediately via the kill signal (no opportunity to shut down).
	// +optional
	// +kubebuilder:validation:Minimum=0
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// DNSPolicy sets the DNS policy for the pod.
	// Defaults to "ClusterFirst".
	// +optional
	// +kubebuilder:default=ClusterFirst
	// +kubebuilder:validation:Enum=ClusterFirstWithHostNet;ClusterFirst;Default;None
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// DNSConfig specifies the DNS parameters of a pod.
	// Parameters specified here will be merged to the generated DNS configuration.
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

// ContainerSpec defines the main container configuration for Logic Operator managed services.
//
// The container name is managed by the operator and cannot be customized. This prevents
// conflicts and ensures consistent naming across deployments.
//
// For sidecar containers, use PodTemplate.Containers instead, which allows
// full corev1.Container specification including the Name field.
type ContainerSpec struct {
	// Image name for the application runtime.
	// This should be a fully qualified image reference including registry and tag.
	//
	// Example:
	//   image: quay.io/kubesmarts/quarkus-flow:2.0.0
	// +optional
	Image string `json:"image,omitempty"`

	// Command to run in the container (overrides container entrypoint).
	// The command is not run in a shell. If you need a shell, explicitly include it in the command.
	//
	// Example:
	//   command: ["/bin/sh", "-c"]
	// +optional
	Command []string `json:"command,omitempty"`

	// Args are arguments to the command (overrides container cmd).
	//
	// Example:
	//   args: ["--config=/etc/app/config.yaml", "--verbose"]
	// +optional
	Args []string `json:"args,omitempty"`

	// Env is a list of environment variables to set in the container.
	//
	// Example:
	//   env:
	//   - name: QUARKUS_PROFILE
	//     value: prod
	//   - name: JAVA_OPTS
	//     value: "-Xmx512m"
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom is a list of sources to populate environment variables from (ConfigMap/Secret).
	//
	// Example:
	//   envFrom:
	//   - configMapRef:
	//       name: app-config
	//   - secretRef:
	//       name: app-secrets
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Resources specifies compute resource requirements (CPU, memory).
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	//
	// Example:
	//   resources:
	//     requests:
	//       memory: "512Mi"
	//       cpu: "250m"
	//     limits:
	//       memory: "1Gi"
	//       cpu: "1000m"
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// VolumeMounts is a list of volume mounts for the container.
	// Volumes must be defined at the pod level (PodSpec.Volumes).
	//
	// Example:
	//   volumeMounts:
	//   - name: config
	//     mountPath: /etc/app
	//     readOnly: true
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// SecurityContext defines the security options the container should be run with.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/security-context/
	//
	// Example:
	//   securityContext:
	//     runAsNonRoot: true
	//     allowPrivilegeEscalation: false
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// LivenessProbe indicates whether the container is alive.
	// If the probe fails, the container will be restarted.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
	//
	// Example:
	//   livenessProbe:
	//     httpGet:
	//       path: /q/health/live
	//       port: 8080
	//     initialDelaySeconds: 30
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe indicates whether the container is ready to serve requests.
	// If the probe fails, the pod will be removed from service endpoints.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
	//
	// Example:
	//   readinessProbe:
	//     httpGet:
	//       path: /q/health/ready
	//       port: 8080
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// StartupProbe indicates that the pod has successfully initialized.
	// If specified, no other probes are executed until this completes successfully.
	// More info: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
	//
	// Example:
	//   startupProbe:
	//     httpGet:
	//       path: /q/health/started
	//       port: 8080
	//     failureThreshold: 30
	//     periodSeconds: 10
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`

	// Lifecycle describes actions that should be taken in response to container lifecycle events.
	//
	// Example (graceful shutdown):
	//   lifecycle:
	//     preStop:
	//       exec:
	//         command: ["/bin/sh", "-c", "sleep 15"]
	// +optional
	Lifecycle *corev1.Lifecycle `json:"lifecycle,omitempty"`

	// ImagePullPolicy describes when to pull the container image.
	// Defaults to Always if :latest tag is specified, or IfNotPresent otherwise.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// WorkingDir is the container's working directory.
	// If not specified, the container runtime's default will be used.
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

// PodDisruptionBudgetSpec describes the pod disruption budget configuration for Logic Operator managed workloads.
//
// A PodDisruptionBudget limits the number of pods of a replicated application that can be down simultaneously
// from voluntary disruptions (e.g., node drains, rolling updates).
//
// More info: https://kubernetes.io/docs/concepts/workloads/pods/disruptions/
type PodDisruptionBudgetSpec struct {
	// MinAvailable specifies the minimum number (or percentage) of pods that must remain available
	// during voluntary disruptions. For example, "3" means at least 3 pods must always be available,
	// or "75%" means at least 75% of pods must remain available.
	//
	// This is mutually exclusive with MaxUnavailable.
	//
	// Example:
	//   minAvailable: 2        # At least 2 pods
	//   minAvailable: "50%"    # At least 50% of pods
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty" protobuf:"bytes,1,opt,name=minAvailable"`

	// MaxUnavailable specifies the maximum number (or percentage) of pods that can be unavailable
	// during voluntary disruptions. For example, "1" means at most 1 pod can be down,
	// or "25%" means at most 25% of pods can be unavailable.
	//
	// This is mutually exclusive with MinAvailable.
	//
	// Example:
	//   maxUnavailable: 1      # At most 1 pod unavailable
	//   maxUnavailable: "30%"  # At most 30% unavailable
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty" protobuf:"bytes,3,opt,name=maxUnavailable"`
}

// PodTemplateSpec describes the desired pod template for Logic Operator managed workloads.
//
// This allows customization of pod configuration while maintaining operator control over
// critical settings. Fields specified here override operator defaults.
//
// Precedence order (highest to lowest):
//  1. PodTemplateSpec fields (most specific)
//  2. Spec-level fields (e.g., LogicFlowRuntimeSpec.Image)
//  3. Operator defaults
//
// Example usage:
//
//	podTemplate:
//	  metadata:
//	    labels:
//	      monitoring: enabled
//	  container:
//	    resources:
//	      requests:
//	        memory: 512Mi
//	  spec:
//	    affinity:
//	      podAntiAffinity: ...
type PodTemplateSpec struct {
	// Metadata allows setting custom labels and annotations on pods.
	// These will be merged with operator-managed labels/annotations.
	//
	// Example:
	//   metadata:
	//     labels:
	//       team: platform
	//       env: production
	//     annotations:
	//       prometheus.io/scrape: "true"
	// +optional
	Metadata *PodTemplateMetadata `json:"metadata,omitempty"`

	// Container configures the main application container.
	// Settings here override operator defaults.
	//
	// The container name is managed by the operator and cannot be customized.
	// Use Containers field for sidecar containers that need custom names.
	//
	// Example:
	//   container:
	//     image: quay.io/kubesmarts/quarkus-flow:2.0.0
	//     resources:
	//       requests:
	//         memory: 512Mi
	//       limits:
	//         memory: 1Gi
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// PodSpec defines pod-level configuration (scheduling, security, volumes, sidecars).
	// These fields are embedded inline for a cleaner YAML structure.
	// +optional
	PodSpec `json:",inline"`

	// Replicas specifies the desired number of pod replicas.
	// If not specified, the operator will use its default replica count.
	//
	// Note: This field is ignored if HorizontalPodAutoscaler is configured.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// PodDisruptionBudget defines the PodDisruptionBudget for this workload.
	// When configured, the operator will automatically create a PodDisruptionBudget
	// resource to limit voluntary disruptions.
	//
	// Example (at least 2 pods always available):
	//   podDisruptionBudget:
	//     minAvailable: 2
	//
	// Example (at most 1 pod unavailable):
	//   podDisruptionBudget:
	//     maxUnavailable: 1
	// +optional
	PodDisruptionBudget *PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
}

// PodTemplateMetadata defines metadata to be applied to pods.
//
// Unlike corev1.ObjectMeta, this only includes fields that make sense for pod templates.
// Fields like Name, Namespace, and OwnerReferences are managed by the operator.
type PodTemplateMetadata struct {
	// Labels are key-value pairs to be added to pod labels.
	// These will be merged with operator-managed labels.
	//
	// Example:
	//   labels:
	//     team: platform
	//     environment: production
	//     cost-center: engineering
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are key-value pairs to be added to pod annotations.
	// These will be merged with operator-managed annotations.
	//
	// Example:
	//   annotations:
	//     prometheus.io/scrape: "true"
	//     prometheus.io/port: "8080"
	//     vault.hashicorp.com/agent-inject: "true"
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}
