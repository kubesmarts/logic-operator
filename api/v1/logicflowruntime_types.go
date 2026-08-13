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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const LogicFlowRuntimeKind = "LogicFlowRuntime"

// LogicFlowRuntimeSpec defines the desired state of LogicFlowRuntime.
//
// Shared Quarkus Flow runner that executes multiple workflow definitions.
// Configuration precedence: LogicFlowRuntime.spec > LogicPlatform.spec.runtimeDefaults > operator defaults
type LogicFlowRuntimeSpec struct {
	RuntimeSpec `json:",inline"`
	Security    RuntimeSecuritySpec `json:"security,omitempty"`
}

// LogicFlowRuntimeStatus defines the observed state of LogicFlowRuntime.
type LogicFlowRuntimeStatus struct {
	// ObservedGeneration tracks the last reconciled spec generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the runtime lifecycle phase, derived from Conditions.
	// +optional
	Phase ApplicationPhase `json:"phase,omitempty"`

	// Replicas is the total number of replicas (for HPA scale subresource).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Selector is the label selector for pods (for HPA scale subresource).
	// +optional
	Selector string `json:"selector,omitempty"`

	// ReadyReplicas is the number of ready replicas.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Definitions lists loaded workflow definitions.
	// +optional
	Definitions []RuntimeDefinitionStatus `json:"definitions,omitempty"`

	// DeploymentRef references the managed Deployment.
	// +optional
	DeploymentRef v1.LocalObjectReference `json:"deploymentRef,omitempty"`

	// ServiceRef references the HTTP Service.
	// +optional
	ServiceRef v1.LocalObjectReference `json:"serviceRef,omitempty"`

	// ConfigMapRefs lists source ConfigMaps for loaded workflows.
	// +optional
	ConfigMapRefs []v1.LocalObjectReference `json:"configMapRefs,omitempty"`

	// LeaseReplicas is the number of durable pool leases.
	// +optional
	LeaseReplicas int32 `json:"leaseReplicas,omitempty"`

	// Conditions represent detailed runtime state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RuntimeDefinitionStatus represents a workflow definition loaded by the runtime.
type RuntimeDefinitionStatus struct {
	// Name of the workflow.
	Name string `json:"name"`

	// Version of the workflow.
	// +optional
	Version string `json:"version,omitempty"`
}

// LogicFlowRuntime is a shared Quarkus Flow runner deployment that executes multiple workflow definitions.
//
// Architecture: 1 Runtime = N Workflow Definitions (shared runner model)
// Workflows are loaded from ConfigMaps or embedded in the runtime image.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:scope=Namespaced,shortName={lfr,runtime}
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LogicFlowRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicFlowRuntimeSpec   `json:"spec,omitempty"`
	Status LogicFlowRuntimeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogicFlowRuntimeList contains a list of LogicFlowRuntime.
type LogicFlowRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicFlowRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogicFlowRuntime{}, &LogicFlowRuntimeList{})
}

// RuntimeSecurityType defines the security mode for workflow runtime HTTP endpoints.
// +kubebuilder:validation:Enum=NONE;API_KEY;OIDC
type RuntimeSecurityType string

const (
	// RuntimeSecurityNone - No authentication required (development/testing only)
	RuntimeSecurityNone RuntimeSecurityType = "NONE"
	// RuntimeSecurityAPIKey - API key-based authentication
	RuntimeSecurityAPIKey RuntimeSecurityType = "API_KEY"
	// RuntimeSecurityOIDC - OpenID Connect authentication
	RuntimeSecurityOIDC RuntimeSecurityType = "OIDC"
)

// RuntimeSecurityRole defines predefined security roles for workflow runtime access.
// +kubebuilder:validation:Enum=flow-admin;flow-invoker
type RuntimeSecurityRole string

const (
	// RuntimeSecurityRoleAdmin - Full access to all workflow operations
	RuntimeSecurityRoleAdmin RuntimeSecurityRole = "flow-admin"
	// RuntimeSecurityRoleInvoker - Execute workflows only (read-only on definitions)
	RuntimeSecurityRoleInvoker RuntimeSecurityRole = "flow-invoker"
)

// RuntimeSecuritySpec configures authentication for workflow runtime HTTP endpoints.
//
// Modes: NONE (dev only), API_KEY (machine-to-machine), OIDC (enterprise SSO)
// Roles: flow-admin (full access), flow-invoker (execute only)
// See: https://docs.quarkiverse.io/quarkus-flow/dev/runner.html#_security_in_depth
type RuntimeSecuritySpec struct {
	// Type specifies the authentication mode.
	// WARNING: NONE mode should only be used in development.
	// +optional
	// +kubebuilder:default=NONE
	// +kubebuilder:validation:Enum=NONE;API_KEY;OIDC
	Type RuntimeSecurityType `json:"type,omitempty"`

	// APIKey configures API key authentication.
	// +optional
	APIKey *APIKeyAuthSpec `json:"apiKey,omitempty"`

	// OIDC configures OpenID Connect authentication.
	// +optional
	OIDC *OIDCAuthSpec `json:"oidc,omitempty"`
}

// APIKeyAuthSpec configures API key-based authentication.
type APIKeyAuthSpec struct {
	// Keys is a list of API keys with roles.
	// +required
	// +kubebuilder:validation:MinItems=1
	Keys []APIKeySpec `json:"keys"`
}

// APIKeySpec defines a single API key configuration.
type APIKeySpec struct {
	// Name is a unique identifier for this API key.
	// +required
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// SecretRef points to a Secret containing the API key value.
	// +required
	SecretRef SecretKeySelector `json:"secretRef"`

	// Roles assigned to this API key.
	// +required
	// +kubebuilder:validation:MinItems=1
	Roles []RuntimeSecurityRole `json:"roles"`
}

// OIDCAuthSpec configures OpenID Connect authentication.
type OIDCAuthSpec struct {
	// AuthServerURL is the OIDC provider URL.
	// +required
	AuthServerURL string `json:"authServerUrl"`

	// ClientID is the OAuth2 client ID.
	// +required
	ClientID string `json:"clientId"`

	// ClientSecret references the OAuth2 client secret.
	// +required
	ClientSecret SecretKeySelector `json:"clientSecret"`

	// RolesClaim is the JWT claim path containing user roles.
	// +optional
	// +kubebuilder:default=roles
	RolesClaim string `json:"rolesClaim,omitempty"`
}

// SecretKeySelector references a specific key within a Secret.
type SecretKeySelector struct {
	// Name of the Secret.
	// +required
	Name string `json:"name"`

	// Key within the Secret.
	// +optional
	// +kubebuilder:default=value
	Key string `json:"key,omitempty"`
}

// RuntimeSpec defines configuration for LogicFlowRuntime deployments.
type RuntimeSpec struct {
	// Persistence configures database connectivity.
	// Avoid using the same schema as the Data Index.
	// +optional
	Persistence *PersistenceOptionsSpec `json:"persistence,omitempty"`

	// ApplicationSpec embeds deployment configuration (image, replicas, resources, container, podTemplate).
	ApplicationSpec `json:",inline"`
}
