/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LogicPlatformStatusPhase string

// LogicPlatformSpec defines the desired state of LogicPlatform.
//
// LogicPlatform is the top-level resource that manages platform-wide services
// and default configurations for Logic flows. It provides:
//   - Data Index service for workflow indexing and queries
//   - Default runtime configuration for all LogicFlowRuntime deployments
//   - Application-specific persistence configuration (isolated per service)
//
// Example:
//
//	apiVersion: logic.kubesmarts.org/v1
//	kind: LogicPlatform
//	metadata:
//	  name: production-platform
//	spec:
//	  version: "2.0.0"
//	  dataIndex:
//	    enabled: true
//	    application:
//	      image: quay.io/kubesmarts/data-index:2.0.0
//	      replicas: 2
//	    persistence:
//	      postgresql:
//	        secretRef:
//	          name: postgres-credentials
//	        serviceRef:
//	          name: postgres
//	          databaseSchema: data-index
//	  runtimeDefaults:
//	    image: quay.io/kubesmarts/quarkus-flow:2.0.0
//	    replicas: 2
//	    persistence:
//	      postgresql:
//	        secretRef:
//	          name: postgres-credentials
//	        serviceRef:
//	          name: postgres
//	          databaseSchema: workflows
type LogicPlatformSpec struct {
	// DataIndex configures the Data Index service for workflow indexing.
	// The Data Index service provides GraphQL and REST APIs to query
	// workflow instances, process definitions, and execution history.
	// +optional
	DataIndex DataIndexSpec `json:"dataIndex,omitempty"`
	// RuntimeDefaults defines default configuration for all LogicFlowRuntime
	// deployments in this namespace. Individual LogicFlowRuntime resources
	// can override these defaults.
	//
	// This provides centralized governance for runtime deployments, ensuring
	// consistent configuration across all workflows while still allowing
	// per-workflow customization when needed.
	//
	// Common use cases:
	//   - Set default runtime image version
	//   - Configure default resource requests/limits
	//   - Set default replica count
	//   - Configure default scheduling constraints
	//   - Configure default persistence (database schema for all runtimes)
	//
	// Example:
	//   runtimeDefaults:
	//     image: quay.io/kubesmarts/quarkus-flow:2.0.0
	//     replicas: 2
	//     resources:
	//       requests:
	//         memory: 512Mi
	//     persistence:
	//       postgresql:
	//         secretRef:
	//           name: postgres-credentials
	//         serviceRef:
	//           name: postgres
	//           databaseSchema: workflows
	// +optional
	RuntimeDefaults RuntimeSpec `json:"runtimeDefaults,omitempty"`
	Version         string      `json:"version,omitempty"`
}

// LogicPlatformStatus defines the observed state of LogicPlatform.
type LogicPlatformStatus struct {
	// ObservedGeneration is the generation of the spec that was last reconciled.
	// This prevents stale status updates when multiple reconciliations happen concurrently.
	// The controller updates this field after successfully reconciling the spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// DataIndex contains status for the Data Index service.
	// This includes the service deployment status, database connectivity,
	// and Vector DaemonSet status (if configured).
	// +optional
	DataIndex DataIndexStatus `json:"dataIndex,omitempty"`
	// IngressRef references the Ingress resource (Kubernetes).
	// Populated when spec.dataIndex.ingress is enabled on Kubernetes.
	// +optional
	IngressRef *corev1.LocalObjectReference `json:"ingressRef,omitempty"`
	// RouteRef references the Route resource (OpenShift).
	// Populated when spec.dataIndex.ingress is enabled on OpenShift.
	// +optional
	RouteRef *corev1.LocalObjectReference `json:"routeRef,omitempty"`
	// Conditions represent the latest available observations of the platform's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition       `json:"conditions,omitempty"`
	Phase      LogicPlatformStatusPhase `json:"phase,omitempty"`
}

// DataIndexStatus contains status information for the Data Index service.
type DataIndexStatus struct {
	// Service contains status for the Data Index service deployment.
	// +optional
	Service DataIndexServiceStatus `json:"service,omitempty"`
	// Persistence contains status for the Data Index persistence configuration validation.
	// This validates that secrets and service refs exist, not runtime database connectivity.
	// +optional
	Persistence *PersistenceConfigStatus `json:"persistence,omitempty"`
	// Vector contains status for the Vector DaemonSet (if enabled).
	// This field is only populated when spec.dataIndex.vector is configured.
	// +optional
	Vector *VectorStatus `json:"vector,omitempty"`
}

