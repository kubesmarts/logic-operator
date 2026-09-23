# LogicPlatform Vector Implementation Handoff

**Status**: Ready for Implementation  
**Branch**: `feat/logicplatform-vector`  
**Issue**: https://github.com/kubesmarts/logic-operator/issues/8  
**Date**: 2026-09-22

## Overview

Implement Vector DaemonSet deployment as part of LogicPlatform controller to collect workflow logs and forward them to Data Index PostgreSQL.

## Architecture

### Data Flow (MODE 1 - PostgreSQL)
```
Workflow Pods (stdout) 
  → /var/log/containers/*.log
  → Vector DaemonSet (kubernetes_logs source)
  → Parse JSON → Filter workflow events
  → PostgreSQL (workflow_events_raw, task_events_raw)
  → PostgreSQL triggers normalize to tables
  → Data Index Service (GraphQL) queries normalized tables
```

### Deployment Model (Phase 1 - Local Mode)

```
Namespace: data-index
├── LogicPlatform (local mode, spec.dataIndex.enabled=true)
│   ├── Data Index Service (Deployment) ✅ Already implemented
│   ├── PostgreSQL (user provides connection in spec)
│   └── Vector DaemonSet ❌ TO IMPLEMENT
│       └── Watches ALL namespaces by default (user-configurable)

Namespace: workflows-a
└── LogicFlowRuntimes → logs → Vector → Data Index PostgreSQL

Namespace: workflows-b  
└── LogicFlowRuntimes → logs → Vector → Data Index PostgreSQL
```

**Key Decisions:**
- ✅ **One Vector DaemonSet** per LogicPlatform (deployed in platform namespace)
- ✅ **Watch ALL namespaces** by default (`spec.dataIndex.vector.watchNamespaces: ["*"]`)
- ✅ **User-configurable namespace filter** for sharding (Phase 2)
- ✅ **BYO PostgreSQL** (user provides connection in `spec.dataIndex.persistence`)

## Implementation Steps

### 1. API Changes

**File**: `api/v1/logicplatform_types.go`

#### Replace FluentBitSpec with VectorSpec

```go
type DataIndexSpec struct {
	// ... existing fields (Enabled, Application, Persistence) ...
	
	// Vector configures the Vector DaemonSet for log forwarding.
	//
	// When configured, the operator deploys a Vector DaemonSet to collect and
	// forward workflow logs to Data Index PostgreSQL. Vector runs one pod per
	// node and tails logs from the specified namespaces.
	//
	// Example:
	//   vector:
	//     watchNamespaces: ["*"]  # All namespaces (default)
	//     image: timberio/vector:0.54.0-distroless-libc
	//     resources:
	//       requests:
	//         memory: 128Mi
	//         cpu: 100m
	// +optional
	Vector *VectorSpec `json:"vector,omitempty"`
}

// VectorSpec configures the Vector log forwarding DaemonSet.
//
// Vector is deployed as a DaemonSet (one pod per node) to collect logs
// from workflow pods and forward them to Data Index PostgreSQL.
//
// The DaemonSet runs on every node and mounts the node's /var/log directory
// to collect container logs. It filters logs by namespace and event type
// before forwarding to PostgreSQL.
type VectorSpec struct {
	// WatchNamespaces specifies which namespaces Vector should tail logs from.
	//
	// Special values:
	//   - ["*"]: Watch all namespaces (default)
	//   - ["ns1", "ns2"]: Watch only specified namespaces
	//
	// This enables organizational sharding (e.g., team-a, team-b namespaces)
	// while all events sink to the same Data Index.
	//
	// Example:
	//   watchNamespaces: ["*"]                    # All namespaces
	//   watchNamespaces: ["workflows", "prod"]    # Specific namespaces
	//
	// +optional
	// +kubebuilder:default={"*"}
	WatchNamespaces []string `json:"watchNamespaces,omitempty"`

	// Container configures the Vector DaemonSet container.
	// Use this for full control over the Vector configuration.
	// +optional
	Container ContainerSpec `json:"container,omitempty"`

	// Image specifies the Vector container image.
	// This is a convenience field - if container.image is set, it takes precedence.
	//
	// Default: timberio/vector:0.54.0-distroless-libc
	//
	// Example: timberio/vector:0.54.0-distroless-libc
	// +optional
	Image           string            `json:"image,omitempty"`
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// Resources specifies compute resource requirements for the Vector DaemonSet pods.
	// This is a convenience field - if container.resources is set, it takes precedence.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// DebugEvents enables stdout logging of all workflow events.
	// WARNING: At 1000 events/sec, generates ~43GB/day of logs.
	// Only enable for troubleshooting.
	// +optional
	// +kubebuilder:default=false
	DebugEvents bool `json:"debugEvents,omitempty"`
}
```

