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

import corev1 "k8s.io/api/core/v1"

// TLSSpec configures TLS/HTTPS for Ingress and Route resources.
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
