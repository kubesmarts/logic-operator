package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterLogicPlatformSpec defines the desired state of ClusterLogicPlatform.
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

	// DBMigrator configures the schema migration Job used by datastores with dbMigrationStrategy=job.
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

	// DBMigrator contains status for the schema migration Jobs when any datastore uses dbMigrationStrategy=job.
	// +optional
	DBMigrator *DBMigratorStatus `json:"dbMigrator,omitempty"`

	// Conditions represent detailed platform state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DBMigratorSpec configures the schema migration Job.
type DBMigratorSpec struct {
	// Application configures the migration Job's pod and container.
	// Image defaults to the logic-db-migrator image bundled with this operator release; replicas is ignored.
	// +optional
	Application ApplicationSpec `json:"application,omitempty"`
}

// MigrationPhase summarizes the state of a schema migration Job.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type MigrationPhase string

const (
	MigrationPhasePending   MigrationPhase = "Pending"
	MigrationPhaseRunning   MigrationPhase = "Running"
	MigrationPhaseSucceeded MigrationPhase = "Succeeded"
	MigrationPhaseFailed    MigrationPhase = "Failed"
)

// DBMigratorStatus reports the schema migration Jobs of a ClusterLogicPlatform.
type DBMigratorStatus struct {
	// Migrations lists one entry per migrated datastore.
	// +optional
	// +listType=map
	// +listMapKey=datastore
	Migrations []DatastoreMigrationStatus `json:"migrations,omitempty"`
}

// DatastoreMigrationStatus is the outcome of the migration Job for one datastore.
type DatastoreMigrationStatus struct {
	// Datastore names the migrated datastore: runtime, quartz, or data-index.
	Datastore string `json:"datastore"`
	// JobRef names the Job backing this migration in the target namespace.
	// +optional
	JobRef string `json:"jobRef,omitempty"`
	// Phase summarizes the Job state.
	// +optional
	Phase MigrationPhase `json:"phase,omitempty"`
	// Message carries the failure detail reported by the migrator.
	// +optional
	Message string `json:"message,omitempty"`
	// Streams reports the outcome per schema stream, parsed from the migrator's termination message.
	// +optional
	Streams []MigrationStreamStatus `json:"streams,omitempty"`
}

// MigrationStreamStatus is one schema stream's result within a migration Job.
type MigrationStreamStatus struct {
	// Name of the stream, e.g. runtime, quartz, or data-index.
	Name string `json:"name"`
	// AppliedVersion is the schema version reported after the run.
	// +optional
	AppliedVersion string `json:"appliedVersion,omitempty"`
	// AppliedCount is how many migrations were applied in the run.
	// +optional
	AppliedCount int32 `json:"appliedCount,omitempty"`
	// LastAppliedAt is when the Job that applied the stream completed.
	// +optional
	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`
}

// ClusterLogicPlatform manages the cluster-scoped configuration for Logic Platform.
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
