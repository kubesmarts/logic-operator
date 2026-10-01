package v1

import corev1 "k8s.io/api/core/v1"

// ApplicationSpec defines common deployment configuration for operator-managed applications.
//
// Configuration precedence: podTemplate.container > container > top-level shortcuts
type ApplicationSpec struct {
	// Image specifies the container image.
	// +optional
	Image string `json:"image,omitempty"`

	// ImagePullPolicy specifies when to pull the image.
	// +optional
	// +kubebuilder:default=IfNotPresent
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Replicas is the desired number of pod replicas.
	// When omitted, defaults to 1. Leave unset to allow HorizontalPodAutoscaler to manage scaling.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources specifies compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// PodTemplate allows full pod customization.
	// +optional
	PodTemplate PodTemplateSpec `json:"podTemplate,omitempty"`

	// Container configures the application container.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`
}
