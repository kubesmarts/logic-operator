package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterLogicPlatformSpec defines the desired state of ClusterLogicPlatform.
//
// ClusterLogicPlatform manages the cluster-scoped configuration for Logic Platform,
// including Data Index, optional Vector log collection, and Workflow Gateway.
type ClusterLogicPlatformSpec struct {
	// TargetNamespace is the namespace where platform components are deployed.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetNamespace string `json:"targetNamespace"`

	// DataIndex configures the Data Index service for workflow indexing.
	// +optional
	DataIndex *DataIndexSpec `json:"dataIndex,omitempty"`

	// Gateway configures the optional workflow-gateway component.
	// +optional
	Gateway *WorkflowGatewaySpec `json:"gateway,omitempty"`

	// Vector configures the Vector DaemonSet for log forwarding.
	// +optional
	Vector *VectorSpec `json:"vector,omitempty"`

	// Runtime configures runtime-wide platform settings, including persistence datastores.
	// +optional
	Runtime *RuntimePlatformSpec `json:"runtime,omitempty"`

	// DBMigrator configures the schema migration tool.
	// +optional
	DBMigrator *DBMigratorSpec `json:"dbMigrator,omitempty"`
}

// ClusterLogicPlatformStatus defines the observed state of ClusterLogicPlatform.
type ClusterLogicPlatformStatus struct {
	// Phase summarizes the readiness of all enabled platform components.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +optional
	Phase ApplicationPhase `json:"phase,omitempty"`

	// ObservedGeneration tracks the last reconciled spec generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// DataIndex contains status for the Data Index component when enabled.
	// +optional
	DataIndex *DataIndexStatus `json:"dataIndex,omitempty"`

	// Vector contains status for the Vector component when enabled.
	// +optional
	Vector *VectorStatus `json:"vector,omitempty"`

	// Gateway contains status for the workflow gateway component.
	// +optional
	Gateway *WorkflowGatewayStatus `json:"gateway,omitempty"`

	// Conditions represent detailed platform state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DBMigratorSpec configures the schema migration tool.
type DBMigratorSpec struct {
	// Image is the container image for the database migration tool.
	Image string `json:"image"`
}

// ClusterLogicPlatform manages the cluster-scoped configuration for Logic Platform.
//
// Architecture: ClusterLogicPlatform manages cluster-wide platform configuration in its own namespace,
// while LogicFlowRuntime resources are deployed in the target namespace.
//
// Example:
//
//	apiVersion: logic.kubesmarts.org/v1
//	kind: ClusterLogicPlatform
//	metadata:
//	  name: global-platform
//	spec:
//	  targetNamespace: workflows
//	  dataIndex:
//	    enabled: true
//	  gateway:
//	    application:
//	      image: quay.io/kubesmarts/workflow-gateway:2.0.0
//	  runtime:
//	    persistence:
//	      runtime:
//	        postgresql:
//	          secretRef:
//	            name: postgres-credentials
//	          serviceRef:
//	            name: postgres
//	            databaseName: workflows
//	            databaseSchema: runtime
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName={clp}
// +kubebuilder:printcolumn:name="Target Namespace",type=string,JSONPath=`.spec.targetNamespace`
// +kubebuilder:printcolumn:name="Data Index Ready",type=boolean,JSONPath=`.status.dataIndex.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ClusterLogicPlatform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterLogicPlatformSpec   `json:"spec,omitempty"`
	Status ClusterLogicPlatformStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterLogicPlatformList contains a list of ClusterLogicPlatform.
type ClusterLogicPlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterLogicPlatform `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterLogicPlatform{}, &ClusterLogicPlatformList{})
}
