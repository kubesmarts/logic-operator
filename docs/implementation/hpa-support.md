# HorizontalPodAutoscaler (HPA) Support Implementation Guide

**Status:** Design Phase  
**Target:** LogicFlowRuntime v1 API  
**Pattern:** Scale Subresource (Standard Kubernetes)

---

## Overview

LogicFlowRuntime supports Kubernetes HorizontalPodAutoscaler through the standard scale subresource pattern. This allows users to create HPA resources that automatically scale runtime pods based on CPU, memory, or custom metrics.

**Implementation Pattern:** Same as Deployment, StatefulSet, ReplicaSet  
**Previous Implementation:** SonataFlow operator v1 used this pattern successfully

---

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    User Resources                            │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  LogicFlowRuntime                  HorizontalPodAutoscaler   │
│  ├── spec.replicas: 1              ├── scaleTargetRef        │
│  └── status                        │   ├── apiVersion: v1    │
│      ├── replicas: 5  ◄────────────┤   ├── kind: LogicFl...  │
│      └── selector: "..."           │   └── name: my-runtime  │
│                                    ├── minReplicas: 2        │
│                                    ├── maxReplicas: 10       │
│                                    └── metrics: [...]        │
│                                                               │
└─────────────────────────────────────────────────────────────┘
                         │
                         │ Scale API
                         ▼
┌─────────────────────────────────────────────────────────────┐
│              LogicFlowRuntime Controller                     │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Check if HPA exists (FindHPAForLogicFlowRuntime)         │
│  2. Create/Update Deployment                                 │
│     - If HPA active: Don't set deployment.spec.replicas     │
│     - If no HPA: Use spec.replicas                          │
│  3. Update status.replicas (observed pod count)             │
│  4. Update status.selector (pod label selector)             │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

---

## API Changes Required

### 1. LogicFlowRuntimeSpec

Add replicas field:

```go
type LogicFlowRuntimeSpec struct {
    // Replicas is the desired number of pod replicas.
    // This field is ignored when a HorizontalPodAutoscaler is configured
    // and actively managing the replicas.
    //
    // If not specified, defaults to 1.
    //
    // Example:
    //   replicas: 3
    // +optional
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:default=1
    Replicas *int32 `json:"replicas,omitempty"`
    
    // ... other fields ...
}
```

### 2. LogicFlowRuntimeStatus

Add HPA-required status fields:

```go
type LogicFlowRuntimeStatus struct {
    // Replicas is the actual number of observed pods.
    // This field is required by the HorizontalPodAutoscaler via the scale subresource.
    //
    // The operator updates this field with the count of ready pods from the
    // underlying Deployment.
    // +optional
    Replicas int32 `json:"replicas,omitempty"`
    
    // Selector is the label selector for pods in string format.
    // This field is required by the HorizontalPodAutoscaler to find pods and collect metrics.
    //
    // Format: "key1=value1,key2=value2"
    //
    // Example:
    //   selector: "app=logic-flow-runtime,logic.kubesmarts.org/runtime=my-runtime"
    // +optional
    Selector string `json:"selector,omitempty"`
    
    // ... other fields ...
}
```

### 3. Scale Subresource Marker

Add kubebuilder marker to LogicFlowRuntime type:

```go
// LogicFlowRuntime is the Schema for the logicflowruntimes API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName={"lfr","runtime"}
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
type LogicFlowRuntime struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   LogicFlowRuntimeSpec   `json:"spec,omitempty"`
    Status LogicFlowRuntimeStatus `json:"status,omitempty"`
}
```

---

## Controller Implementation

### Reconcile Loop Pattern