#### Remove FluentBitSpec

Search for `FluentBitSpec` and remove the entire type definition and all references.

**Files to update:**
- `api/v1/logicplatform_types.go` - Remove FluentBitSpec type
- Run `make manifests` to regenerate CRDs

### 2. Create Constants File

**File**: `internal/controller/dataindex_constants.go`

```go
package controller

const (
	// Vector DaemonSet
	VectorRegistry = "timberio"
	VectorImage    = "vector"
	VectorVersion  = "0.54.0-distroless-libc"
	VectorPort     = int32(9598) // Prometheus metrics
	VectorAPIPort  = int32(8686) // Health/readiness API

	// Vector configuration
	VectorConfigMountPath = "/etc/vector"
	VectorDataMountPath   = "/tmp/vector"
	VectorVarLogPath      = "/var/log"
	VectorDockerPath      = "/var/lib/docker/containers"

	// Vector RBAC
	VectorServiceAccount = "vector"
	VectorClusterRole    = "logic-operator-vector"

	// Data Index Service
	DataIndexRegistry = "quay.io/kubesmarts"
	DataIndexImage    = "data-index"
	DataIndexVersion  = "1.0.0"
	DataIndexPort     = int32(8080)

	// PostgreSQL raw event tables (created by Vector, normalized by triggers)
	PostgresWorkflowEventsTable = "workflow_events_raw"
	PostgresTaskEventsTable     = "task_events_raw"
)

// Default Vector resource requirements
var DefaultVectorResources = corev1.ResourceRequirements{
	Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	},
	Limits: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1000m"),
		corev1.ResourceMemory: resource.MustParse("512Mi"),
	},
}
```

### 3. Add Go Module Dependency

**File**: `go.mod`

Add dependency (wait for logic-apps tag):
```go
require (
    github.com/kubesmarts/logic-apps/data-index/collectors v1.0.0
    // ... other dependencies
)
```

**Note**: User will tag logic-apps soon. Use version from tag.

### 4. Add Makefile Sync Target

**File**: `Makefile`

Add new target:
```makefile
.PHONY: sync-configs
sync-configs:
	@echo "Syncing data-index configs from Go module..."
	@mkdir -p internal/configs
	@MODPATH=$$(go list -m -f '{{.Dir}}' github.com/kubesmarts/logic-apps/data-index/collectors 2>/dev/null); \
	if [ -z "$$MODPATH" ]; then \
		echo "ERROR: Module github.com/kubesmarts/logic-apps/data-index/collectors not found"; \
		echo "Run: go mod download"; \
		exit 1; \
	fi; \
	rm -rf internal/configs/vector; \
	cp -r $$MODPATH/vector internal/configs/; \
	echo "✓ Configs synced from $$MODPATH/vector"
```

Update existing `generate` target:
```makefile
.PHONY: generate
generate: controller-gen sync-configs ## Generate code and sync configs
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
```

### 5. Add to .gitignore

**File**: `.gitignore`

```gitignore
# Generated configs (synced from Go module dependencies)
internal/configs/
```

### 6. RBAC Manifests

**File**: `config/rbac/vector_cluster_role.yaml` (NEW)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: logic-operator-vector
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/component: vector
rules:
  - apiGroups: [""]
    resources:
      - namespaces
      - pods
      - nodes
    verbs: ["get", "list", "watch"]
