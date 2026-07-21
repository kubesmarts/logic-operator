# LogicFlowService Implementation Pattern

**Status:** Design Document  
**Version:** v1.0  
**Last Updated:** 2026-06-30

## Overview

LogicFlowService provides external HTTP access to workflows with stable endpoints, traffic splitting, TLS, and version management. This document describes the architecture, relationships, and implementation strategy.

## Table of Contents

1. [Relationship Model](#relationship-model)
2. [Reference Direction Pattern](#reference-direction-pattern)
3. [Path Rewriting Strategy](#path-rewriting-strategy)
4. [Validation Strategy](#validation-strategy)
5. [Ingress Controller Support](#ingress-controller-support)
6. [TLS Configuration](#tls-configuration)
7. [Traffic Splitting](#traffic-splitting)
8. [Implementation Phases](#implementation-phases)
9. [User Workflows](#user-workflows)
10. [Platform Compatibility](#platform-compatibility)

---

## Relationship Model

### The Three-Tier Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ LogicFlowRuntime (Execution Layer)                          │
│   - Shared Quarkus Flow runner                              │
│   - Loads and executes workflows                            │
│   - HTTP endpoints: /q/flow/exec/{ns}/{workflow}/{version}  │
└─────────────────────────────────────────────────────────────┘
                    ▲                      ▲
                    │                      │
        ┌───────────┴─────────┐   ┌──────┴────────────┐
        │ LogicFlowService    │   │ LogicFlowService  │
        │ (Routing Layer)     │   │ (Routing Layer)   │
        │                     │   │                   │
        │ - Stable endpoint   │   │ - Stable endpoint │
        │ - Ingress/Route     │   │ - Ingress/Route   │
        │ - TLS termination   │   │ - TLS termination │
        │ - Traffic splits    │   │ - Traffic splits  │
        └───────────┬─────────┘   └──────┬────────────┘
                    │                     │
         ┌──────────┴────────┐       ┌───┴────────────┐
         │                   │       │                │
    ┌────▼────────┐   ┌─────▼─────┐ │                │
    │ Definition  │   │ Definition│ │  Definition    │
    │ (Version)   │   │ (Version) │ │  (Version)     │
    │             │   │           │ │                │
    │ payment-v1.0│   │ payment-v1.1│  client-v1.0   │
    └─────────────┘   └───────────┘ └────────────────┘
```

### Cardinality

- **1 LogicFlowRuntime : N LogicFlowServices**
  - One runtime can serve multiple workflows
  - Example: `payments-runtime` serves both `payment-processor` and `client-workflow`

- **1 LogicFlowService : N LogicFlowDefinitions**
  - One service groups all versions of a workflow
  - Example: `payment-processor` service routes to v1.0.0 and v1.1.0

- **1 LogicFlowDefinition : 1 Version**
  - Each definition is immutable and represents one version
  - Example: `payment-processor-v1-0-0` is version 1.0.0

### Key Principle

**LogicFlowService is OPTIONAL**

Workflows can exist and execute without a LogicFlowService:
- Internal workflows (called by other workflows)
- Background jobs
- Event-driven workflows

Only create LogicFlowService when external HTTP access is needed.

---

## Reference Direction Pattern

### Design Choice: Forward References Only

**Pattern:** Service → Definition (Knative model)

```go
type LogicFlowServiceSpec struct {
    // Service OWNS the relationship
    Traffic []TrafficSplit `json:"traffic,omitempty"`
}

type TrafficSplit struct {
    // Forward reference to Definition
    DefinitionRef ObjectReference `json:"definitionRef"`
    Weight        int32           `json:"weight"`
}

type LogicFlowDefinitionSpec struct {
    RuntimeRef ObjectReference `json:"runtimeRef"`
    Workflow   WorkflowDocument `json:"workflow"`
    // NO serviceRef - definitions are independent
}
```

### Why Not Bidirectional?

**Rejected pattern:** Definition → Service (bidirectional)

```go
// ❌ Don't do this
type LogicFlowDefinitionSpec struct {
    ServiceRef ObjectReference  // Creates circular dependency
    RuntimeRef ObjectReference
}
```

**Problems with bidirectional references:**
1. **Circular dependency** - Service needs Definition, Definition needs Service
2. **Complex validation** - Must validate both directions
3. **Unclear ownership** - Who creates who first?
4. **Unnecessary coupling** - Definitions shouldn't know about exposure

### Benefits of Forward-Only

✅ **Clear ownership** - Service owns the relationship  
✅ **Simple validation** - Only validate at Service level  
✅ **Independent definitions** - Can exist without Service  
✅ **Flexible routing** - Change traffic splits without touching Definitions  

### Industry Precedent

**Knative Serving:**
```yaml
kind: Service  # Owns the relationship
spec:
  traffic:
    - revisionName: hello-v1  # Forward reference
      percent: 80
    - revisionName: hello-v2
      percent: 20
---
kind: Revision  # Independent, no reference back
metadata:
  name: hello-v1
```

**Istio VirtualService:**
```yaml
kind: VirtualService  # Owns the relationship
spec:
  http:
    - route:
      - destination:
          host: reviews-v1  # Forward reference
        weight: 75
      - destination:
          host: reviews-v2
        weight: 25
```

---

## Path Rewriting Strategy

### The Problem

**External URL (stable, user-facing):**
```
https://payment-processor.prod.cluster.com/invoke
```

**Internal URL (Quarkus Flow runtime):**
```
http://payments-runtime.namespace.svc:8080/q/flow/exec/namespace/payment-processor/v1.0.0/invoke
```

### The Solution: Ingress Annotations

Use Ingress controller annotations to rewrite paths transparently.

#### Nginx Ingress Controller

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor
  annotations:
    # Rewrite /anything to /q/flow/exec/payments/payment-processor/v1.0.0/anything
    nginx.ingress.kubernetes.io/rewrite-target: /q/flow/exec/payments/payment-processor/v1.0.0/$2
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  rules:
    - host: payment-processor.prod.cluster.com
      http:
        paths:
          - path: /(/|$)(.*)  # Capture everything after /
            pathType: Prefix
            backend:
              service:
                name: payments-runtime
                port:
                  number: 8080
```

**How it works:**
- User calls: `POST https://payment-processor.prod.cluster.com/invoke`
- Nginx captures: `$2 = "invoke"`
- Nginx rewrites to: `POST http://payments-runtime:8080/q/flow/exec/payments/payment-processor/v1.0.0/invoke`

#### Traefik Ingress Controller

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor
  annotations:
    traefik.ingress.kubernetes.io/router.middlewares: payments-payment-rewrite@kubernetescrd
---
apiVersion: traefik.containo.us/v1alpha1
kind: Middleware
metadata:
  name: payment-rewrite
spec:
  replacePathRegex:
    regex: ^/(.*)
    replacement: /q/flow/exec/payments/payment-processor/v1.0.0/$1
```

### Building the Rewrite Path

The operator has all information needed to construct the rewrite path:

```go
func buildRewritePath(svc *v1.LogicFlowService, def *v1.LogicFlowDefinition) string {
    // Pattern: /q/flow/exec/{namespace}/{workflow-name}/{version}/$2
    return fmt.Sprintf(
        "/q/flow/exec/%s/%s/%s/$2",
        svc.Namespace,
        def.Spec.Workflow.Name,
        def.Spec.Workflow.Version,
    )
}
```

**Sources:**
- Namespace: `svc.Namespace`
- Workflow name: `def.Spec.Workflow.Name`
- Version: `def.Spec.Workflow.Version`
- Runtime service: `svc.Spec.RuntimeRef.Name`

### OpenShift Routes

OpenShift uses Routes instead of Ingress:

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: payment-processor
  annotations:
    haproxy.router.openshift.io/rewrite-target: /q/flow/exec/payments/payment-processor/v1.0.0
spec:
  host: payment-processor.prod.cluster.com
  to:
    kind: Service
    name: payments-runtime
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
```

---

## Validation Strategy

### Validation Webhook for LogicFlowService

**Validating Admission Webhook** ensures:
1. Referenced definitions exist
2. All definitions target the same runtime
3. Traffic weights sum to 100
4. (Optional) All definitions have same workflow name

```go
func (v *LogicFlowServiceValidator) ValidateCreate(svc *v1.LogicFlowService) error {
    if len(svc.Spec.Traffic) == 0 {
        return fmt.Errorf("at least one traffic entry required")
    }
    
    var workflowName string
    totalWeight := int32(0)
    
    for i, traffic := range svc.Spec.Traffic {
        // Get the definition
        def := &v1.LogicFlowDefinition{}
        if err := v.Client.Get(context.TODO(), types.NamespacedName{
            Name: traffic.DefinitionRef.Name,
            Namespace: svc.Namespace,
        }, def); err != nil {
            return fmt.Errorf("traffic[%d]: definition %s not found", 
                i, traffic.DefinitionRef.Name)
        }
        
        // Validate same runtime
        if def.Spec.RuntimeRef.Name != svc.Spec.RuntimeRef.Name {
            return fmt.Errorf("traffic[%d]: definition targets runtime %s, service targets %s",
                i, def.Spec.RuntimeRef.Name, svc.Spec.RuntimeRef.Name)
        }
        
        // Validate same workflow name (optional - helps prevent user errors)
        if workflowName == "" {
            workflowName = def.Spec.Workflow.Name
        } else if def.Spec.Workflow.Name != workflowName {
            return fmt.Errorf("traffic[%d]: workflow name mismatch: %s != %s",
                i, def.Spec.Workflow.Name, workflowName)
        }
        
        totalWeight += traffic.Weight
    }
    
    // Validate weights
    if totalWeight != 100 {
        return fmt.Errorf("traffic weights must sum to 100, got %d", totalWeight)
    }
    
    return nil
}
```

### What We DON'T Validate

**LogicFlowDefinition has NO validation of Services:**
- Definitions are independent
- No need to check if a Service references it
- Can create Definitions without Services

**Workflow name grouping is user responsibility:**
- Users must ensure `payment-processor-v1-0-0` and `payment-processor-v1-1-0` have the same `spec.workflow.name`
- We validate this at Service level (all definitions in traffic must match)
- We DON'T enforce naming conventions on Definition metadata.name

**This gives users flexibility while preventing errors at the routing layer.**

---

## Ingress Controller Support

### Supported Controllers (v1.0)

| Controller | Support Status | Rewrite Method | TLS | Notes |
|------------|----------------|---------------|-----|-------|
| **Nginx** | ✅ v1.0 | Annotations | ✅ | Most common, best canary support |
| **OpenShift Router** | ✅ v1.0 | Route CRD | ✅ | Auto-detected, uses Routes instead of Ingress |
| **Traefik** | ⏳ v1.1 | Middleware CRD | ✅ | Requires Middleware resource |
| **HAProxy** | ⏳ v1.1 | Annotations | ✅ | Limited canary support |
| **Contour** | ⏳ Future | HTTPProxy CRD | ✅ | Uses custom CRD |

### Controller Type Auto-Detection

The operator auto-detects the platform if `spec.ingress.controllerType` is not specified:

```go
func (r *LogicFlowServiceReconciler) resolveControllerType(
    ctx context.Context,
    svc *v1.LogicFlowService,
) (string, error) {
    // User specified explicitly
    if svc.Spec.Ingress.ControllerType != "" {
        return svc.Spec.Ingress.ControllerType, nil
    }
    
    // Auto-detect: OpenShift → openshift, K8s → nginx
    if r.isOpenShift(ctx) {
        return "openshift", nil
    }
    
    return "nginx", nil
}

func (r *LogicFlowServiceReconciler) isOpenShift(ctx context.Context) bool {
    // Check for Route CRD existence
    routeCRD := &apiextensionsv1.CustomResourceDefinition{}
    err := r.Client.Get(ctx, types.NamespacedName{
        Name: "routes.route.openshift.io",
    }, routeCRD)
    return err == nil
}
```

### Immutability Rules

**ControllerType immutability is platform-specific:**

```go
func (v *LogicFlowServiceValidator) ValidateUpdate(
    oldSvc, newSvc *v1.LogicFlowService,
) error {
    oldType := v.resolveControllerType(oldSvc)
    newType := v.resolveControllerType(newSvc)
    
    // OpenShift: cannot change between Route and Ingress
    if oldType == "openshift" || newType == "openshift" {
        if oldType != newType {
            return fmt.Errorf(
                "controllerType is immutable on OpenShift: cannot change from %s to %s",
                oldType, newType,
            )
        }
    }
    
    // K8s: changing between Ingress controllers is allowed
    // (nginx → traefik, traefik → nginx, etc.)
    // The operator will recreate the Ingress with new annotations
    
    return nil
}
```

**Why this rule:**
- **OpenShift:** Route is a completely different resource type than Ingress. Switching would require deleting one and creating another, breaking external DNS/load balancers.
- **K8s:** All Ingress controllers use the same `networking.k8s.io/v1/Ingress` resource type. Only annotations change, which can be updated in-place.

### Platform-Specific Logic

```go
func (r *LogicFlowServiceReconciler) createExternalAccess(
    ctx context.Context,
    svc *v1.LogicFlowService,
) error {
    controllerType := r.resolveControllerType(ctx, svc)
    
    switch controllerType {
    case "openshift":
        return r.createRoute(ctx, svc)
    case "nginx":
        return r.createNginxIngress(ctx, svc)
    default:
        return fmt.Errorf("unsupported controller type: %s", controllerType)
    }
}
```

---

## TLS Configuration

### Three TLS Options

#### Option 1: User-Provided Certificate

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  ingress:
    tls:
      enabled: true
      secretName: payment-tls  # User creates this Secret manually
```

User creates Secret:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: payment-tls
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-cert>
  tls.key: <base64-encoded-key>
```

#### Option 2: cert-manager Integration (Recommended)

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  ingress:
    tls:
      enabled: true
      certManager:
        issuerRef:
          name: letsencrypt-prod
          kind: ClusterIssuer
```

Operator adds annotations:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
    - hosts:
        - payment-processor.prod.cluster.com
      secretName: payment-processor-tls  # cert-manager auto-creates this
```

#### Option 3: No TLS (Development)

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  ingress:
    tls:
      enabled: false  # HTTP only
```

### TLS Implementation

```go
func (r *LogicFlowServiceReconciler) buildTLSConfig(
    svc *v1.LogicFlowService,
) []networkingv1.IngressTLS {
    if svc.Spec.Ingress.TLS == nil || !svc.Spec.Ingress.TLS.Enabled {
        return nil
    }
    
    // User-provided secret
    secretName := svc.Spec.Ingress.TLS.SecretName
    
    // Auto-generate for cert-manager
    if secretName == "" {
        secretName = fmt.Sprintf("%s-tls", svc.Name)
    }
    
    return []networkingv1.IngressTLS{{
        Hosts:      []string{svc.Spec.Ingress.Host},
        SecretName: secretName,
    }}
}

func (r *LogicFlowServiceReconciler) addCertManagerAnnotations(
    svc *v1.LogicFlowService,
    annotations map[string]string,
) {
    if cm := svc.Spec.Ingress.TLS.CertManager; cm != nil {
        issuerKind := cm.IssuerRef.Kind
        if issuerKind == "" {
            issuerKind = "ClusterIssuer"
        }
        
        if issuerKind == "ClusterIssuer" {
            annotations["cert-manager.io/cluster-issuer"] = cm.IssuerRef.Name
        } else {
            annotations["cert-manager.io/issuer"] = cm.IssuerRef.Name
        }
    }
}
```

---

## Traffic Splitting

### Canary Deployments

Nginx Ingress supports canary routing via multiple Ingress resources:

**Primary Ingress (80% traffic):**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /q/flow/exec/payments/payment-processor/v1.0.0/$2
spec:
  rules:
    - host: payment-processor.prod.cluster.com
      http:
        paths:
          - path: /(/|$)(.*)
            backend:
              service:
                name: payments-runtime
                port: 8080
```

**Canary Ingress (20% traffic):**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor-v1-1-0-canary
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /q/flow/exec/payments/payment-processor/v1.1.0/$2
    # Canary annotations
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-weight: "20"
spec:
  rules:
    - host: payment-processor.prod.cluster.com  # Same host!
      http:
        paths:
          - path: /(/|$)(.*)
            backend:
              service:
                name: payments-runtime
                port: 8080
```

### Implementation

```go
func (r *LogicFlowServiceReconciler) reconcileTraffic(
    ctx context.Context,
    svc *v1.LogicFlowService,
) error {
    // Single version - simple ingress
    if len(svc.Spec.Traffic) == 1 {
        return r.createSingleIngress(ctx, svc, svc.Spec.Traffic[0])
    }
    
    // Multiple versions - canary pattern
    return r.createCanaryIngresses(ctx, svc)
}

func (r *LogicFlowServiceReconciler) createCanaryIngresses(
    ctx context.Context,
    svc *v1.LogicFlowService,
) error {
    // Sort by weight descending
    sort.Slice(svc.Spec.Traffic, func(i, j int) bool {
        return svc.Spec.Traffic[i].Weight > svc.Spec.Traffic[j].Weight
    })
    
    // Primary (highest weight) without canary annotation
    primary := svc.Spec.Traffic[0]
    if err := r.createSingleIngress(ctx, svc, primary, false); err != nil {
        return err
    }
    
    // Canaries (all others) with canary annotations
    for i := 1; i < len(svc.Spec.Traffic); i++ {
        canary := svc.Spec.Traffic[i]
        if err := r.createCanaryIngress(ctx, svc, canary); err != nil {
            return err
        }
    }
    
    return nil
}

func (r *LogicFlowServiceReconciler) createCanaryIngress(
    ctx context.Context,
    svc *v1.LogicFlowService,
    traffic v1.TrafficSplit,
) error {
    def := &v1.LogicFlowDefinition{}
    if err := r.Get(ctx, types.NamespacedName{
        Name: traffic.DefinitionRef.Name,
        Namespace: svc.Namespace,
    }, def); err != nil {
        return err
    }
    
    annotations := r.buildAnnotations(svc, def)
    // Add canary annotations
    annotations["nginx.ingress.kubernetes.io/canary"] = "true"
    annotations["nginx.ingress.kubernetes.io/canary-weight"] = fmt.Sprintf("%d", traffic.Weight)
    
    ingress := &networkingv1.Ingress{
        ObjectMeta: metav1.ObjectMeta{
            Name:        fmt.Sprintf("%s-%s-canary", svc.Name, def.Spec.Workflow.Version),
            Namespace:   svc.Namespace,
            Annotations: annotations,
        },
        Spec: r.buildIngressSpec(svc, def),
    }
    
    return r.Create(ctx, ingress)
}
```

### Gradual Rollout Strategy

**Step 1: Deploy new version (0% traffic)**
```yaml
traffic:
  - definitionRef: {name: payment-v1-0-0}
    weight: 100
  - definitionRef: {name: payment-v1-1-0}
    weight: 0  # Deployed but not serving traffic
```

**Step 2: Canary (10% traffic)**
```yaml
traffic:
  - definitionRef: {name: payment-v1-0-0}
    weight: 90
  - definitionRef: {name: payment-v1-1-0}
    weight: 10
```

**Step 3: Expand (50% traffic)**
```yaml
traffic:
  - definitionRef: {name: payment-v1-0-0}
    weight: 50
  - definitionRef: {name: payment-v1-1-0}
    weight: 50
```

**Step 4: Full rollout (100% traffic)**
```yaml
traffic:
  - definitionRef: {name: payment-v1-1-0}
    weight: 100
```

**Step 5: Cleanup (remove old version)**
```yaml
# Delete payment-v1-0-0 LogicFlowDefinition
```

---

## Implementation Phases

### Phase 1: Basic Ingress (v1.0)

**Scope:**
- Single version per service
- Basic Ingress creation
- Path rewriting
- Nginx support only

**Features:**
- ✅ Create Ingress with rewrite annotations
- ✅ Forward reference (Service → Definition)
- ✅ Validation webhook
- ❌ No TLS support
- ❌ No traffic splitting
- ❌ Single Ingress controller only

### Phase 2: TLS Support (v1.1)

**Scope:**
- User-provided certificates
- Basic cert-manager integration

**Features:**
- ✅ TLS termination
- ✅ cert-manager annotations
- ✅ HTTP → HTTPS redirect
- ❌ No traffic splitting yet

### Phase 3: Traffic Splitting (v1.2)

**Scope:**
- Canary deployments
- Weighted routing

**Features:**
- ✅ Multiple versions per service
- ✅ Nginx canary annotations
- ✅ Weight validation (sum = 100)
- ✅ Gradual rollout support

### Phase 4: Multi-Controller (v2.0)

**Scope:**
- Support multiple Ingress controllers
- OpenShift Routes

**Features:**
- ✅ Nginx, Traefik, HAProxy support
- ✅ OpenShift Route creation
- ✅ Auto-detection of controller
- ✅ Platform-specific optimizations

---

## User Workflows

### Workflow 1: Basic Exposure

**Goal:** Expose a workflow externally with HTTPS

**Steps:**

1. Create Runtime
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: payments-runtime
spec:
  image: quay.io/kubesmarts/quarkus-flow:2.0.0
  replicas: 3
```

2. Create Definition
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: payment-processor-v1-0-0
spec:
  runtimeRef:
    name: payments-runtime
  workflow:
    name: payment-processor
    version: 1.0.0
    tasks: [...]
```

3. Create Service
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  runtimeRef:
    name: payments-runtime
  traffic:
    - definitionRef:
        name: payment-processor-v1-0-0
      weight: 100
  ingress:
    enabled: true
    host: payment-processor.prod.cluster.com
    tls:
      enabled: true
      certManager:
        issuerRef:
          name: letsencrypt-prod
```

4. Access
```bash
curl -X POST https://payment-processor.prod.cluster.com/invoke \
  -H "Content-Type: application/json" \
  -d '{"amount": 100}'
```

### Workflow 2: Canary Deployment

**Goal:** Deploy new version with gradual rollout

**Steps:**

1. Deploy new version (existing service still at 100% v1.0.0)
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: payment-processor-v1-1-0
spec:
  runtimeRef:
    name: payments-runtime
  workflow:
    name: payment-processor
    version: 1.1.0
    tasks: [...]
```

2. Start canary (10% traffic to new version)
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  traffic:
    - definitionRef: {name: payment-processor-v1-0-0}
      weight: 90
    - definitionRef: {name: payment-processor-v1-1-0}
      weight: 10  # Canary
```

3. Monitor metrics, increase gradually
```yaml
# 50/50 split
traffic:
  - definitionRef: {name: payment-processor-v1-0-0}
    weight: 50
  - definitionRef: {name: payment-processor-v1-1-0}
    weight: 50
```

4. Complete rollout
```yaml
# 100% to new version
traffic:
  - definitionRef: {name: payment-processor-v1-1-0}
    weight: 100
```

5. Cleanup
```bash
# Delete old definition
kubectl delete logicflowdefinition payment-processor-v1-0-0
```

### Workflow 3: Internal Workflow (No Service)

**Goal:** Create a workflow that's only called internally

**Steps:**

1. Create Definition (no Service)
```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: fraud-check-v1-0-0
spec:
  runtimeRef:
    name: payments-runtime
  workflow:
    name: fraud-check
    version: 1.0.0
    # Internal workflow - no external access needed
```

2. Call from another workflow
```yaml
# payment-processor workflow calls fraud-check internally
tasks:
  - name: checkFraud
    function: fraud-check  # Internal call, no HTTP
```

**Result:** Workflow loaded in runtime but NOT exposed externally.

---

## Platform Compatibility

### KIND (Local Development)

```bash
# 1. Install nginx-ingress
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml

# 2. Wait for controller
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s

# 3. Create LogicFlowService
kubectl apply -f payment-service.yaml

# 4. Port-forward
kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80 8443:443

# 5. Test (add to /etc/hosts: 127.0.0.1 payment-processor.local)
curl -H "Host: payment-processor.local" http://localhost:8080
```

### Minikube

```bash
# 1. Enable ingress addon
minikube addons enable ingress

# 2. Create LogicFlowService
kubectl apply -f payment-service.yaml

# 3. Get Minikube IP
minikube ip  # Example: 192.168.49.2

# 4. Add to /etc/hosts
echo "192.168.49.2 payment-processor.prod.cluster.com" >> /etc/hosts

# 5. Test
curl https://payment-processor.prod.cluster.com
```

### Production Kubernetes

```bash
# 1. Install Ingress controller (if not present)
# Nginx:
helm install ingress-nginx ingress-nginx/ingress-nginx

# Traefik:
helm install traefik traefik/traefik

# 2. Install cert-manager (optional, for auto-TLS)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# 3. Create ClusterIssuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
      - http01:
          ingress:
            class: nginx
EOF

# 4. Create LogicFlowService with cert-manager
kubectl apply -f payment-service.yaml
```

### OpenShift

```bash
# 1. Operator auto-detects OpenShift

# 2. Create LogicFlowService (same YAML)
oc apply -f payment-service.yaml

# 3. Operator creates Route (not Ingress)

# 4. Get Route
oc get route payment-processor

# 5. OpenShift auto-provisions TLS
# Uses default wildcard cert or edge termination
```

**Operator detects OpenShift and creates Route:**
```go
if r.isOpenShift() {
    return r.createRoute(ctx, svc)  // Creates Route
} else {
    return r.createIngress(ctx, svc)  // Creates Ingress
}
```

---

## Testing Checklist

### Basic Functionality

- [ ] Service creates Ingress with correct rewrite annotations
- [ ] External URL maps to internal Quarkus Flow path
- [ ] Multiple services can reference same runtime
- [ ] Validation rejects invalid traffic weights
- [ ] Validation rejects mismatched runtimes
- [ ] Definitions can exist without Services

### TLS

- [ ] User-provided certificate works
- [ ] cert-manager auto-generates certificate
- [ ] HTTP redirects to HTTPS
- [ ] TLS termination at Ingress
- [ ] Multiple hosts with different certs

### Traffic Splitting

- [ ] Single version (100% traffic)
- [ ] Two versions (canary split)
- [ ] Three+ versions (multi-way split)
- [ ] Weight changes take effect
- [ ] Weights must sum to 100

### Platform Compatibility

- [ ] Works on KIND with port-forward
- [ ] Works on Minikube with Ingress addon
- [ ] Works on production K8s with nginx-ingress
- [ ] Works on production K8s with Traefik
- [ ] Works on OpenShift with Routes

### Edge Cases

- [ ] Definition deleted while referenced by Service
- [ ] Service updated to remove all traffic entries
- [ ] Runtime deleted while Services reference it
- [ ] Multiple Services reference same Definition
- [ ] Service without Ingress enabled

---

## References

- [Knative Serving Traffic Management](https://knative.dev/docs/serving/traffic-management/)
- [Nginx Ingress Canary Deployments](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Istio VirtualService](https://istio.io/latest/docs/reference/config/networking/virtual-service/)
- [OpenShift Routes](https://docs.openshift.com/container-platform/latest/networking/routes/route-configuration.html)
