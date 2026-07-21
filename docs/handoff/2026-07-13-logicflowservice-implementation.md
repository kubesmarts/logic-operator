# LogicFlowService Implementation Handoff

**Date:** 2026-07-13  
**Branch:** `feat/issue-4-crd-implementation`  
**Issue:** #4 - Logic Operator v2 API Design

## Summary

Completed implementation of LogicFlowService CRD API types with full documentation. This provides external HTTP access to workflows via Ingress/Route with traffic splitting, TLS, and controller selection.

## What Was Completed

### 1. LogicFlowService API Types (`api/v1/logicflowservice_types.go`)

**Full implementation with:**

- **Traffic Management**
  - `Traffic []TrafficSpec` - Canary deployment with weighted routing
  - `DefaultVersion string` - Simple 100% routing to single version
  - Mutually exclusive validation rules
  - `TotalWeight()` helper method

- **Ingress Configuration**
  - `ControllerType` field with enum validation (nginx, openshift)
  - Auto-detection when not specified (OpenShift → openshift, K8s → nginx)
  - Platform-specific immutability (immutable on OpenShift, mutable on K8s)
  - `IngressClassName` for controller selection
  - User annotations support
  - Host configuration

- **TLS Support (3 modes)**
  - Manual: `SecretRef` - User-provided certificate
  - Auto: `CertManager` - cert-manager integration
  - None: `Enabled: false` - HTTP only

- **cert-manager Integration**
  - `CertManagerSpec` type
  - `CertManagerIssuerRef` with name, kind, group fields
  - Follows cert-manager's ObjectReference pattern
  - No namespace field (Issuer = same namespace, ClusterIssuer = cluster-wide)
  - Defaults: kind=ClusterIssuer, group=cert-manager.io

- **Status Fields**
  - ObservedGeneration
  - IngressRef / RouteRef (platform-specific)
  - URL (external)
  - Traffic status array
  - Conditions

- **Kubebuilder Markers**
  - Validation: Enum, MinItems, Minimum/Maximum
  - Print columns: Host, Runtime, TLS, Age
  - Resource scope: Namespaced
  - Short names: lfs, flowservice

- **Documentation**
  - Concise type-level docs (1-2 sentences)
  - One-line field comments
  - Mutual exclusivity notes
  - Platform behavior notes

### 2. Implementation Pattern Documentation

**Created:** `docs/implementation/logicflowservice-pattern.md` (400+ lines)

**Comprehensive guide covering:**

