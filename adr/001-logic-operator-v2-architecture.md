# Logic Operator v2.0 - Design Specification

**Date**: 2026-06-22  
**Status**: Draft  
**Migration Strategy**: Clean Slate - New Operator

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Architecture Overview](#architecture-overview)
3. [Migration Strategy](#migration-strategy)
4. [CRD Specifications](#crd-specifications)
5. [Data Index Architecture](#data-index-architecture)
6. [Versioning Model](#versioning-model)
7. [Deployment Topologies](#deployment-topologies)
8. [Removed Components](#removed-components)
9. [Implementation Phases](#implementation-phases)

---

## Executive Summary

### Project Goals

Refactor the Logic Operator to:
1. **Drop SonataFlow** in favor of **Quarkus Flow** runtime
2. **Support Serverless Workflow Specification v1.0.0** exclusively
3. **Remove builder/container complexity** - use dynamic workflow loading
4. **Implement new Data Index (MODE1)** - FluentBit + PostgreSQL + GraphQL
5. **Support multi-version workflows** - side-by-side version execution
6. **Upgrade to latest Operator SDK**

### Core Principles

- **Clean Slate**: New operator, no backward compatibility burden
- **Runtime Model Shift**: From per-workflow containers to shared runners
- **Specification Support**: Only SW DSL v1.0.0 (drop v0.8 entirely)
- **Operational Simplicity**: Leverage Quarkus Flow's immutability and lease-based durability

---

## Architecture Overview

### Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Logic Operator v2.0                       │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  CRDs (all namespace-scoped):                                │
│  ├── LogicPlatform                                           │
│  ├── LogicFlowService                                        │
│  ├── LogicFlowDefinition                                     │
│  └── LogicFlowRuntime                                        │
│                                                               │
│  Controllers:                                                 │
│  ├── LogicPlatformController                                 │
│  │   ├── Manages FluentBit DaemonSet (Data Index)           │
│  │   ├── Manages Data Index Service (GraphQL API)           │
│  │   └── Provides runtime defaults                          │
│  │                                                            │
│  ├── LogicFlowServiceController                              │
│  │   ├── Creates Ingress (traffic routing)                  │
│  │   ├── Manages traffic splitting                          │
│  │   └── Aggregates version status                          │
│  │                                                            │
│  ├── LogicFlowDefinitionController                           │
│  │   ├── Updates Runtime ConfigMap (workflow YAML)          │
│  │   ├── Validates immutability                             │
│  │   ├── Queries Data Index (active instances)              │
│  │   └── Blocks deletion if instances running               │
│  │                                                            │
│  └── LogicFlowRuntimeController                              │
│      ├── Creates Deployment (Quarkus Flow Runner)           │
│      ├── Creates Service (HTTP API)                         │
│      ├── Creates ConfigMap (workflow definitions)           │
│      ├── Creates Leases (durable coordination)              │
│      └── Manages RBAC (ServiceAccount, Role, RoleBinding)   │
│                                                               │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                   Runtime Components                         │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  Quarkus Flow Runner (Pod):                                  │
│  ├── Immutable - loads workflows from ConfigMap at startup  │
│  ├── Exposes REST API (/q/flow/exec/<ns>/<name>/<version>)  │
│  ├── Uses Kubernetes Lease for durable workflow identity    │
│  ├── Persists state to PostgreSQL (via Data Index)          │
│  └── Emits CloudEvents as JSON logs to stdout               │
│                                                               │
│  FluentBit (DaemonSet per namespace):                        │
│  ├── Tails /var/log/containers/*_<namespace>_*.log          │
│  ├── Filters: eventType=io.serverlessworkflow.*             │
│  └── Streams to PostgreSQL staging tables                   │
│                                                               │
│  PostgreSQL (user-provided):                                 │
│  ├── Staging: workflow_instance_events (raw JSONB)          │
│  ├── Triggers: Normalize into final tables                  │
│  ├── Final: workflow_instances, task_executions             │
│  └── Schema managed externally (DBMigrator deferred)         │
│                                                               │
│  Data Index Service (Deployment):                            │
│  ├── GraphQL API: /graphql, /graphql-ui                     │
│  ├── Queries PostgreSQL via JPA                             │
│  └── Used by operator to check active instances             │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Runtime Model Shift

**OLD (SonataFlow)**:
```
WorkflowCR → Build Container Image → Deploy Dedicated Pod per Workflow
```

**NEW (LogicFlow)**:
```
WorkflowDefinitionCR → Mount to ConfigMap → Shared/Dedicated Runner Pod
```

---

## Migration Strategy

### Approach: Clean Slate - New Operator

**No backward compatibility** - users migrate manually at their own pace.

#### Characteristics

- **New operator binary**: `logic-operator v2.0.0`
- **New CRDs**: LogicFlowService, LogicFlowDefinition, LogicFlowRuntime, LogicPlatform
- **No old CRDs**: SonataFlow v1alpha08 completely removed from this codebase
- **Side-by-side deployment**: New operator can run alongside old SonataFlow operator (if users still have it installed)
- **Manual migration**: Users rewrite workflow CRs to new format

#### Timeline

- **Phase 1 (Months 1-3)**: Develop Logic Operator v2.0.0
- **Phase 2 (Months 4-15)**: Deprecation window - both operators supported
- **Phase 3 (Month 16+)**: SonataFlow operator deprecated, users fully migrated

#### Benefits

✅ **Cleanest architecture** - no legacy code  
✅ **No conversion webhooks** - simpler codebase  
✅ **Separate release cycle** - v2 doesn't break v1 users  
✅ **Clear "old vs new" story** - easier to document  
✅ **Allows gradual adoption** - teams migrate at their own pace

#### Trade-offs

❌ **Manual migration work** for users  
❌ **No automated state transfer** - workflow instances lost  
❌ **Two operators running** during transition (resource overhead)

---

## CRD Specifications

### 1. LogicPlatform

**Scope**: Namespace  
**Purpose**: Manages Data Index infrastructure and runtime defaults

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform
  namespace: prod
spec:
  # Deployment mode
  mode: local  # local | central | remote (default: local)
  
  # Reference to central platform (mode=remote only)
  dataIndexRef:
    name: logic-platform-central
    namespace: logic-infra
  
  # Data Index configuration
  dataIndex:
    enabled: true
    
    # FluentBit DaemonSet (mode=local or remote)
    fluentBit:
      image: fluent/fluent-bit:3.0
      resources:
        requests:
          memory: "128Mi"
          cpu: "100m"
        limits:
          memory: "512Mi"
          cpu: "500m"
      nodeSelector: {}
      tolerations: []
    
    # Data Index Service (mode=local or central)
    service:
      image: quay.io/kubesmarts/logic-data-index:1.0.0
      replicas: 2
      resources:
        requests:
          memory: "256Mi"
          cpu: "250m"
        limits:
          memory: "512Mi"
          cpu: "500m"
      autoscaling:
        enabled: false
        minReplicas: 2
        maxReplicas: 10
        targetCPUUtilizationPercentage: 70
      graphql:
        enabled: true
        ui: true
        path: /graphql
    
    # PostgreSQL connection
    postgresql:
      host: postgres.prod.svc.cluster.local
      port: 5432
      database: dataindex
      secretRef:
        name: postgres-credentials
        keys:
          username: username
          password: password
      ssl:
        enabled: true
        mode: require  # disable | allow | prefer | require
      pool:
        maxSize: 20
        minSize: 5
  
  # Default LogicFlowRuntime template
  runtimeDefaults:
    image: quay.io/kubesmarts/quarkus-flow-runner:1.0.0
    imagePullPolicy: IfNotPresent
    resources:
      requests:
        memory: "256Mi"
        cpu: "250m"
      limits:
        memory: "1Gi"
        cpu: "1"
    security:
      type: API_KEY  # NONE | API_KEY | OIDC
    durable:
      enabled: true

status:
  mode: local
  
  # Data Index status
  dataIndex:
    fluentBit:
      ready: true
      daemonSetRef:
        name: logic-platform-fluentbit
      nodes: 3
      metricsEndpoint: http://logic-platform-fluentbit.prod.svc:2020/api/v1/metrics/prometheus
    service:
      ready: true
      deploymentRef:
        name: logic-platform-dataindex
      serviceRef:
        name: logic-platform-dataindex
      graphqlEndpoint: http://logic-platform-dataindex.prod.svc:8080/graphql
      graphqlUI: http://logic-platform-dataindex.prod.svc:8080/graphql-ui
      metricsEndpoint: http://logic-platform-dataindex.prod.svc:8080/q/metrics
    postgresql:
      connected: true
      schemaReady: true
      schemaVersion: "1.0.0"
  
  # Connected namespaces (mode=central only)
  connectedNamespaces: []
  
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2026-06-22T10:00:00Z"
  - type: FluentBitReady
    status: "True"
  - type: DataIndexServiceReady
    status: "True"
  - type: PostgreSQLReady
    status: "True"
```

---

### 2. LogicFlowService

**Scope**: Namespace  
**Purpose**: Groups workflow versions, manages traffic routing and ingress

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: payment-processor
  namespace: prod
spec:
  # Runtime reference
  runtimeRef:
    name: payments-runtime
  
  # Traffic management (optional - defaults to latest version)
  traffic:
  - definitionRef:
      name: payment-processor-v1-0-0
    weight: 80
  - definitionRef:
      name: payment-processor-v1-1-0
    weight: 20
  
  # Default version when traffic not specified
  defaultVersion: "latest"  # latest | <semver>
  
  # Ingress configuration
  ingress:
    enabled: true
    host: payment-processor.prod.cluster.com
    tls:
      enabled: true
      secretName: wildcard-tls
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
      nginx.ingress.kubernetes.io/ssl-redirect: "true"

status:
  # Service endpoint
  url: https://payment-processor.prod.cluster.com
  
  # Version summary
  versions:
  - name: payment-processor-v1-0-0
    version: "1.0.0"
    weight: 80
    activeInstances: 15
    ready: true
  - name: payment-processor-v1-1-0
    version: "1.1.0"
    weight: 20
    activeInstances: 5
    ready: true
  
  conditions:
  - type: Ready
    status: "True"
  - type: IngressReady
    status: "True"
  - type: RuntimeReady
    status: "True"
```

---

### 3. LogicFlowDefinition

**Scope**: Namespace  
**Purpose**: Immutable workflow definition (version snapshot)

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowDefinition
metadata:
  name: payment-processor-v1-0-0
  namespace: prod
  
  # Workflow metadata (replaces document section)
  annotations:
    org.kubesmarts.logic/flow-name: payment-processor  # Workflow name (not CR name!)
    logic.kubesmarts.io/dsl-version: "1.0.0"  # SW DSL version
    logic.kubesmarts.io/title: "Payment Processing Workflow"
    logic.kubesmarts.io/summary: |
      Processes payment requests via payment gateway.
      Handles retries and notifications.
  
  # Tags as labels
  labels:
    logic.kubesmarts.io/service: payment-processor
    logic.kubesmarts.io/domain: payments
    logic.kubesmarts.io/team: platform

spec:
  # Parent service reference
  serviceRef:
    name: payment-processor
  
  # Workflow semantic version
  version: "1.0.0"
  
  # Workflow definition (ONLY workflow spec, no document!)
  # Operator constructs full SW document from metadata + definition
  definition:
    # Primary workflow tasks
    do:
    - processPayment:
        call: http
        with:
          method: POST
          endpoint: https://payment-gateway/charge
          body:
            amount: ${ .input.amount }
            currency: ${ .input.currency }
    
    - notifyCustomer:
        emit:
          event:
            type: com.example.payment.completed
            source: payment-processor
            data: ${ .output }
    
    # Reusable components (optional)
    use:
      authentications:
        gatewayAuth:
          oauth2:
            authority: https://auth.example.com
            grant: client_credentials
            client:
              id: payment-service
              secret: ${ .secrets.GATEWAY_SECRET }
      
      retries:
        defaultRetry:
          maxAttempts: 3
          delay: PT1S
          backoff:
            multiplier: 2
      
      errors:
        paymentError:
          type: https://example.com/errors/payment-failed
          status: 402
    
    # Input schema (optional)
    input:
      schema:
        format: json
        document:
          type: object
          required: [amount, currency]
          properties:
            amount:
              type: number
              minimum: 0
            currency:
              type: string
              enum: [USD, EUR, GBP]
    
    # Timeout (optional)
    timeout:
      after: PT30S

status:
  # Active workflow instances (queried from Data Index)
  activeInstances: 15
  
  # Lifecycle phase
  phase: Active  # Active | Draining | Decommissioned
  
  # Mounted to runtime
  runtimeRef:
    name: payments-runtime
    namespace: prod
  
  conditions:
  - type: Ready
    status: "True"
  - type: MountedToRuntime
    status: "True"
  - type: Decommissionable
    status: "False"
    message: "15 active instances running"
```

**Operator Behavior**: Constructs full Serverless Workflow document from metadata + spec:

```yaml
# Mounted to ConfigMap as payment-processor-v1.0.0.yaml:
document:
  dsl: '1.0.0'  # From annotation: logic.kubesmarts.io/dsl-version
  namespace: prod  # From metadata.namespace
  name: payment-processor  # From annotation: org.kubesmarts.logic/flow-name
  version: '1.0.0'  # From spec.version
  title: "Payment Processing Workflow"  # From annotation: logic.kubesmarts.io/title
  summary: |  # From annotation: logic.kubesmarts.io/summary
    Processes payment requests via payment gateway.
    Handles retries and notifications.
  tags:  # From labels (filtered by logic.kubesmarts.io/*)
    domain: payments
    team: platform
do:  # From spec.definition.do
  - processPayment: {...}
use:  # From spec.definition.use
  authentications: {...}
  retries: {...}
input:  # From spec.definition.input
  schema: {...}
timeout:  # From spec.definition.timeout
  after: PT30S
```

**Key Mapping**:
- `document.name` ← `annotations["org.kubesmarts.logic/flow-name"]` (NOT metadata.name!)
- `document.namespace` ← `metadata.namespace`
- `document.version` ← `spec.version`
- `document.dsl` ← `annotations["logic.kubesmarts.io/dsl-version"]`

---

### 4. LogicFlowRuntime

**Scope**: Namespace  
**Purpose**: Quarkus Flow Runner deployment and configuration

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowRuntime
metadata:
  name: payments-runtime
  namespace: prod
spec:
  # Quarkus Flow Runner configuration
  image: quay.io/kubesmarts/quarkus-flow-runner:1.0.0
  imagePullPolicy: IfNotPresent
  replicas: 3
  
  # Durable workflow coordination (lease-based)
  durable:
    enabled: true
    leasePoolName: payments-runtime
  
  # Security
  security:
    type: API_KEY  # NONE | API_KEY | OIDC
    secretRef:
      name: runner-api-keys
  
  # Resources per pod
  resources:
    requests:
      memory: "512Mi"
      cpu: "500m"
    limits:
      memory: "2Gi"
      cpu: "2"
  
  # Autoscaling
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
  
  # Pod template customization
  podTemplate:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
    spec:
      serviceAccountName: workflow-runtime
      securityContext:
        runAsNonRoot: true
        fsGroup: 1000
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app: logicflow-runtime
              topologyKey: kubernetes.io/hostname

status:
  # Deployment status
  phase: Running  # Pending | Running | Failed
  replicas: 3
  readyReplicas: 3
  
  # Managed workflow definitions
  definitions:
  - name: payment-processor-v1-0-0
    service: payment-processor
    version: "1.0.0"
  - name: payment-processor-v1-1-0
    service: payment-processor
    version: "1.1.0"
  - name: refund-processor-v1-0-0
    service: refund-processor
    version: "1.0.0"
  
  # Kubernetes resources
  deploymentRef:
    name: payments-runtime
  serviceRef:
    name: payments-runtime
  configMapRef:
    name: payments-runtime-workflows
  leasesReady: true
  
  conditions:
  - type: Ready
    status: "True"
  - type: DeploymentReady
    status: "True"
  - type: LeasesReady
    status: "True"
```

**Operator Creates**:

1. **Deployment** (Quarkus Flow Runner pods)
2. **Service** (HTTP API)
3. **ConfigMap** (workflow YAML files)
4. **Leases** (durable coordination - one per replica)
5. **ServiceAccount** + **Role** + **RoleBinding** (RBAC for leases)

---

## Data Index Architecture

### MODE1: FluentBit + PostgreSQL + GraphQL

#### Architecture Flow

```
Quarkus Flow Runner
  ↓ Emits JSON structured logs to stdout
Kubernetes /var/log/containers/*.log
  ↓ Container logs captured by K8s
FluentBit DaemonSet
  ├─ Tails /var/log/containers/*_<namespace>_*.log
  ├─ Filters: eventType=io.serverlessworkflow.*
  ├─ Routes by event type (started, completed, faulted)
  └─ INSERT into PostgreSQL staging tables
PostgreSQL
  ├─ Staging: workflow_instance_events (raw JSONB)
  ├─ Triggers: Extract & UPSERT into final tables
  └─ Final: workflow_instances, task_executions
Data Index Service
  ├─ GraphQL API: /graphql, /graphql-ui
  ├─ Queries: getWorkflowInstances, getTaskExecutions
  └─ Reads from PostgreSQL via JPA
```

#### Deployment Topologies

**Local Mode** (per-namespace isolation):
```
Namespace: prod
├── LogicPlatform (mode: local)
├── FluentBit DaemonSet → PostgreSQL prod
├── Data Index Service → PostgreSQL prod
└── LogicFlowRuntimes
```

**Centralized Mode** (multi-tenant):
```
Namespace: logic-infra
├── LogicPlatform (mode: central)
├── Data Index Service (scaled) → PostgreSQL (all namespaces)
└── PostgreSQL (shared)

Namespace: prod, staging, team-a
├── LogicPlatform (mode: remote, dataIndexRef → logic-infra)
├── FluentBit DaemonSet → PostgreSQL in logic-infra
└── LogicFlowRuntimes
```

#### PostgreSQL Schema

**Staging Tables** (FluentBit writes):
```sql
CREATE TABLE workflow_instance_events (
    tag VARCHAR(255),
    time TIMESTAMP,
    data JSONB  -- Complete Quarkus Flow event
);

CREATE TABLE task_execution_events (
    tag VARCHAR(255),
    time TIMESTAMP,
    data JSONB
);
```

**Final Tables** (Triggers normalize):
```sql
CREATE TABLE workflow_instances (
    id UUID PRIMARY KEY,
    namespace VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    start_date TIMESTAMP,
    end_date TIMESTAMP,
    last_update TIMESTAMP,
    input JSONB,
    output JSONB,
    error_type VARCHAR(255),
    error_title VARCHAR(255),
    error_detail TEXT,
    error_status INTEGER,
    error_instance VARCHAR(255)
);

CREATE TABLE task_executions (
    id UUID PRIMARY KEY,
    workflow_instance_id UUID REFERENCES workflow_instances(id),
    task_name VARCHAR(255),
    task_position VARCHAR(255),  -- JSON Pointer: "/do/0"
    enter TIMESTAMP,
    exit TIMESTAMP,
    error_message TEXT,
    input_args JSONB,
    output_args JSONB
);
```

**Triggers** handle out-of-order events via UPSERT + COALESCE.

#### GraphQL API

**Example Queries**:
```graphql
# List instances
{
  getWorkflowInstances(
    where: {
      namespace: {equal: "prod"},
      status: {in: [RUNNING, SUSPENDED]}
    }
  ) {
    id
    name
    version
    status
    startDate
  }
}

# Get instance details
{
  getWorkflowInstance(id: "uuid-1234") {
    id
    name
    status
    input
    output
    error {
      type
      title
      detail
    }
  }
}
```

**Operator Usage**:
```go
// Check active instances before deletion
func (r *LogicFlowDefinitionReconciler) canDelete(def *LogicFlowDefinition) (bool, error) {
    platform := getLogicPlatform(def.Namespace)
    graphqlURL := platform.Status.DataIndex.Service.GraphqlEndpoint
    
    query := `{
      getWorkflowInstances(
        where: {
          name: {equal: "%s"},
          version: {equal: "%s"},
          status: {in: [RUNNING, SUSPENDED]}
        }
      ) { id }
    }`
    
    instances := graphqlQuery(graphqlURL, fmt.Sprintf(query, 
        def.Spec.ServiceRef.Name, def.Spec.Version))
    
    return len(instances) == 0, nil
}
```

---

## Versioning Model

### Explicit Versions (No Auto-Revisions)

**Principle**: Kubernetes CRDs = Production operational resources

- No "SNAPSHOT" or "unpublished" states
- LogicFlowDefinition is immutable and production-ready when created
- Testing happens in dev/staging clusters
- Users explicitly create new LogicFlowDefinition for each version

### Version Lifecycle

#### 1. Deploy v1.0.0

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  runtimeRef:
    name: payments-runtime
---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowDefinition
metadata:
  name: payment-processor-v1-0-0
spec:
  serviceRef:
    name: payment-processor
  version: "1.0.0"
  definition:
    do: [...]
```

**URL**: `https://payment-processor.prod.cluster.com` → v1.0.0

---

#### 2. Deploy v1.1.0 (Canary)

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowDefinition
metadata:
  name: payment-processor-v1-1-0
spec:
  serviceRef:
    name: payment-processor
  version: "1.1.0"
  definition:
    do: [... new features ...]
```

**Enable canary**:
```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  traffic:
  - definitionRef:
      name: payment-processor-v1-0-0
    weight: 90
  - definitionRef:
      name: payment-processor-v1-1-0
    weight: 10  # 10% canary
```

**URLs**:
- `https://payment-processor/` → 90% v1.0.0, 10% v1.1.0 (weighted)
- `https://payment-processor/v1.0.0` → v1.0.0 (explicit)
- `https://payment-processor/v1.1.0` → v1.1.0 (explicit)

---

#### 3. Gradual Rollout

```bash
# Increase to 50/50
kubectl patch logicflowservice payment-processor --type=merge -p '
spec:
  traffic:
  - definitionRef: {name: payment-processor-v1-0-0}
    weight: 50
  - definitionRef: {name: payment-processor-v1-1-0}
    weight: 50
'

# Full rollout
kubectl patch logicflowservice payment-processor --type=merge -p '
spec:
  traffic:
  - definitionRef: {name: payment-processor-v1-1-0}
    weight: 100
'
```

---

#### 4. Decommission v1.0.0

```bash
# Check active instances via Data Index
kubectl get logicflowdefinition payment-processor-v1-0-0 \
  -o jsonpath='{.status.activeInstances}'
# Output: 0

# Delete (webhook blocks if activeInstances > 0)
kubectl delete logicflowdefinition payment-processor-v1-0-0
```

Operator removes from ConfigMap, runtime pods restart (Recreate strategy or RollingUpdate with maxUnavailable=1).

---

### Traffic Splitting Implementation (Ingress-Level)

**Operator generates Ingress with canary annotations**:

```yaml
# Main ingress (90% traffic to v1.0.0)
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor
  namespace: prod
spec:
  rules:
  - host: payment-processor.prod.cluster.com
    http:
      paths:
      - path: /
        backend:
          service:
            name: payments-runtime
            port: 8080
        # Operator configures rewrite to: /q/flow/exec/prod/payment-processor/1.0.0

---
# Canary ingress (10% traffic to v1.1.0)
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payment-processor-v1-1-0-canary
  namespace: prod
  annotations:
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-weight: "10"
spec:
  rules:
  - host: payment-processor.prod.cluster.com
    http:
      paths:
      - path: /
        backend:
          service:
            name: payments-runtime
            port: 8080
        # Rewrites to: /q/flow/exec/prod/payment-processor/1.1.0
```

---

### Webhook Protections

#### Prevent Accidental Deletion

```go
func (v *LogicFlowDefinitionValidator) ValidateDelete(def *LogicFlowDefinition) error {
    // Query Data Index for active instances
    activeInstances := getActiveInstances(def)
    
    if activeInstances > 0 {
        return fmt.Errorf("cannot delete: %d active instances. Wait for completion or use label logic.kubesmarts.io/force-delete=true", activeInstances)
    }
    
    return nil
}
```

#### Prevent Runner Migration with Active Instances

```go
func (v *LogicFlowServiceValidator) ValidateUpdate(old, new *LogicFlowService) error {
    if old.Spec.RuntimeRef.Name != new.Spec.RuntimeRef.Name {
        // Check all definitions for active instances
        for _, version := range old.Status.Versions {
            if version.ActiveInstances > 0 {
                return fmt.Errorf("cannot change runtimeRef: version %s has %d active instances. Migration blocked.", version.Version, version.ActiveInstances)
            }
        }
    }
    
    return nil
}
```

---

## Deployment Topologies

### 1. Development (1:1 Auto-Created Runners)

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform
  namespace: dev
spec:
  mode: local
  dataIndex:
    postgresql:
      host: postgres.dev.svc
      database: workflows_dev

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: hello-world
  namespace: dev
spec:
  # No runtimeRef → operator auto-creates dedicated runner

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowDefinition
metadata:
  name: hello-world-v1-0-0
spec:
  serviceRef:
    name: hello-world
  version: "1.0.0"
  definition:
    do:
    - greet:
        set:
          message: "Hello World"
```

**Operator auto-creates**:
- LogicFlowRuntime: `hello-world-runner` (1 replica, Recreate strategy)
- Service: `hello-world` (routes to runner)
- ConfigMap: `hello-world-runner-workflows`
- Ingress: `hello-world.dev.cluster.com`

---

### 2. Production (Shared Runner, HA)

```yaml
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform
  namespace: prod
spec:
  mode: local
  dataIndex:
    service:
      replicas: 3
      autoscaling:
        enabled: true
    postgresql:
      host: postgres-ha.prod.svc
      database: workflows

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowRuntime
metadata:
  name: payments-runtime
spec:
  replicas: 5
  durable:
    enabled: true
  autoscaling:
    enabled: true
    minReplicas: 5
    maxReplicas: 20

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: payment-processor
spec:
  runtimeRef:
    name: payments-runtime  # Shared

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicFlowService
metadata:
  name: refund-processor
spec:
  runtimeRef:
    name: payments-runtime  # Same shared runner
```

**Result**: Both workflows run in same 5-replica runner, load-balanced via lease-based sharding.

---

### 3. Multi-Tenant (Centralized Data Index)

```yaml
# Infrastructure namespace
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform-central
  namespace: logic-infra
spec:
  mode: central
  dataIndex:
    service:
      replicas: 10
      autoscaling:
        enabled: true
        minReplicas: 10
        maxReplicas: 50
    postgresql:
      host: postgres-central.logic-infra.svc
      database: dataindex
      pool:
        maxSize: 100

---
# Application namespaces reference central
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform
  namespace: prod
spec:
  mode: remote
  dataIndexRef:
    name: logic-platform-central
    namespace: logic-infra

---
apiVersion: logic.kubesmarts.io/v1
kind: LogicPlatform
metadata:
  name: logic-platform
  namespace: staging
spec:
  mode: remote
  dataIndexRef:
    name: logic-platform-central
    namespace: logic-infra
```

**Result**: All namespaces → Single scaled Data Index Service → Single PostgreSQL

---

## Removed Components

### From SonataFlow Operator

1. **Jobs Service** - Removed entirely (no replacement needed)
2. **Container Builder** - Removed (Quarkus Flow Runner is pre-built)
3. **SonataFlowBuild CRD** - Removed (no build step)
4. **Workflow project handler** - Removed (no source builds)
5. **Kaniko/Buildah integration** - Removed
6. **Dev mode** - Removed (use Quarkus dev mode in runner directly)
7. **v0.8 support** - Removed (only v1.0.0)

### Deferred for Later

1. **DBMigrator Job** - PostgreSQL schema management (manual for now)
2. **E2E tests (current approach)** - Will redesign testing strategy

---

## Implementation Phases

### Phase 1: Core CRDs & Controllers (Months 1-2)

**Deliverables**:
- LogicPlatform CRD + Controller (local mode only)
- LogicFlowRuntime CRD + Controller
- LogicFlowDefinition CRD + Controller
- LogicFlowService CRD + Controller
- Basic e2e test (single workflow, single version)

**Validation**:
- Deploy single workflow to dev cluster
- Verify runtime creates deployment + service + configmap
- Verify workflow executes via REST API
- Verify FluentBit → PostgreSQL → Data Index flow

---

### Phase 2: Data Index Integration (Month 2)

**Deliverables**:
- FluentBit DaemonSet generation
- Data Index Service deployment
- PostgreSQL connection management
- GraphQL API integration in controllers
- Active instance querying before deletion

**Validation**:
- Workflow events appear in Data Index
- GraphQL queries return instances
- Deletion blocked when instances active

---

### Phase 3: Versioning & Traffic Management (Month 3)

**Deliverables**:
- Multi-version support (multiple LogicFlowDefinitions per service)
- Traffic splitting (Ingress canary)
- Version lifecycle management
- Decommissioning workflow

**Validation**:
- Deploy v1.0.0 → Deploy v1.1.0 → Canary rollout → Decommission v1.0.0
- Verify traffic weights respected
- Verify deletion blocked with active instances

---

### Phase 4: Advanced Features (Months 4-5)

**Deliverables**:
- Centralized Data Index (mode: central + remote)
- Runtime autoscaling
- Durable workflow lease coordination validation
- Migration guide from SonataFlow

**Validation**:
- Multi-namespace setup with central Data Index
- Runner scaling under load
- Lease failover on pod crash

---

### Phase 5: Production Readiness (Month 6)

**Deliverables**:
- Security hardening (RBAC, pod security)
- Observability (metrics, logging, tracing)
- Documentation (user guide, API reference)
- OLM bundle for OperatorHub
- Upgrade testing

**Validation**:
- Security audit passed
- Prometheus metrics exposed
- Complete user documentation
- Installable via OLM

---

## End of Design Specification

**Next Steps**:
1. Review and approve design
2. Create implementation plan with task breakdown
3. Set up development environment
4. Begin Phase 1 implementation
