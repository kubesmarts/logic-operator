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

// DataIndexSpec configures the Data Index service; when nil, Data Index is not deployed.
type DataIndexSpec struct {
	// Application configures the Data Index deployment.
	// +optional
	Application ApplicationSpec `json:"application,omitempty"`

	// Persistence configures the PostgreSQL datastore; use a schema separate from runtimes.
	// +optional
	Persistence *DatastoreSpec `json:"persistence,omitempty"`

	// Vector configures the Vector DaemonSet for log forwarding.
	// +optional
	Vector *VectorSpec `json:"vector,omitempty"`

	// Ingress exposes the Data Index GraphQL API via Ingress (Kubernetes) or Route (OpenShift).
	// +optional
	Ingress *DataIndexIngressSpec `json:"ingress,omitempty"`
}

// VectorSpec configures the Vector DaemonSet for log forwarding; when nil, Vector is not deployed.
type VectorSpec struct {
	// WatchNamespaces sets the namespace to tail logs from; defaults to targetNamespace.
	// Only the first entry is used; wildcards are not supported.
	// +optional
	WatchNamespaces []string `json:"watchNamespaces,omitempty"`
	// DebugEvents logs all workflow events to Vector's stdout.
	// WARNING: high log volume; enable only for troubleshooting.
	// +optional
	// +kubebuilder:default=false
	DebugEvents bool `json:"debugEvents,omitempty"`
	// Application configures the Vector deployment.
	Application ApplicationSpec `json:"application,omitempty"`
}

// DataIndexIngressSpec configures external access to the Data Index service.
type DataIndexIngressSpec struct {
	// Enabled creates an Ingress/Route; requires Host.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the external hostname; required when Enabled is true.
	// +optional
	Host string `json:"host,omitempty"`

	// IngressClassName selects the Ingress controller (Kubernetes only); defaults to the cluster default.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// Annotations for the Ingress/Route resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS configures HTTPS/TLS termination.
	// +optional
	TLS TLSSpec `json:"tls,omitempty"`
}

// WorkflowGatewaySpec configures the optional workflow-gateway component for routing workflows.
type WorkflowGatewaySpec struct {
	// Application configures the gateway Deployment.
	// +optional
	Application ApplicationSpec `json:"application,omitempty"`

	// Ingress configures optional external access to the gateway's user API.
	// +optional
	Ingress *DataIndexIngressSpec `json:"ingress,omitempty"`
}

// RuntimePlatformSpec contains platform-level settings used by runtimes.
type RuntimePlatformSpec struct {
	// Persistence configures separate workflow runtime and Quartz datastores.
	// +optional
	Persistence *RuntimePersistenceSpec `json:"persistence,omitempty"`
}

// DataIndexStatus contains status information for the Data Index component.
type DataIndexStatus struct {
	// Ready indicates whether the enabled Data Index component is ready to serve requests.
	Ready bool `json:"ready"`

	// Service contains status for the Data Index service deployment.
	// +optional
	Service DataIndexServiceStatus `json:"service,omitempty"`
	// Persistence reports validation of the Data Index persistence configuration.
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
	// +optional
	Replicas ReplicaStatus `json:"replicas,omitempty"`
	// GraphQLEndpoint is the internal cluster URL for the Data Index GraphQL API.
	// +optional
	GraphQLEndpoint string `json:"graphqlEndpoint,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Prometheus metrics.
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
	// URL is the external Data Index URL; set when spec.dataIndex.ingress is enabled.
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
	// +optional
	Updated int32 `json:"updated,omitempty"`
}

// VectorStatus contains status information for the Vector DaemonSet.
type VectorStatus struct {
	// Ready indicates whether the Vector DaemonSet is ready.
	Ready bool `json:"ready"`
	// DaemonSetRef references the Vector DaemonSet resource.
	// +optional
	DaemonSetRef corev1.LocalObjectReference `json:"daemonSetRef,omitempty"`
	// MetricsEndpoint is the internal cluster URL for Vector's Prometheus metrics.
	// +optional
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`
}

// PersistenceConfigStatus reports persistence reference validation and, when available,
// database connectivity observed from Data Index readiness or a direct connection check.
type PersistenceConfigStatus struct {
	// Valid indicates whether the referenced secrets and services exist.
	Valid bool `json:"valid"`
	// SecretExists indicates whether the referenced secret exists.
	SecretExists bool `json:"secretExists"`
	// ServiceExists indicates whether the referenced service exists.
	// +optional
	ServiceExists bool `json:"serviceExists,omitempty"`
	// DatabaseConnected indicates whether the database is reachable.
	// +optional
	DatabaseConnected *bool `json:"databaseConnected,omitempty"`
	// Error is the validation error message when Valid is false.
	// +optional
	Error string `json:"error,omitempty"`
}

// WorkflowGatewayStatus reports gateway availability and child resources.
type WorkflowGatewayStatus struct {
	// Ready indicates whether the gateway has a ready replica and its Service exists.
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

	// URL is the external gateway URL; set when spec.gateway.ingress is enabled.
	// +optional
	URL string `json:"url,omitempty"`
}