```

**File**: `config/rbac/kustomization.yaml`

Add to resources list:
```yaml
resources:
- role.yaml
- role_binding.yaml
- leader_election_role.yaml
- leader_election_role_binding.yaml
- vector_cluster_role.yaml  # ← ADD THIS
```

**Note**: ClusterRoleBinding will be created dynamically by the controller
(binds per-namespace ServiceAccount to ClusterRole).

### 7. Controller Implementation

**File**: `internal/controller/logicplatform_controller.go`

#### Add Embedded Config

```go
package controller

import (
	_ "embed"
	// ... other imports
)

// Embed Vector configuration from synced configs
//go:embed internal/configs/vector/mode1-postgresql/vector.yaml
var vectorPostgreSQLConfig string
```

#### Update Reconcile Loop

```go
func (r *LogicPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var platform logicv1.LogicPlatform
	if err := r.Get(ctx, req.NamespacedName, &platform); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile Data Index components if enabled
	if platform.Spec.DataIndex.Enabled {
		// 1. Reconcile Data Index Service (already implemented)
		if err := r.reconcileDataIndexService(ctx, &platform); err != nil {
			log.Error(err, "failed to reconcile Data Index Service")
			return ctrl.Result{}, err
		}

		// 2. Reconcile Vector DaemonSet (NEW)
		if platform.Spec.DataIndex.Vector != nil {
			if err := r.reconcileVector(ctx, &platform); err != nil {
				log.Error(err, "failed to reconcile Vector DaemonSet")
				return ctrl.Result{}, err
			}
		}
	}

	// Update status
	if err := r.updateStatus(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
```

#### Add Vector Reconciliation Methods

Create new file: `internal/controller/logicplatform_vector.go`

```go
package controller

import (
	"context"
	"fmt"
	"strings"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *LogicPlatformReconciler) reconcileVector(ctx context.Context, platform *logicv1.LogicPlatform) error {
	// 1. Create ServiceAccount
	if err := r.applyVectorServiceAccount(ctx, platform); err != nil {
		return fmt.Errorf("failed to apply Vector ServiceAccount: %w", err)
	}

	// 2. Create ClusterRoleBinding (binds SA to existing ClusterRole)
	if err := r.applyVectorClusterRoleBinding(ctx, platform); err != nil {
		return fmt.Errorf("failed to apply Vector ClusterRoleBinding: %w", err)
	}

	// 3. Create ConfigMap with templated vector.yaml
	if err := r.applyVectorConfigMap(ctx, platform); err != nil {
		return fmt.Errorf("failed to apply Vector ConfigMap: %w", err)
	}

	// 4. Create DaemonSet
	if err := r.applyVectorDaemonSet(ctx, platform); err != nil {
		return fmt.Errorf("failed to apply Vector DaemonSet: %w", err)
	}

	// 5. Create Service (for Prometheus metrics - optional)
	if err := r.applyVectorService(ctx, platform); err != nil {
		return fmt.Errorf("failed to apply Vector Service: %w", err)
	}

	return nil
}

func (r *LogicPlatformReconciler) applyVectorServiceAccount(ctx context.Context, platform *logicv1.LogicPlatform) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VectorServiceAccount,
			Namespace: platform.Namespace,
			Labels:    vectorLabels(platform),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(platform, logicv1.GroupVersion.WithKind("LogicPlatform")),
			},
		},
	}
	return r.Apply(ctx, sa, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyVectorClusterRoleBinding(ctx context.Context, platform *logicv1.LogicPlatform) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("logic-operator-vector-%s-%s", platform.Namespace, platform.Name),
			Labels: vectorLabels(platform),
			// Note: ClusterRoleBinding cannot have OwnerReferences to namespaced resources
			// We'll need to clean this up when platform is deleted (finalizer)
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     VectorClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      VectorServiceAccount,
				Namespace: platform.Namespace,
			},
		},
	}
	return r.Apply(ctx, crb, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyVectorConfigMap(ctx context.Context, platform *logicv1.LogicPlatform) error {
	// Template vector.yaml with platform-specific values
	config := r.templateVectorConfig(platform)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vector-config",
			Namespace: platform.Namespace,
			Labels:    vectorLabels(platform),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(platform, logicv1.GroupVersion.WithKind("LogicPlatform")),
			},
		},
		Data: map[string]string{
			"vector.yaml": config,
		},
	}
	return r.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) templateVectorConfig(platform *logicv1.LogicPlatform) string {
	// Start with embedded config
	config := vectorPostgreSQLConfig

	// Template is already correct - it uses environment variables
	// The DaemonSet will inject: WORKFLOW_NAMESPACE, POSTGRES_*, DEBUG_EVENTS
	// No string replacement needed - config references ${WORKFLOW_NAMESPACE} etc.

	return config
}