```go
func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // 1. Fetch LogicFlowRuntime
    runtime := &logicv1.LogicFlowRuntime{}
    if err := r.Get(ctx, req.NamespacedName, runtime); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Check if HPA exists and is managing this runtime
    hpa, err := kubernetes.FindHPAForLogicFlowRuntime(ctx, r.Client, req.Namespace, req.Name)
    if err != nil {
        log.Error(err, "Failed to check for HorizontalPodAutoscaler")
        // Continue anyway - not a fatal error
    }
    
    hpaActive := hpa != nil && kubernetes.HPAIsWorking(hpa)
    
    // 3. Build/Update Deployment
    deployment := r.buildDeployment(runtime)
    
    if hpaActive {
        // HPA is managing replicas - don't set deployment.spec.replicas
        // The HPA controller will update the deployment directly
        deployment.Spec.Replicas = nil
        log.Info("HPA is active, delegating replica management to HPA",
            "hpa", hpa.Name,
            "minReplicas", *hpa.Spec.MinReplicas,
            "maxReplicas", hpa.Spec.MaxReplicas)
    } else {
        // No HPA - use spec.replicas
        replicas := int32(1) // default
        if runtime.Spec.Replicas != nil {
            replicas = *runtime.Spec.Replicas
        }
        deployment.Spec.Replicas = &replicas
    }
    
    // 4. Create or update Deployment
    if err := r.reconcileDeployment(ctx, runtime, deployment); err != nil {
        return ctrl.Result{}, err
    }
    
    // 5. Update status.replicas and status.selector
    if err := r.updateScaleStatus(ctx, runtime, deployment); err != nil {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

### Helper Functions

#### Update Scale Status

```go
func (r *LogicFlowRuntimeReconciler) updateScaleStatus(
    ctx context.Context,
    runtime *logicv1.LogicFlowRuntime,
    deployment *appsv1.Deployment,
) error {
    // Get current deployment to read actual replica count
    currentDeploy := &appsv1.Deployment{}
    if err := r.Get(ctx, client.ObjectKeyFromObject(deployment), currentDeploy); err != nil {
        return err
    }
    
    // Update replicas count (ready replicas)
    runtime.Status.Replicas = currentDeploy.Status.ReadyReplicas
    
    // Update selector (convert label selector to string)
    if currentDeploy.Spec.Selector != nil {
        selector, err := metav1.LabelSelectorAsSelector(currentDeploy.Spec.Selector)
        if err != nil {
            return fmt.Errorf("failed to convert selector: %w", err)
        }
        runtime.Status.Selector = selector.String()
    }
    
    // Update status subresource
    if err := r.Status().Update(ctx, runtime); err != nil {
        return fmt.Errorf("failed to update scale status: %w", err)
    }
    
    return nil
}
```

#### Build Deployment with Labels

```go
func (r *LogicFlowRuntimeReconciler) buildDeployment(
    runtime *logicv1.LogicFlowRuntime,
) *appsv1.Deployment {
    // Labels used for pod selector (MUST be consistent for HPA)
    labels := map[string]string{
        "app":                              "logic-flow-runtime",
        "logic.kubesmarts.org/runtime":     runtime.Name,
        "logic.kubesmarts.org/managed-by":  "logic-operator",
    }
    
    deployment := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      runtime.Name,
            Namespace: runtime.Namespace,
            Labels:    labels,
        },
        Spec: appsv1.DeploymentSpec{
            Selector: &metav1.LabelSelector{
                MatchLabels: labels,
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: labels,
                },
                Spec: corev1.PodSpec{
                    // ... pod spec ...
                },
            },
        },
    }
    
    return deployment
}
```

---

## Existing Utilities

The following HPA utility functions are already available in `utils/kubernetes/autoscaling.go`:

### FindHPAForLogicFlowRuntime

Finds an HPA targeting a specific LogicFlowRuntime:

```go
func FindHPAForLogicFlowRuntime(
    ctx context.Context,
    c client.Client,
    namespace string,
    name string,
) (*autoscalingv2.HorizontalPodAutoscaler, error)
```

### HPAIsWorking

Checks if an HPA is actively managing replicas:

```go
func HPAIsWorking(hpa *autoscalingv2.HorizontalPodAutoscaler) bool
```

Returns true if:
- HPA status shows `ScalingActive` condition is True, OR
- HPA has `DesiredReplicas > 0`

### HPAIsActive

Checks if HPA is in active state:

```go
func HPAIsActive(hpa *autoscalingv2.HorizontalPodAutoscaler) bool
```

### Other Utilities

```go
// Check if object is HPA targeting LogicFlowRuntime
func IsHPAndTargetsALogicFlowRuntime(obj client.Object) (*autoscalingv2.HorizontalPodAutoscaler, bool)

// Boolean variant
func IsHPAndTargetsALogicFlowRuntimeAsBool(obj client.Object) bool

// Check if HPA min replicas is greater than a value
func HPAMinReplicasIsGreaterThan(hpa *autoscalingv2.HorizontalPodAutoscaler, value int32) bool
```

---

## User Documentation

### Basic Usage

**Step 1: Create LogicFlowRuntime**

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: my-workflow-runtime
spec:
  replicas: 1  # Initial replicas (overridden when HPA is created)
  image: quay.io/kubesmarts/quarkus-flow:2.0.0
  podTemplate:
    container:
      resources:
        requests:
          cpu: 250m
          memory: 512Mi
        limits:
          cpu: 1000m
          memory: 1Gi
```

**Step 2: Create HorizontalPodAutoscaler**

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: my-workflow-runtime-hpa
  namespace: default
spec:
  scaleTargetRef:
    apiVersion: logic.kubesmarts.org/v1
    kind: LogicFlowRuntime
    name: my-workflow-runtime
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
      - type: Percent
        value: 100
        periodSeconds: 30
      - type: Pods
        value: 2
        periodSeconds: 30
      selectPolicy: Max
```

**Step 3: Verify HPA is Working**

```bash
# Check HPA status
kubectl get hpa my-workflow-runtime-hpa

# Check LogicFlowRuntime status
kubectl get logicflowruntime my-workflow-runtime -o yaml