// DataIndexServiceStatus contains status information for the Data Index service deployment.
type DataIndexServiceStatus struct {
	// Ready indicates whether the Data Index service is ready to serve requests.
	// This is true when the deployment has at least one ready replica.
	Ready bool `json:"ready"`
	// DeploymentRef references the Data Index Application resource.
	// +optional
	DeploymentRef corev1.LocalObjectReference `json:"deploymentRef,omitempty"`
	// ServiceRef references the Data Index service resource.
	// +optional
	ServiceRef corev1.LocalObjectReference `json:"serviceRef,omitempty"`
	// Replicas shows the current replica counts for the Data Index deployment.
	// This helps users understand the deployment's health and rollout status.
	// +optional
	Replicas ReplicaStatus `json:"replicas,omitempty"`
	// GraphQLEndpoint is the internal cluster URL for the Data Index GraphQL API.
	// Users can use this endpoint to query workflow instances and process definitions.
	//
	// Example: http://data-index.default.svc.cluster.local:8080/graphql
	// +optional
	GraphQLEndpoint string `json:"graphqlEndpoint,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	//
	// Example: http://data-index.default.svc.cluster.local:8080/q/metrics
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
	// URL is the external URL to access Data Index (via Ingress/Route).
	// Populated when spec.dataIndex.ingress is enabled.
	//
	// Example: https://data-index.example.com
	// +optional
	URL string `json:"url,omitempty"`
}

// ReplicaStatus shows the current state of replicas for a deployment.
type ReplicaStatus struct {
	// Desired is the desired number of replicas specified in the spec.
	Desired int32 `json:"desired"`
	// Current is the current total number of replicas (ready + not ready).
	Current int32 `json:"current"`
	// Ready is the number of replicas that are ready to serve requests.
	Ready int32 `json:"ready"`
	// Updated is the number of replicas that have the latest pod template.
	// During a rolling update, this will be less than Current.
	// +optional
	Updated int32 `json:"updated,omitempty"`
}

// VectorStatus contains status information for the Vector DaemonSet.
//
// There is a 1:1 relationship between LogicPlatform and the Vector DaemonSet.
// One DaemonSet per platform coordinates all event inflow from Data Index and runtimes.
type VectorStatus struct {
	// Ready indicates whether the Vector DaemonSet is ready.
	// This is true when the DaemonSet is successfully deployed and operational.
	Ready bool `json:"ready"`
	// DaemonSetRef references the Vector DaemonSet resource.
	// +optional
	DaemonSetRef corev1.LocalObjectReference `json:"daemonSetRef,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	// This points to the Vector Application endpoint.
	//
	// Example: http://vector.default.svc.cluster.local:2020/api/v1/metrics/prometheus
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
}

// PersistenceConfigStatus validates persistence configuration without testing runtime connectivity.
// The operator checks that referenced secrets and services exist, but does not connect to the database.
// Actual database connectivity is verified by the application's health checks.
type PersistenceConfigStatus struct {
	// Valid indicates whether the persistence configuration is valid.
	// This checks that referenced secrets and service refs exist in the cluster.
	Valid bool `json:"valid"`
	// SecretExists indicates whether the referenced secret exists.
	SecretExists bool `json:"secretExists"`
	// ServiceExists indicates whether the referenced service exists (if using serviceRef).
	// Only populated when using serviceRef instead of JDBC URL.
	// +optional
	ServiceExists bool `json:"serviceExists,omitempty"`
	// Error contains any configuration validation error message.
	// This field is populated when Valid=false.
	// +optional
	Error string `json:"error,omitempty"`
}

// LogicPlatform is the top-level resource for managing the Logic workflow platform.
//
// It provides:
//   - Centralized configuration for platform services (Data Index)
//   - Default runtime configuration (image, resources, persistence) for all LogicFlowRuntime deployments
//   - Application-specific persistence configuration to prevent schema conflicts
//   - Platform-wide service mesh and observability integration
//
// Note: Each service (Data Index, Runtimes) configures its own database schema
// to prevent table name collisions and ensure independent schema migrations.
//
// Only one LogicPlatform should exist per namespace. The operator uses this
// resource to deploy and manage shared platform services.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName={lp,platform}
// +kubebuilder:printcolumn:name="Data Index",type=boolean,JSONPath=`.spec.dataIndex.enabled`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LogicPlatform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicPlatformSpec   `json:"spec,omitempty"`
	Status LogicPlatformStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogicPlatformList contains a list of LogicPlatform.
type LogicPlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicPlatform `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogicPlatform{}, &LogicPlatformList{})
}

// DataIndexSpec configures the Data Index service for the Logic Platform.
//
// The Data Index service provides indexing and query capabilities for workflow instances,
// storing workflow execution state and metadata in a PostgreSQL database. It exposes
// GraphQL and REST APIs for querying workflow instances, process definitions, and
// execution history.
//
// The Data Index service is optional but recommended for production deployments as it
// enables powerful querying capabilities and workflow observability.
type DataIndexSpec struct {
	// Enabled determines whether to deploy the Data Index service.
	// When false, the operator will not create any Data Index resources.
	//
	// Disabling Data Index means no workflow querying capabilities, but workflows
	// can still execute normally. Enable this for production environments where
	// workflow observability and querying are required.
	//
	// Defaults to true.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Application configures the Data Index service deployment.
	//
	// This controls the Data Index application container image, replicas,
	// resource limits, and pod/container customization.
	//
	// Example:
	//   application:
	//     image: quay.io/kubesmarts/data-index:2.0.0
	//     replicas: 2
	//     resources:
	//       requests:
	//         memory: 512Mi
	//         cpu: 250m
	// +optional
	Application ApplicationSpec `json:"application,omitempty"`

	// Persistence configures database connectivity for the Data Index service.
	//
	// Data Index requires PostgreSQL to store workflow instance data, process
	// definitions, and execution history. The database schema should be isolated
	// from other services (use a different databaseSchema than runtimes).
	//
	// Example:
	//   persistence:
	//     postgresql:
	//       secretRef:
	//         name: postgres-credentials
	//       serviceRef:
	//         name: postgres
	//         databaseSchema: data-index
	// +optional
	Persistence *PersistenceOptionsSpec `json:"persistence,omitempty"`

	// Vector configures the Vector DaemonSet for log forwarding.
	//
	// When configured, the operator will deploy a Vector DaemonSet in the namespace
	// to collect and forward logs from all pods (Data Index and workflow runtimes) to
	// external log aggregation systems.
	//
	// The DaemonSet is deployed 1:1 per LogicPlatform (one DaemonSet coordinates all
	// event inflow for the platform).
	//
	// Example:
	//   vector:
	//     image: vector/vector:2.0
	//     resources:
	//       requests:
	//         memory: 128Mi
	//         cpu: 100m
	// +optional
	Vector *VectorSpec `json:"vector,omitempty"`

	// Ingress configures external access to the Data Index GraphQL API.
	//
	// When enabled, the operator creates an Ingress (Kubernetes) or Route (OpenShift)
	// to expose the Data Index service externally. This allows users to query workflow
	// instances from outside the cluster.
	//
	// Example:
	//   ingress:
	//     enabled: true
	//     host: data-index.example.com
	//     tls:
	//       enabled: true
	//       certManager:
	//         issuerRef:
	//           name: letsencrypt-prod
	// +optional
	Ingress *DataIndexIngressSpec `json:"ingress,omitempty"`
}

type VectorSpec struct {
	// Container configures the Vector DaemonSet container.
	// Use this for full control over the Vector configuration.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`
	// Image specifies the Vector container image.
	// This is a convenience field - if container.image is set, it takes precedence.
	// +optional
	Image           string            `json:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// Resources specifies compute resource requirements for the Vector DaemonSet pods.
	// This is a convenience field - if container.resources is set, it takes precedence.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// DataIndexIngressSpec configures external access to the Data Index service.
type DataIndexIngressSpec struct {
	// Enabled determines whether to create Ingress/Route for external access.
	// When true, host must be specified.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the external hostname for accessing Data Index.
	// Required when Enabled is true.
	// Example: data-index.example.com
	// +optional
	Host string `json:"host,omitempty"`

	// IngressClassName selects the Ingress controller (Kubernetes only).
	// If not specified, uses the cluster's default IngressClass.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// Annotations for the Ingress/Route resource.
	// Can be used for additional configuration like rate limiting, CORS, etc.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS configures HTTPS/TLS termination.
	// +optional
	TLS TLSSpec `json:"tls,omitempty"`
}