func (r *LogicPlatformReconciler) applyVectorDaemonSet(ctx context.Context, platform *logicv1.LogicPlatform) error {
	ds := r.buildVectorDaemonSet(platform)
	return r.Apply(ctx, ds, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) buildVectorDaemonSet(platform *logicv1.LogicPlatform) *appsv1.DaemonSet {
	spec := platform.Spec.DataIndex.Vector
	labels := vectorLabels(platform)
	
	// Build image
	image := vectorImage(spec)
	
	// Build namespace filter
	watchNamespaces := spec.WatchNamespaces
	if len(watchNamespaces) == 0 || (len(watchNamespaces) == 1 && watchNamespaces[0] == "*") {
		// Empty means all namespaces - Vector kubernetes_logs will watch all
		watchNamespaces = []string{"*"}
	}
	// Join namespaces for env var (Vector config will use this)
	namespaceFilter := strings.Join(watchNamespaces, ",")

	// Build env vars
	envVars := []corev1.EnvVar{
		// Kubernetes metadata
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		// Workflow configuration
		{
			Name:  "WORKFLOW_NAMESPACE",
			Value: namespaceFilter,
		},
		// Debug
		{
			Name:  "DEBUG_EVENTS",
			Value: fmt.Sprintf("%t", spec.DebugEvents),
		},
	}

	// PostgreSQL connection from platform.Spec.DataIndex.Persistence
	if platform.Spec.DataIndex.Persistence.PostgreSQL != nil {
		pg := platform.Spec.DataIndex.Persistence.PostgreSQL
		
		// Build connection details
		host, port, db := buildPostgreSQLConnection(pg, platform.Namespace)
		
		envVars = append(envVars,
			corev1.EnvVar{Name: "POSTGRES_HOST", Value: host},
			corev1.EnvVar{Name: "POSTGRES_PORT", Value: fmt.Sprintf("%d", port)},
			corev1.EnvVar{Name: "POSTGRES_DB", Value: db},
		)

		// Add credentials from secret
		userKey := pg.SecretRef.UserKey
		if userKey == "" {
			userKey = "postgres-username"
		}
		passwordKey := pg.SecretRef.PasswordKey
		if passwordKey == "" {
			passwordKey = "postgres-password"
		}

		envVars = append(envVars,
			corev1.EnvVar{
				Name: "POSTGRES_USER",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pg.SecretRef.Name,
						},
						Key: userKey,
					},
				},
			},
			corev1.EnvVar{
				Name: "POSTGRES_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: pg.SecretRef.Name,
						},
						Key: passwordKey,
					},
				},
			},
		)
	}

	// Build resources
	resources := spec.Resources
	if resources.Requests == nil && resources.Limits == nil {
		resources = DefaultVectorResources
	}

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vector",
			Namespace: platform.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(platform, logicv1.GroupVersion.WithKind("LogicPlatform")),
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   fmt.Sprintf("%d", VectorPort),
						"prometheus.io/path":   "/metrics",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: VectorServiceAccount,
					HostNetwork:        false,
					DNSPolicy:          corev1.DNSClusterFirst,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						RunAsUser:    &[]int64{1000}[0],
						FSGroup:      &[]int64{1000}[0],
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "vector",
							Image:           image,
							ImagePullPolicy: imagePullPolicy(spec),
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &[]bool{false}[0],
								ReadOnlyRootFilesystem:   &[]bool{true}[0],
								RunAsNonRoot:             &[]bool{true}[0],
								RunAsUser:                &[]int64{1000}[0],
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "metrics",
									ContainerPort: VectorPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "api",
									ContainerPort: VectorAPIPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: envVars,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: VectorConfigMountPath,
									ReadOnly:  true,
								},
								{
									Name:      "data-dir",
									MountPath: VectorDataMountPath,
								},
								{
									Name:      "varlog",
									MountPath: VectorVarLogPath,
									ReadOnly:  true,
								},
								{
									Name:      "varlibdockercontainers",
									MountPath: VectorDockerPath,
									ReadOnly:  true,
								},
							},
							Resources: resources,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(VectorAPIPort),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(VectorAPIPort),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
								FailureThreshold:    2,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "vector-config",
									},
								},
							},
						},
						{
							Name: "data-dir",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "varlog",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: VectorVarLogPath,
									Type: &[]corev1.HostPathType{corev1.HostPathDirectory}[0],
								},
							},
						},
						{
							Name: "varlibdockercontainers",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: VectorDockerPath,
									Type: &[]corev1.HostPathType{corev1.HostPathDirectoryOrCreate}[0],
								},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Effect:   corev1.TaintEffectNoSchedule,
							Operator: corev1.TolerationOpExists,
						},
						{
							Key:      "node-role.kubernetes.io/master",
							Effect:   corev1.TaintEffectNoSchedule,
							Operator: corev1.TolerationOpExists,
						},
					},
					PriorityClassName: "system-node-critical",
				},
			},
		},
	}
}