# Should see:
# status:
#   replicas: 5  # Current count (managed by HPA)
#   selector: "app=logic-flow-runtime,logic.kubesmarts.org/runtime=my-workflow-runtime"
```

### Using kubectl autoscale

Users can also create HPA using kubectl:

```bash
kubectl autoscale logicflowruntime my-workflow-runtime \
  --min=2 \
  --max=10 \
  --cpu-percent=70
```

---

## Testing Checklist

### Unit Tests

- [ ] LogicFlowRuntime CRD has scale subresource in generated YAML
- [ ] Status.Replicas field is correctly typed and documented
- [ ] Status.Selector field is correctly typed and documented
- [ ] Spec.Replicas has proper validation (minimum=0)

### Integration Tests

- [ ] Create LogicFlowRuntime without HPA - replicas honored
- [ ] Create HPA targeting LogicFlowRuntime - HPA takes over
- [ ] Status.Replicas updates when pods scale
- [ ] Status.Selector is set correctly
- [ ] Deployment.Spec.Replicas is nil when HPA active
- [ ] Deployment.Spec.Replicas uses spec.replicas when no HPA
- [ ] kubectl scale works on LogicFlowRuntime
- [ ] kubectl autoscale creates working HPA

### E2E Tests

- [ ] HPA scales up based on CPU load
- [ ] HPA scales down when load decreases
- [ ] HPA respects minReplicas and maxReplicas
- [ ] Multiple HPAs can exist in same namespace
- [ ] Deleting HPA returns control to spec.replicas

---

## Common Pitfalls

### ❌ Don't Set Deployment Replicas When HPA Exists

```go
// WRONG - Will fight with HPA
deployment.Spec.Replicas = runtime.Spec.Replicas

// RIGHT - Check if HPA is active first
if hpa != nil && kubernetes.HPAIsWorking(hpa) {
    deployment.Spec.Replicas = nil  // Let HPA manage
} else {
    deployment.Spec.Replicas = runtime.Spec.Replicas
}
```

### ❌ Don't Use AvailableReplicas

```go
// WRONG - AvailableReplicas includes non-ready pods
runtime.Status.Replicas = deployment.Status.AvailableReplicas

// RIGHT - Use ReadyReplicas only
runtime.Status.Replicas = deployment.Status.ReadyReplicas
```

### ❌ Don't Forget to Update Status

```go
// Status MUST be updated regularly for HPA to work
if err := r.Status().Update(ctx, runtime); err != nil {
    return err
}
```

### ❌ Don't Change Label Selectors

```go
// Labels used for selector MUST be consistent across reconciles
// Otherwise HPA will lose track of pods
labels := map[string]string{
    "app":                          "logic-flow-runtime",
    "logic.kubesmarts.org/runtime": runtime.Name,
    // DO NOT add dynamic labels here (timestamps, random IDs, etc.)
}
```

---

## Debugging

### Check HPA Status

```bash
# Detailed HPA status
kubectl describe hpa my-workflow-runtime-hpa

# Watch HPA in action
kubectl get hpa --watch

# Check HPA events
kubectl get events --field-selector involvedObject.name=my-workflow-runtime-hpa
```

### Check Scale Subresource

```bash
# View scale info
kubectl get logicflowruntime my-workflow-runtime --subresource=scale

# Manually scale (test scale subresource)
kubectl scale logicflowruntime my-workflow-runtime --replicas=5
```

### Common Issues

**HPA shows "unknown" for metrics:**
- Check that pods have resource requests defined
- Verify metrics-server is running: `kubectl get deployment metrics-server -n kube-system`

**HPA not scaling:**
- Check `status.replicas` is being updated by controller
- Check `status.selector` matches actual pod labels
- Verify HPA metrics are collecting: `kubectl get hpa <name> -o yaml`

**Deployment fights with HPA:**
- Controller is setting `deployment.spec.replicas` even when HPA is active
- Fix: Only set replicas when HPA is NOT active

---

## References

- **Kubernetes HPA Documentation:** https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/
- **Scale Subresource:** https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#scale-subresource
- **Kubebuilder Scale Marker:** https://book.kubebuilder.io/reference/generating-crd.html#scale
- **Old Operator Implementation:** `main-1.x` branch - `api/v1alpha08/sonataflow_types.go`
- **Utils Package:** `utils/kubernetes/autoscaling.go`

---

## Implementation Status

- [x] Design documented
- [ ] API types updated (LogicFlowRuntimeSpec, LogicFlowRuntimeStatus)
- [ ] Kubebuilder markers added
- [ ] CRD generated with scale subresource
- [ ] Controller reconcile logic implemented
- [ ] Unit tests written
- [ ] Integration tests written
- [ ] E2E tests written
- [ ] User documentation written

---

**Last Updated:** 2026-06-23  
**Author:** Logic Operator Team  
**Review Status:** Draft
