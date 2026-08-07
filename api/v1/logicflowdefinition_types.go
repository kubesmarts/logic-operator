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
	"fmt"

	"github.com/open-workflow-specification/sdk-go/v4/model"
	"github.com/open-workflow-specification/sdk-go/v4/parser"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// LogicFlowDefinitionSpec defines the desired state of LogicFlowDefinition.
//
// Immutable workflow version referencing a LogicFlowRuntime for execution.
type LogicFlowDefinitionSpec struct {
	// RuntimeRef points to the LogicFlowRuntime executing this workflow.
	// +required
	RuntimeRef corev1.LocalObjectReference `json:"runtimeRef"`

	// Flow contains the full Open Workflow Specification 1.0.0 document.
	// Stored as raw JSON; parsed and validated via sdk-go during admission.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +required
	Flow runtime.RawExtension `json:"flow"`
}

// ParseFlow deserializes the raw flow field into a model.Workflow.
func (s *LogicFlowDefinitionSpec) ParseFlow() (*model.Workflow, error) {
	if s.Flow.Raw == nil {
		return nil, fmt.Errorf("flow field is empty")
	}
	wf, err := parser.FromJSONSource(s.Flow.Raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse flow: %w", err)
	}
	return wf, nil
}

// LogicFlowDefinitionStatus defines the observed state of LogicFlowDefinition.
type LogicFlowDefinitionStatus struct {
	// ObservedGeneration tracks the last reconciled spec generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// WorkflowName is the workflow identity extracted from flow.document.name.
	// +optional
	WorkflowName string `json:"workflowName,omitempty"`

	// WorkflowVersion is the version extracted from flow.document.version.
	// +optional
	WorkflowVersion string `json:"workflowVersion,omitempty"`

	// WorkflowNamespace is the DSL namespace extracted from flow.document.namespace.
	// Not the Kubernetes namespace.
	// +optional
	WorkflowNamespace string `json:"workflowNamespace,omitempty"`

	// ConfigMapRef references the materialized ConfigMap containing the flow document.
	// +optional
	ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`

	// Conditions represent detailed definition state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LogicFlowDefinition represents an immutable workflow version.
//
// Each instance contains one Open Workflow Specification 1.0.0 document
// and references the LogicFlowRuntime that executes it.
// Optionally referenced by LogicFlowService for external HTTP traffic routing.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName={lfd,flowdef}
// +kubebuilder:printcolumn:name="Workflow",type=string,JSONPath=`.status.workflowName`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.workflowVersion`
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.spec.runtimeRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LogicFlowDefinition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogicFlowDefinitionSpec   `json:"spec,omitempty"`
	Status LogicFlowDefinitionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogicFlowDefinitionList contains a list of LogicFlowDefinition.
type LogicFlowDefinitionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogicFlowDefinition `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogicFlowDefinition{}, &LogicFlowDefinitionList{})
}
