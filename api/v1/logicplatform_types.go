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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LogicPlatformStatusPhase string

// LogicPlatformSpec defines the desired state of LogicPlatform.
//
// LogicPlatform is the top-level resource that manages platform-wide services
// and default configurations for Logic flows. It provides:
//   - Data Index service for workflow indexing and queries
//   - Default runtime configuration for all LogicFlowRuntime deployments
//   - Service-specific persistence configuration (isolated per service)
//
// Example:
//
//	apiVersion: logic.kubesmarts.org/v1
//	kind: LogicPlatform
//	metadata:
//	  name: production-platform
//	spec:
//	  dataIndex:
//	    enabled: true
//	    service:
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
	RuntimeDefaults RuntimeDefaultsSpec `json:"runtimeDefaults,omitempty"`
	Version         string              `json:"version,omitempty"`
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
	// DeploymentRef references the Data Index Deployment resource.
	// +optional
	DeploymentRef ObjectReference `json:"deploymentRef,omitempty"`
	// ServiceRef references the Data Index Service resource.
	// +optional
	ServiceRef ObjectReference `json:"serviceRef,omitempty"`
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
	DaemonSetRef ObjectReference `json:"daemonSetRef,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	// This points to the Fluent Bit Service endpoint.
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
//   - Service-specific persistence configuration to prevent schema conflicts
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