- **Relationship Model**
  - 1 Runtime : N Services : N Definitions
  - Forward-only references (Service → Definition)
  - Optional pattern (workflows don't need Services)

- **Path Rewriting Strategy**
  - External stable URLs → Internal Quarkus Flow paths
  - Controller-specific annotations (nginx, traefik, OpenShift)
  - Operator builds rewrite paths from namespace + workflow + version

- **Validation Strategy**
  - Service webhook validates definitions exist
  - Same runtime requirement
  - Workflow name consistency
  - Weight validation (sum = 100)
  - TLS mutual exclusivity

- **Controller Support**
  - v1.0: nginx + openshift
  - v1.1+: traefik, haproxy
  - Auto-detection logic
  - Platform-specific handling

- **TLS Configuration**
  - User certificates via Secret
  - cert-manager auto-generation
  - Certificate resource creation flow

- **Traffic Splitting**
  - Nginx canary annotations
  - Primary + canary Ingress pattern
  - Weight distribution

- **Platform Compatibility**
  - KIND, Minikube, K8s, OpenShift
  - Platform detection
  - Resource type selection (Ingress vs Route)

## Key Design Decisions

### 1. Controller Type Field

**Decision:** Explicit `controllerType` field with auto-detection

**Rationale:**
- Avoid backwards compatibility issues when adding new controllers
- Users can override auto-detection
- Clear API contract

**Immutability:**
- **OpenShift:** Immutable (Route ≠ Ingress, different resource types)
- **K8s:** Mutable (same Ingress resource, only annotations change)

### 2. cert-manager Integration

**Decision:** Follow cert-manager's ObjectReference pattern exactly

**Fields:**
- `name` - Issuer/ClusterIssuer name
- `kind` - Issuer or ClusterIssuer (default: ClusterIssuer)
- `group` - API group (default: cert-manager.io)
- **No namespace** - Issuer = same namespace as Certificate (implicit)

**Rationale:**
- Matches cert-manager API conventions
- Supports custom issuers via group field
- Namespace isolation for Issuers
- Cluster-wide access for ClusterIssuers

### 3. Traffic vs DefaultVersion

**Decision:** Two mutually exclusive fields

**Traffic array:**
- Canary deployments
- Gradual rollouts
- Weights must sum to 100

**DefaultVersion:**
- Simple single-version routing
- Auto 100% traffic
- Less YAML verbosity

**Validation:** Webhook ensures only one is specified

### 4. Forward-Only References

**Pattern:** Service → Definition (not bidirectional)

**Benefits:**
- Clear ownership (Service owns routing)
- Simple validation (Service webhook checks definitions)
- Independent definitions (no Service awareness)
- Flexible routing (Service can change traffic without Definition updates)

**Models:** Knative Serving, Istio VirtualService

## What's Pending

### 1. Controller Implementation

**Need to implement:**

```
internal/controller/logicflowservice_controller.go
├── Reconcile loop
├── Traffic resolution (Traffic vs DefaultVersion)
├── Platform detection (isOpenShift)
├── Controller type resolution (auto-detect vs explicit)
├── Ingress creation (nginx)
├── Route creation (OpenShift)
├── Certificate creation (cert-manager)
└── Status updates
```

**Key functions needed:**
- `resolveControllerType(ctx, svc) (string, error)`
- `resolveTraffic(svc) ([]TrafficSpec, error)`
- `createNginxIngress(ctx, svc) error`
- `createRoute(ctx, svc) error`
- `createCertificate(ctx, svc) error`
- `buildRewritePath(svc, def) string`

### 2. Validation Webhook

**Need to implement:**

```
api/v1/logicflowservice_webhook.go
├── ValidateCreate
│   ├── Traffic OR DefaultVersion (not both, not neither)
│   ├── Traffic weights sum to 100
│   ├── All definitions exist
│   ├── All definitions target same runtime
│   ├── All definitions have same workflow name
│   └── TLS: SecretRef OR CertManager (not both)
├── ValidateUpdate
│   ├── Same as ValidateCreate
│   └── Platform-specific immutability (OpenShift: controllerType immutable)
└── ValidateDelete (no-op)
```

### 3. Status Implementation

**Fields to populate:**

```go
Status.ObservedGeneration  // Set to svc.Generation after reconcile
Status.IngressRef         // Reference to created Ingress (K8s)
Status.RouteRef          // Reference to created Route (OpenShift)
Status.URL               // https://host from ingress/route
Status.Traffic           // Current traffic distribution with ready status
Status.Conditions        // Ready, IngressReady, TLSReady, etc.
```

**Condition types:**
- `Ready` - Overall service readiness
- `IngressReady` / `RouteReady` - External access ready
- `TLSReady` - Certificate issued (if cert-manager)
- `DefinitionsReady` - All referenced definitions exist

### 4. E2E Tests

**Test scenarios needed:**

1. **Simple exposure (DefaultVersion)**
   - Create LogicFlowService with defaultVersion
   - Verify Ingress created with 100% traffic
   - Verify path rewriting annotations

2. **Traffic splitting (Traffic array)**
   - Create LogicFlowService with traffic weights (80/20)
   - Verify canary Ingress annotations
   - Verify traffic distribution

3. **TLS with cert-manager**
   - Create LogicFlowService with certManager
   - Verify Certificate resource created
   - Verify Secret generated
   - Verify Ingress references Secret

4. **TLS with user cert**
   - Create LogicFlowService with secretRef
   - Verify Ingress references existing Secret

5. **Platform detection**
   - Test on K8s (Ingress created)
   - Test on OpenShift (Route created)

6. **Controller type switching (K8s only)**
   - Create with controllerType=nginx
   - Update to controllerType=traefik (v1.1+)
   - Verify annotations updated

7. **Validation failures**
   - Traffic + DefaultVersion (both specified)
   - Neither Traffic nor DefaultVersion
   - Traffic weights ≠ 100
   - Definition not found
   - Definition targets different runtime
   - TLS: both secretRef and certManager

### 5. Documentation

**Still needed:**

- [ ] User guide for LogicFlowService
- [ ] Migration guide (if upgrading from v1alpha08)
- [ ] Troubleshooting guide
- [ ] Examples directory with common patterns
- [ ] API reference (auto-generated from godoc)

## Important Notes

### 1. Namespace Scoping

**All resources must be in the same namespace:**
- LogicFlowService
- LogicFlowRuntime
- LogicFlowDefinitions
- Secrets (TLS certs, API keys)
- Issuers (if using cert-manager Issuer not ClusterIssuer)

**Exception:** ClusterIssuers are cluster-scoped (can be referenced from any namespace)

### 2. Path Rewriting Pattern

**Operator builds internal path from:**
- Namespace: `svc.Namespace`
- Workflow name: `def.Spec.Workflow.Name`
- Version: `def.Spec.Workflow.Version`

**Result:** `/q/flow/exec/{namespace}/{workflow}/{version}/{path}`

**Example:**
```
External: POST https://payment.prod.com/invoke
Internal: POST http://runtime.ns.svc:8080/q/flow/exec/ns/payment/v1.0.0/invoke
```

### 3. Traffic Weight Resolution

**If Traffic specified:**
- Use as-is
- Validate weights sum to 100

**If DefaultVersion specified:**
- Create synthetic TrafficSpec: `{DefinitionRef: defaultVersion, Weight: 100}`
- Simplifies controller logic (always work with Traffic array)

**If neither:**
- Validation webhook rejects (required field)

### 4. Platform-Specific Behavior

**OpenShift:**
- Creates Route (not Ingress)
- controllerType immutable (cannot change from openshift)
- Uses HAProxy annotations

**Kubernetes:**
- Creates Ingress
- controllerType mutable (can switch between nginx/traefik/etc)
- Uses controller-specific annotations

### 5. cert-manager Certificate Lifecycle

**Operator responsibilities:**
1. Create Certificate resource
2. Set ownerReference to LogicFlowService
3. Point Certificate.spec.secretName to generated name
4. Set Certificate.spec.issuerRef from user config
5. Set Certificate.spec.dnsNames from ingress.host

**cert-manager responsibilities:**
1. Validate Certificate
2. Create ACME challenge (HTTP-01 or DNS-01)
3. Get cert from CA
4. Store cert in Secret
5. Renew before expiry

**Operator uses the Secret:**
- Wait for Secret to exist (cert-manager created it)
- Reference Secret in Ingress.spec.tls

## Files Changed

```
api/v1/logicflowservice_types.go           # API type implementation
api/v1/zz_generated.deepcopy.go            # Generated (make manifests)
config/crd/bases/logic.kubesmarts.org_logicflowservices.yaml  # Generated CRD
docs/implementation/logicflowservice-pattern.md  # Implementation guide
```

## Next Steps

### Immediate (Day 1)

1. **Review this handoff**
   - Verify design decisions align with requirements
   - Check API types match expectations
   - Validate controller support (nginx + openshift for v1.0)

2. **Implement controller skeleton**
   ```bash
   # Controller already scaffolded, need to implement Reconcile
   vi internal/controller/logicflowservice_controller.go
   ```

3. **Implement validation webhook**
   ```bash
   kubebuilder create webhook --group logic --version v1 --kind LogicFlowService --defaulting --programmatic-validation
   ```

### Short-term (Week 1)

4. **Implement basic reconciliation**
   - Platform detection
   - Traffic resolution
   - Ingress creation (nginx only)

5. **Write unit tests**
   - Traffic resolution logic
   - Path rewriting
   - Platform detection

6. **Test on KIND**
   - Deploy operator
   - Create LogicFlowService
   - Verify Ingress created

### Medium-term (Week 2-3)

7. **Add OpenShift support**
   - Route creation
   - Test on OpenShift cluster or CRC

8. **Add cert-manager support**
   - Certificate creation
   - Wait for Secret
   - Ingress TLS configuration

9. **Write E2E tests**
   - See "E2E Tests" section above

10. **User documentation**
    - Getting started guide
    - Examples
    - Troubleshooting

### Long-term (v1.1+)

11. **Add more controllers**
    - Traefik
    - HAProxy
    - Contour

12. **Advanced features**
    - HTTP header routing
    - Request mirroring
    - Timeouts/retries

## Questions to Resolve

### Before Implementation

1. **Should we support IngressClassName on OpenShift?**
   - OpenShift uses Routes, not Ingress
   - Field would be ignored
   - Current: ignored with comment in docs

2. **Should we validate workflow name consistency?**
   - Currently: optional validation (helps prevent user errors)
   - Alternative: trust users to group correctly
   - Recommendation: validate (fail fast)

3. **How to handle cert-manager not installed?**
   - Option A: Webhook validation checks for cert-manager CRDs
   - Option B: Controller sets condition to "CertManagerNotFound"
   - Recommendation: Controller condition (more user-friendly)

4. **Should DefaultVersion reference be validated?**
   - Currently: yes, webhook checks definition exists
   - Ensures no dead references
   - Matches Traffic array validation

5. **Status.Traffic: should we show 0-weight entries?**
   - Scenario: definition exists but has 0% traffic
   - Option A: show all definitions (even 0%)
   - Option B: only show >0% traffic
   - Recommendation: show all (matches spec)

## Reference Links

- **Issue:** https://github.com/kubesmarts/logic-operator/issues/4
- **ADR (if exists):** Link to architecture decision record
- **Quarkus Flow docs:** https://docs.quarkiverse.io/quarkus-flow/dev/
- **cert-manager docs:** https://cert-manager.io/docs/
- **Nginx Ingress docs:** https://kubernetes.github.io/ingress-nginx/
- **OpenShift Route docs:** https://docs.openshift.com/container-platform/latest/networking/routes/

## Contact

**For questions about this implementation:**
- Check `docs/implementation/logicflowservice-pattern.md` first
- Review API types in `api/v1/logicflowservice_types.go`
- Refer to this handoff document

**Key principles:**
- Follow existing ApplicationSpec pattern (composition not embedding)
- Keep docs concise (see CLAUDE.md guidelines)
- No license headers on new files (automated script handles it)
- Ask before git operations (CLAUDE.md workflow rules)
