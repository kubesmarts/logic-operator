package v1

import corev1 "k8s.io/api/core/v1"

// DataIndexSpec configures the Data Index service for the Logic Platform.
//
// The Data Index service provides indexing and query capabilities for workflow instances,
// storing workflow execution state and metadata in a PostgreSQL database.
//
// Example:
//
//	dataIndex:
//	  enabled: true
//	  service:
//	    replicas: 2
//	    image: quay.io/kubesmarts/data-index:2.0
//	  persistence:
//	    postgresql:
//	      secretRef:
//	        name: postgres-credentials
//	      serviceRef:
//	        name: postgres
//	        databaseSchema: data-index
//	  fluentBit:
//	    image: fluent/fluent-bit:2.0
type DataIndexSpec struct {
	// Enabled determines whether to deploy the Data Index service.
	// When false, the operator will not create Data Index resources.
	// Defaults to true.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`
	// Service configures the Data Index service deployment.
	// +optional
	Service DataIndexService `json:"service,omitempty"`
	// Persistence configures database connectivity for the Data Index service.
	// If not specified, inherits from logicplatform.spec.persistence.
	// +optional
	Persistence PersistenceOptionsSpec `json:"persistence,omitempty"`
	// FluentBit configures the Fluent Bit DaemonSet for log forwarding.
	// When configured, the operator will deploy a Fluent Bit DaemonSet in the namespace
	// to collect and forward logs from all pods to Data Index.
	// +optional
	FluentBit *FluentBitSpec `json:"fluentBit,omitempty"`
}

// DataIndexService configures the Data Index service deployment.
//
// Configuration precedence (highest to lowest):
//  1. podTemplate.container fields (most specific)
//  2. container field
//  3. Top-level replicas/resources/image fields (convenience shortcuts)
//
// The top-level replicas, resources, and image fields are convenience shortcuts
// that will be overridden if the same fields are set in container or podTemplate.container.
//
// Example (using shortcuts):
//
//	service:
//	  replicas: 2
//	  image: quay.io/kubesmarts/data-index:2.0
//	  resources:
//	    requests:
//	      memory: 512Mi
//	      cpu: 250m
//
// Example (using container for more control):
//
//	service:
//	  container:
//	    image: quay.io/kubesmarts/data-index:2.0
//	    resources:
//	      requests:
//	        memory: 512Mi
//	    env:
//	    - name: QUARKUS_PROFILE
//	      value: prod
//	  podTemplate:
//	    affinity:
//	      podAntiAffinity: ...
type DataIndexService struct {
	// Replicas is the desired number of Data Index pod replicas.
	// This is a convenience field - if podTemplate.replicas is set, it takes precedence.
	// Defaults to 1.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`
	// Resources specifies compute resource requirements for the Data Index container.
	// This is a convenience field - if container.resources or podTemplate.container.resources
	// is set, those take precedence.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Image specifies the Data Index container image.
	// This is a convenience field - if container.image or podTemplate.container.image
	// is set, those take precedence.
	//
	// Example: quay.io/kubesmarts/data-index:2.0.0
	// +optional
	Image string `json:"image,omitempty"`
	// Container configures the Data Index application container.
	// Settings here override the top-level replicas/resources/image fields.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`
	// PodTemplate allows full customization of the Data Index pod.
	// This provides the most control, overriding all other fields.
	// +optional
	PodTemplate PodTemplateSpec `json:"podTemplate,omitempty"`
}

// FluentBitSpec configures the Fluent Bit log forwarding DaemonSet.
//
// Fluent Bit is deployed as a DaemonSet (one pod per node) to collect logs
// from all pods in the namespace and forward them to external systems
// (e.g., Elasticsearch, CloudWatch, Kafka, Loki).
//
// The DaemonSet runs on every node and mounts the node's /var/log directory
// to collect container logs. It can be configured to filter, transform, and
// route logs based on pod labels and namespaces.
//
// Configuration precedence:
//  1. container fields (most specific)
//  2. Top-level image/resources fields (convenience shortcuts)
//
// Example (using shortcuts):
//
//	fluentBit:
//	  image: fluent/fluent-bit:2.0
//	  resources:
//	    requests:
//	      memory: 128Mi
//	      cpu: 100m
//
// Example (using container for full control):
//
//	fluentBit:
//	  container:
//	    image: fluent/fluent-bit:2.0
//	    resources:
//	      requests:
//	        memory: 128Mi
//	    volumeMounts:
//	    - name: fluent-bit-config
//	      mountPath: /fluent-bit/etc/
//	    env:
//	    - name: FLB_OUTPUT
//	      value: elasticsearch
//
// Note: The Replicas field in PodTemplateSpec is ignored for DaemonSet workloads,
// as DaemonSets automatically run one pod per node.
type FluentBitSpec struct {
	// Container configures the Fluent Bit DaemonSet container.
	// Use this for full control over the Fluent Bit configuration.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`
	// Image specifies the Fluent Bit container image.
	// This is a convenience field - if container.image is set, it takes precedence.
	//
	// Example: fluent/fluent-bit:2.0.8
	// +optional
	Image string `json:"image,omitempty"`
	// Resources specifies compute resource requirements for the Fluent Bit DaemonSet pods.
	// This is a convenience field - if container.resources is set, it takes precedence.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}