func (r *LogicPlatformReconciler) applyVectorService(ctx context.Context, platform *logicv1.LogicPlatform) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vector",
			Namespace: platform.Namespace,
			Labels:    vectorLabels(platform),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(platform, logicv1.GroupVersion.WithKind("LogicPlatform")),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: vectorLabels(platform),
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       VectorPort,
					TargetPort: intstr.FromInt32(VectorPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

// Helper functions

func vectorLabels(platform *logicv1.LogicPlatform) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "vector",
		"app.kubernetes.io/component":  "log-collector",
		"app.kubernetes.io/part-of":    "logic-platform",
		"app.kubernetes.io/managed-by": "logic-operator",
		"app.kubernetes.io/instance":   platform.Name,
	}
}

func vectorImage(spec *logicv1.VectorSpec) string {
	if spec.Image != "" {
		return spec.Image
	}
	if spec.Container.Image != nil && *spec.Container.Image != "" {
		return *spec.Container.Image
	}
	return fmt.Sprintf("%s/%s:%s", VectorRegistry, VectorImage, VectorVersion)
}

func imagePullPolicy(spec *logicv1.VectorSpec) corev1.PullPolicy {
	if spec.ImagePullPolicy != "" {
		return spec.ImagePullPolicy
	}
	if spec.Container.ImagePullPolicy != nil && *spec.Container.ImagePullPolicy != "" {
		return *spec.Container.ImagePullPolicy
	}
	return corev1.PullIfNotPresent
}

func buildPostgreSQLConnection(pg *logicv1.PostgreSQLPersistenceOptions, fallbackNamespace string) (host string, port int, db string) {
	if pg.ServiceRef != nil {
		ns := pg.ServiceRef.Namespace
		if ns == "" {
			ns = fallbackNamespace
		}
		host = fmt.Sprintf("%s.%s.svc.cluster.local", pg.ServiceRef.Name, ns)
		
		port = 5432
		if pg.ServiceRef.Port != nil {
			port = int(*pg.ServiceRef.Port)
		}
		
		db = pg.ServiceRef.DatabaseName
		if db == "" {
			db = "dataindex"
		}
	}
	return
}
```

#### Add Finalizer for ClusterRoleBinding Cleanup

Update `Reconcile` to handle ClusterRoleBinding cleanup:

```go
func (r *LogicPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var platform logicv1.LogicPlatform
	if err := r.Get(ctx, req.NamespacedName, &platform); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !platform.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &platform)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&platform, "logicplatform.logic.kubesmarts.org/finalizer") {
		controllerutil.AddFinalizer(&platform, "logicplatform.logic.kubesmarts.org/finalizer")
		if err := r.Update(ctx, &platform); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ... rest of reconciliation ...
}

