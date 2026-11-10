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
)

// DataIndexSpec configures the Data Index service for the Logic Platform.
// When DataIndex is nil, the Data Index service is not deployed.
type DataIndexSpec struct {
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
	Persistence *DatastoreSpec `json:"persistence,omitempty"`

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

// VectorSpec configures the Vector DaemonSet for log forwarding.
// When Vector is nil, the Vector DaemonSet is not deployed.
type VectorSpec struct {
	// WatchNamespaces specifies the namespace Vector should tail logs from.
	// When omitted, the ClusterLogicPlatform targetNamespace is used.
	// The current controller passes only the first entry as an exact namespace
	// filter; wildcard and multi-namespace filtering are not supported.
	//
	// +optional
	WatchNamespaces []string `json:"watchNamespaces,omitempty"`
	// DebugEvents enables debug logging of all workflow events to Vector's stdout.
	// At 1000 events/sec, generates ~43 GB/day of logs.
	// WARNING: Only enable for troubleshooting; disable after debugging.
	// Defaults to false.
	// +optional
	// +kubebuilder:default=false
	DebugEvents bool `json:"debugEvents,omitempty"`
	// Application configures the Vector deployment.
	Application ApplicationSpec `json:"application,omitempty"`
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

// WorkflowGatewaySpec configures the optional workflow-gateway component for routing workflows.
type WorkflowGatewaySpec struct {
	// Application configures the gateway Deployment.
	// Image, replicas, resources and pod/container customization follow ApplicationSpec.
	// +optional
	Application ApplicationSpec `json:"application,omitempty"`

	// Ingress configures optional external access to the gateway's user API.
	// Uses the same pattern as DataIndexIngressSpec: host required when enabled.
	// +optional
	Ingress *DataIndexIngressSpec `json:"ingress,omitempty"`
}

// DataIndexStatus contains status information for the Data Index component.
type DataIndexStatus struct {
	// Ready indicates whether the enabled Data Index component is ready to serve requests.
	Ready bool `json:"ready"`

	// Service contains status for the Data Index service deployment.
	// +optional
	Service DataIndexServiceStatus `json:"service,omitempty"`
	// Persistence contains status for the Data Index persistence configuration validation.
	// The controller checks referenced resources and reports database connectivity when it can
	// infer it from Data Index readiness or run a direct connection check.
	// +optional
	Persistence *PersistenceConfigStatus `json:"persistence,omitempty"`
}

// DataIndexServiceStatus contains status information for the Data Index service deployment.
type DataIndexServiceStatus struct {
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
	// Example: http://data-index.default.svc.cluster.local:80/graphql
	// +optional
	GraphQLEndpoint string `json:"graphqlEndpoint,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	//
	// Example: http://data-index.default.svc.cluster.local:80/q/metrics
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

// PersistenceConfigStatus reports persistence reference validation and, when available,
// database connectivity observed from Data Index readiness or a direct connection check.
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
	// DatabaseConnected indicates whether the database is actually reachable.
	// Checked via DataIndex readiness probe (when pod is ready) or direct connection test (when pod is starting).
	// +optional
	DatabaseConnected *bool `json:"databaseConnected,omitempty"`
	// Error contains any configuration validation error message.
	// This field is populated when Valid=false.
	// +optional
	Error string `json:"error,omitempty"`
}

// WorkflowGatewayStatus reports gateway availability and child resources.
type WorkflowGatewayStatus struct {
	// Ready indicates whether the gateway Deployment has at least one ready replica
	// and the expected Service exists.
	Ready bool `json:"ready"`

	// DeploymentRef references the gateway Deployment.
	// +optional
	DeploymentRef corev1.LocalObjectReference `json:"deploymentRef,omitempty"`

	// ServiceRef references the gateway Service.
	// +optional
	ServiceRef corev1.LocalObjectReference `json:"serviceRef,omitempty"`

	// ServiceAccountRef references the gateway ServiceAccount.
	// +optional
	ServiceAccountRef corev1.LocalObjectReference `json:"serviceAccountRef,omitempty"`

	// IngressRef references the Kubernetes Ingress (populated on Kubernetes only).
	// +optional
	IngressRef *corev1.LocalObjectReference `json:"ingressRef,omitempty"`

	// RouteRef references the OpenShift Route (populated on OpenShift only).
	// +optional
	RouteRef *corev1.LocalObjectReference `json:"routeRef,omitempty"`

	// Replicas shows the current replica counts for the gateway Deployment.
	// +optional
	Replicas ReplicaStatus `json:"replicas,omitempty"`

	// URL is the external URL to access the gateway (via Ingress/Route).
	// Populated only when spec.workflowGateway.ingress.enabled is true and hostname is assigned.
	// +optional
	URL string `json:"url,omitempty"`
}
