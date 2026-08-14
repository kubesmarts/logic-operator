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

const LogicFlowServiceKind = "LogicFlowService"

// LogicFlowServiceSpec defines the desired state of LogicFlowService.
//
// Provides stable external HTTP access to workflows with traffic splitting and TLS.
// Forward reference pattern: Service → Definition (no bidirectional references).
// The runtime is discovered transitively from the referenced definitions.
type LogicFlowServiceSpec struct {
	// DefaultDefinition routes 100% traffic to a single LogicFlowDefinition.
	// Mutually exclusive with Traffic.
	// +optional
	DefaultDefinition *corev1.LocalObjectReference `json:"defaultDefinition,omitempty"`

	// Traffic distributes requests across workflow versions.
	// Mutually exclusive with DefaultDefinition.
	// Weights must sum to 100.
	// +optional
	Traffic []TrafficSpec `json:"traffic,omitempty"`

	// Ingress configures external HTTP access.
	// +required
	Ingress IngressSpec `json:"ingress"`
}

// TotalWeight returns the sum of all traffic weights.
func (l *LogicFlowServiceSpec) TotalWeight() int32 {
	totalWeight := int32(0)
	for _, t := range l.Traffic {
		totalWeight += t.Weight
	}
	return totalWeight
}

// TrafficSpec routes a percentage of traffic to a specific workflow version.
type TrafficSpec struct {
	// DefinitionRef references a LogicFlowDefinition (workflow version).
	// +required
	DefinitionRef corev1.LocalObjectReference `json:"definitionRef"`

	// Weight is the percentage of traffic (0-100).
	// +required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`
}

// IngressSpec configures external HTTP/HTTPS access.
// Creates an Ingress (Kubernetes), Route (OpenShift), or HTTPRoute (Gateway API).
type IngressSpec struct {
	// Host is the external hostname.
	// Required when GatewayRef is not set.
	// +optional
	Host string `json:"host,omitempty"`

	// IngressClassName selects the Ingress controller (Kubernetes Ingress mode).
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`

	// GatewayRef selects the Gateway for HTTPRoute creation (Gateway API mode).
	// When set, an HTTPRoute is created instead of Ingress/Route.
	// Required for traffic splitting on OpenShift.
	// +optional
	GatewayRef *GatewayRef `json:"gatewayRef,omitempty"`

	// Annotations for the Ingress/Route/HTTPRoute resource (user-provided).
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// TLS configures HTTPS.
	// +optional
	TLS TLSSpec `json:"tls,omitempty"`
}

// TLSSpec configures TLS/HTTPS termination.
type TLSSpec struct {
	// Enabled determines whether to use HTTPS.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// SecretRef references an existing TLS Secret.
	// Mutually exclusive with CertManager.
	// +optional
	SecretRef corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// CertManager configures automatic certificate generation.
	// Mutually exclusive with SecretRef.
	// +optional
	CertManager *CertManagerSpec `json:"certManager,omitempty"`
}

// CertManagerSpec configures automatic TLS certificate generation via cert-manager.
type CertManagerSpec struct {
	// IssuerRef references a cert-manager Issuer or ClusterIssuer.
	// +required
	IssuerRef CertManagerIssuerRef `json:"issuerRef"`
}

// CertManagerIssuerRef references a cert-manager Issuer or ClusterIssuer.
// Matches cert-manager's ObjectReference pattern.
type CertManagerIssuerRef struct {
	// Name of the Issuer/ClusterIssuer.
	// +required
	Name string `json:"name"`

	// Kind is either "Issuer" or "ClusterIssuer".
	// +optional
	// +kubebuilder:default=ClusterIssuer
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	Kind string `json:"kind,omitempty"`

	// Group is the API group of the Issuer.
	// +optional
	// +kubebuilder:default=cert-manager.io
	Group string `json:"group,omitempty"`
}

// GatewayRef references a Gateway for HTTPRoute parentRefs.
type GatewayRef struct {
	// Name of the Gateway.
	// +required
	Name string `json:"name"`

	// Namespace of the Gateway (defaults to the service's namespace).
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}

// LogicFlowServiceStatus defines the observed state of LogicFlowService.
type LogicFlowServiceStatus struct {
	// ObservedGeneration tracks the last reconciled spec generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// RuntimeRef is the discovered LogicFlowRuntime from the referenced definitions.
	// +optional
	RuntimeRef *corev1.LocalObjectReference `json:"runtimeRef,omitempty"`

	// IngressRef references the created Ingress resource.
	// +optional
	IngressRef *corev1.LocalObjectReference `json:"ingressRef,omitempty"`

	// RouteRef references the created Route (OpenShift only).
	// +optional
	RouteRef *corev1.LocalObjectReference `json:"routeRef,omitempty"`

	// HTTPRouteRef references the created HTTPRoute (Gateway API).
	// +optional
	HTTPRouteRef *corev1.LocalObjectReference `json:"httpRouteRef,omitempty"`

	// URL is the full external URL for this service.
	// +optional
	URL string `json:"url,omitempty"`

	// Traffic shows the current traffic distribution.
	// +optional
	Traffic []TrafficStatus `json:"traffic,omitempty"`

	// Conditions represent detailed service state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TrafficStatus shows the status of a traffic target.
type TrafficStatus struct {
	// DefinitionRef references the workflow version.
	DefinitionRef corev1.LocalObjectReference `json:"definitionRef"`

	// Weight is the configured traffic percentage.
	Weight int32 `json:"weight"`

	// Ready indicates if this version is ready to serve traffic.
	Ready bool `json:"ready"`
}

// LogicFlowService provides stable external HTTP access to workflows.
//
// Forward reference pattern: Service → Definition (no bidirectional references).
// Supports traffic splitting for canary deployments and gradual rollouts.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName={lfs,flowsvc}
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.ingress.host`
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.status.runtimeRef.name`
// +kubebuilder:printcolumn:name="TLS",type=boolean,JSONPath=`.spec.ingress.tls.enabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LogicFlowService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicFlowServiceSpec   `json:"spec,omitempty"`
	Status LogicFlowServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogicFlowServiceList contains a list of LogicFlowService.
type LogicFlowServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicFlowService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogicFlowService{}, &LogicFlowServiceList{})
}
