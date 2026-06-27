package v1

import corev1 "k8s.io/api/core/v1"

// RuntimeDefaultsSpec defines default configuration for all LogicFlowRuntime deployments
// in the namespace governed by this LogicPlatform.
//
// This provides a centralized way to set common defaults for all workflow runtimes,
// reducing duplication and ensuring consistency across deployments. Individual
// LogicFlowRuntime resources can override these defaults.
//
// Configuration precedence (highest to lowest):
//  1. LogicFlowRuntime-specific configuration (most specific)
//  2. RuntimeDefaultsSpec (this field) - namespace-level defaults
//  3. Operator built-in defaults (fallback)
//
// Common use cases:
//   - Set a default runtime image for all flows
//   - Configure default resource requests/limits
//   - Set default persistence configuration
//   - Configure default pod scheduling (affinity, tolerations)
//   - Set default replica count
//
// Example (platform-level runtime defaults):
//
//	runtimeDefaults:
//	  image: quay.io/kubesmarts/quarkus-flow:2.0.0
//	  replicas: 2
//	  resources:
//	    requests:
//	      memory: 512Mi
//	      cpu: 250m
//	    limits:
//	      memory: 1Gi
//	      cpu: 1000m
//	  persistence:
//	    postgresql:
//	      secretRef:
//	        name: postgres-credentials
//	      serviceRef:
//	        name: postgres
//	        databaseSchema: workflows
//	  podTemplate:
//	    affinity:
//	      podAntiAffinity:
//	        preferredDuringSchedulingIgnoredDuringExecution:
//	        - weight: 100
//	          podAffinityTerm:
//	            labelSelector:
//	              matchLabels:
//	                app: logic-flow-runtime
//	            topologyKey: kubernetes.io/hostname
type RuntimeDefaultsSpec struct {
	// Persistence configures the default database connection for all workflow runtimes.
	// Individual LogicFlowRuntime resources can override this configuration.
	//
	// This is the third level in the persistence cascade:
	//  1. logicplatform.spec.persistence (global default)
	//  2. logicplatform.spec.runtimeDefaults.persistence (runtime-specific default)
	//  3. Individual LogicFlowRuntime persistence config (most specific)
	// +optional
	Persistence *PersistenceOptionsSpec `json:"persistence,omitempty"`

	// Image specifies the default container image for all workflow runtimes.
	// This should be a fully qualified image reference including registry and tag.
	//
	// Individual LogicFlowRuntime resources can override this via:
	//  - logicflowruntime.spec.image, OR
	//  - logicflowruntime.spec.container.image, OR
	//  - logicflowruntime.spec.podTemplate.container.image
	//
	// Example: quay.io/kubesmarts/quarkus-flow:2.0.0
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the default desired number of pod replicas for workflow runtimes.
	// This is ignored when HorizontalPodAutoscaler is configured for a runtime.
	//
	// Individual runtimes can override via logicflowruntime.spec.replicas or
	// logicflowruntime.spec.podTemplate.replicas.
	//
	// Defaults to 1 if not specified.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources specifies default compute resource requirements for workflow runtime containers.
	// Individual runtimes can override via logicflowruntime.spec.resources,
	// logicflowruntime.spec.container.resources, or
	// logicflowruntime.spec.podTemplate.container.resources.
	//
	// Example:
	//   resources:
	//     requests:
	//       memory: 512Mi
	//       cpu: 250m
	//     limits:
	//       memory: 1Gi
	//       cpu: 1000m
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Container configures default settings for the workflow runtime application container.
	// Individual runtimes can override via logicflowruntime.spec.container or
	// logicflowruntime.spec.podTemplate.container.
	//
	// Use this to set common environment variables, volume mounts, probes, etc.
	// that should apply to all runtimes by default.
	//
	// Example:
	//   container:
	//     env:
	//     - name: QUARKUS_PROFILE
	//       value: prod
	//     livenessProbe:
	//       httpGet:
	//         path: /q/health/live
	//         port: 8080
	//     readinessProbe:
	//       httpGet:
	//         path: /q/health/ready
	//         port: 8080
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// PodTemplate configures default pod-level settings for workflow runtimes.
	// Individual runtimes can override via logicflowruntime.spec.podTemplate.
	//
	// Use this to set common scheduling constraints, security contexts, volumes,
	// sidecars, and other pod-level configuration that should apply by default.
	//
	// Example:
	//   podTemplate:
	//     metadata:
	//       labels:
	//         monitoring: enabled
	//     affinity:
	//       podAntiAffinity:
	//         preferredDuringSchedulingIgnoredDuringExecution:
	//         - weight: 100
	//           podAffinityTerm:
	//             topologyKey: kubernetes.io/hostname
	//     tolerations:
	//     - key: workload
	//       operator: Equal
	//       value: workflows
	//       effect: NoSchedule
	// +optional
	PodTemplate PodTemplateSpec `json:"podTemplate,omitempty"`
}