func (r *LogicPlatformReconciler) reconcileDelete(ctx context.Context, platform *logicv1.LogicPlatform) (ctrl.Result, error) {
	// Delete ClusterRoleBinding (not owned by namespaced resource)
	if platform.Spec.DataIndex.Vector != nil {
		crbName := fmt.Sprintf("logic-operator-vector-%s-%s", platform.Namespace, platform.Name)
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: crbName},
		}
		if err := r.Delete(ctx, crb); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(platform, "logicplatform.logic.kubesmarts.org/finalizer")
	if err := r.Update(ctx, platform); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
```

### 8. Add RBAC for LogicPlatform Controller

**File**: `config/rbac/role.yaml`

Add Vector-related permissions:

```yaml
# Vector resources (created by controller)
- apiGroups: [""]
  resources:
    - serviceaccounts
    - configmaps
    - services
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources:
    - daemonsets
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources:
    - clusterrolebindings
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### 9. Update SetupWithManager

**File**: `internal/controller/logicplatform_controller.go`

Add watches for Vector resources:

```go
func (r *LogicPlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicPlatform{}).
		Owns(&appsv1.Deployment{}). // Data Index Service
		Owns(&corev1.Service{}).
		Owns(&appsv1.DaemonSet{}). // Vector
		Owns(&corev1.ConfigMap{}). // Vector config
		Owns(&corev1.ServiceAccount{}). // Vector SA
		Named("logicplatform").
		Complete(r)
}
```

## Testing Strategy

### Unit Tests

**File**: `internal/controller/logicplatform_vector_test.go`

Test cases:
1. ✅ `TestVectorDaemonSet_DefaultNamespaceFilter` - Verify `*` means all namespaces
2. ✅ `TestVectorDaemonSet_CustomNamespaceFilter` - Verify custom namespaces
3. ✅ `TestVectorDaemonSet_PostgreSQLConnection` - Verify env vars
4. ✅ `TestVectorDaemonSet_DefaultImage` - Verify default image
5. ✅ `TestVectorDaemonSet_CustomImage` - Verify custom image override
6. ✅ `TestVectorDaemonSet_Resources` - Verify resource requests/limits
7. ✅ `TestVectorConfigMap_Templating` - Verify config templating
8. ✅ `TestVectorRBAC_ServiceAccount` - Verify SA creation
9. ✅ `TestVectorRBAC_ClusterRoleBinding` - Verify CRB creation
10. ✅ `TestVectorFinalizer_ClusterRoleBindingCleanup` - Verify CRB deletion

### E2E Tests

**File**: `test/e2e/logicplatform_test.go`

Test cases:
1. ✅ Deploy LogicPlatform with Vector enabled
2. ✅ Verify Vector DaemonSet running on all nodes
3. ✅ Verify Vector can tail logs from workflow pods
4. ✅ Verify events reach PostgreSQL raw tables
5. ✅ Delete LogicPlatform and verify ClusterRoleBinding cleanup

## Implementation Order

### Phase 1: API & Dependencies (No Code Changes)
1. ✅ Tag logic-apps with version (user will do)
2. ✅ Add Go module dependency to go.mod
3. ✅ Update API (VectorSpec, remove FluentBitSpec)
4. ✅ Create constants file (dataindex_constants.go)
5. ✅ Add Makefile sync-configs target
6. ✅ Add RBAC manifest (vector_cluster_role.yaml)
7. ✅ Run `make generate manifests` to regenerate CRDs

