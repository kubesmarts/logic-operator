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
	"github.com/kubesmarts/logic-operator/api"
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
	// and Fluent Bit DaemonSet status (if configured).
	// +optional
	DataIndex DataIndexStatus `json:"dataIndex,omitempty"`
	// Conditions represent the latest available observations of the platform's state.
	// Standard condition types:
	//   - Ready: All platform services are ready and operational
	//   - DataIndexReady: Data Index service is ready to serve requests
	//   - PersistenceReady: Database connections are established and schemas are ready
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []api.Condition          `json:"conditions,omitempty"`
	Phase      LogicPlatformStatusPhase `json:"phase,omitempty"`
}

// DataIndexStatus contains status information for the Data Index service.
type DataIndexStatus struct {
	// Service contains status for the Data Index service deployment.
	// +optional
	Service DataIndexServiceStatus `json:"service,omitempty"`
	// Persistence contains status for the Data Index database connection.
	// +optional
	Persistence PostgresSQLStatus `json:"persistence,omitempty"`
	// FluentBit contains status for the Fluent Bit DaemonSet (if enabled).
	// This field is only populated when spec.dataIndex.fluentBit is configured.
	// +optional
	FluentBit *FluentBitStatus `json:"fluentBit,omitempty"`
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

// FluentBitStatus contains status information for the Fluent Bit DaemonSet.
//
// There is a 1:1 relationship between LogicPlatform and the Fluent Bit DaemonSet.
// One DaemonSet per platform coordinates all event inflow from Data Index and runtimes.
type FluentBitStatus struct {
	// Ready indicates whether the Fluent Bit DaemonSet is ready.
	// This is true when the DaemonSet is successfully deployed and operational.
	Ready bool `json:"ready"`
	// DaemonSetRef references the Fluent Bit DaemonSet resource.
	// +optional
	DaemonSetRef corev1.LocalObjectReference `json:"daemonSetRef,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	// This points to the Fluent Bit Application endpoint.
	//
	// Example: http://fluent-bit.default.svc.cluster.local:2020/api/v1/metrics/prometheus
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
}

// PostgresSQLStatus contains status information for a PostgreSQL database connection.
type PostgresSQLStatus struct {
	// Connected indicates whether the database connection is established.
	// The operator tests connectivity during reconciliation.
	Connected bool `json:"connected"`
	// SchemaReady indicates whether the database schema is initialized and up-to-date.
	// This is true after successful schema migrations.
	SchemaReady bool `json:"schemaReady"`
	// SchemaVersion is the current schema version in the database.
	// This corresponds to the migration version number.
	// +optional
	SchemaVersion string `json:"schemaVersion,omitempty"`
	// Error contains any database connection or schema error message.
	// This field is populated when Connected=false or SchemaReady=false.
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
//
// Example (minimal configuration):
//
//	dataIndex:
//	  enabled: true
//	  application:
//	    image: quay.io/kubesmarts/data-index:2.0.0
//	    replicas: 2
//	  persistence:
//	    postgresql:
//	      secretRef:
//	        name: postgres-credentials
//	      serviceRef:
//	        name: postgres
//	        databaseSchema: data-index
//
// Example (with FluentBit log forwarding):
//
//	dataIndex:
//	  enabled: true
//	  application:
//	    image: quay.io/kubesmarts/data-index:2.0.0
//	    replicas: 2
//	    resources:
//	      requests:
//	        memory: 512Mi
//	        cpu: 250m
//	  persistence:
//	    postgresql:
//	      secretRef:
//	        name: postgres-credentials
//	      serviceRef:
//	        name: postgres
//	        databaseSchema: data-index
//	  fluentBit:
//	    image: fluent/fluent-bit:2.0
//	    resources:
//	      requests:
//	        memory: 128Mi
//	        cpu: 100m
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
	Persistence PersistenceOptionsSpec `json:"persistence,omitempty"`

	// FluentBit configures the Fluent Bit DaemonSet for log forwarding.
	//
	// When configured, the operator will deploy a Fluent Bit DaemonSet in the namespace
	// to collect and forward logs from all pods (Data Index and workflow runtimes) to
	// external log aggregation systems.
	//
	// The DaemonSet is deployed 1:1 per LogicPlatform (one DaemonSet coordinates all
	// event inflow for the platform).
	//
	// Example:
	//   fluentBit:
	//     image: fluent/fluent-bit:2.0
	//     resources:
	//       requests:
	//         memory: 128Mi
	//         cpu: 100m
	// +optional
	FluentBit *FluentBitSpec `json:"fluentBit,omitempty"`
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
	Image           string            `json:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// Resources specifies compute resource requirements for the Fluent Bit DaemonSet pods.
	// This is a convenience field - if container.resources is set, it takes precedence.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}