### Phase 2: Controller Implementation
1. ✅ Create logicplatform_vector.go
2. ✅ Implement reconcileVector
3. ✅ Implement DaemonSet builder
4. ✅ Implement ConfigMap templating
5. ✅ Add finalizer for ClusterRoleBinding cleanup
6. ✅ Update SetupWithManager

### Phase 3: Testing
1. ✅ Write unit tests
2. ✅ Write E2E tests
3. ✅ Test with sample LogicPlatform CR

### Phase 4: Documentation
1. ✅ Update issue #8 checklist
2. ✅ Add usage examples to samples/
3. ✅ Update operator documentation

## Sample CR

**File**: `config/samples/logic_v1_logicplatform_vector.yaml`

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicPlatform
metadata:
  name: platform-with-vector
  namespace: data-index
spec:
  dataIndex:
    enabled: true
    
    # PostgreSQL persistence (required for Vector)
    persistence:
      postgresql:
        secretRef:
          name: postgresql-credentials
        serviceRef:
          name: postgresql
          namespace: data-index
          databaseName: dataindex
    
    # Data Index Service
    application:
      replicas: 2
      resources:
        requests:
          memory: 512Mi
          cpu: 250m
    
    # Vector DaemonSet
    vector:
      # Watch all namespaces (default)
      watchNamespaces: ["*"]
      
      # Or watch specific namespaces
      # watchNamespaces: ["workflows", "team-a", "team-b"]
      
      # Optional: custom image
      # image: timberio/vector:0.54.0-distroless-libc
      
      # Optional: resource limits
      resources:
        requests:
          memory: 128Mi
          cpu: 100m
        limits:
          memory: 512Mi
          cpu: 1000m
      
      # Debug events (testing only)
      debugEvents: false
```

## Validation Checklist

Before marking implementation complete:

- [ ] API changes merged and CRDs regenerated
- [ ] Constants file created with correct image versions
- [ ] Go module dependency added and configs synced
- [ ] RBAC manifests created (ClusterRole)
- [ ] Controller creates all resources correctly:
  - [ ] ServiceAccount
  - [ ] ClusterRoleBinding
  - [ ] ConfigMap (templated vector.yaml)
  - [ ] DaemonSet
  - [ ] Service (metrics)
- [ ] Finalizer cleans up ClusterRoleBinding
- [ ] Unit tests pass
- [ ] E2E tests pass
- [ ] Sample CR works end-to-end
- [ ] Issue #8 checklist updated

## References

- **Issue**: https://github.com/kubesmarts/logic-operator/issues/8
- **logic-apps collectors module**: https://github.com/kubesmarts/logic-apps/tree/main/data-index/collectors
- **Helm chart reference**: `/Users/ricferna/dev/github/kubesmarts/logic-apps/data-index/helm/data-index/`
- **Vector config**: `/Users/ricferna/dev/github/kubesmarts/logic-apps/data-index/collectors/vector/mode1-postgresql/vector.yaml`
- **DaemonSet example**: `/Users/ricferna/dev/github/kubesmarts/logic-apps/data-index/collectors/examples/mode1-postgresql/daemonset.yaml`

## Notes

- **Namespace filter `*`**: Means all namespaces (not `["*"]`)
- **Vector config**: Uses environment variable substitution (`${WORKFLOW_NAMESPACE}`), no string templating needed
- **ClusterRoleBinding cleanup**: Requires finalizer since CR is namespaced but CRB is cluster-scoped
- **PostgreSQL schema**: Reuses existing schema from Data Index Service
- **Image versions**: Centralized in constants file for easy updates
- **Future**: Phase 2 will add remote platform support (Vector in team namespace → remote Data Index)

## Questions for Implementation

1. ✅ **Resolved**: `*` means all namespaces
2. ✅ **Resolved**: Default image `timberio/vector:0.54.0-distroless-libc`
3. ✅ **Resolved**: RBAC via static manifests, not programmatic
4. ⏳ **Pending**: logic-apps tag version (user will provide)
5. ⏳ **Pending**: Should we add status conditions for Vector (e.g., `VectorReady`)?

---

**Ready for implementation on branch `feat/logicplatform-vector`**
