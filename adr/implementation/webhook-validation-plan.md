# Webhook Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add validating admission webhooks for `LogicFlowDefinition` and `LogicFlowRuntime` CRDs to reject invalid CRs at admission time instead of at reconciliation time.

**Architecture:** Each CRD gets a typed `admission.Validator[T]` implementation (controller-runtime v0.24 generics API) registered via `builder.WebhookManagedBy()`. Webhook infrastructure (cert-manager + kustomize config) is scaffolded manually following Kubebuilder v4 patterns. Validation logic reuses existing functions (`ParseFlow()`, `ValidateRunnerImage()`) — no new parsing or image-matching code.

**Tech Stack:** Go 1.26, controller-runtime v0.24.1, Kubebuilder v4, cert-manager (already deployed in e2e cluster), kustomize

## Global Constraints

- controller-runtime v0.24 uses the generic `admission.Validator[T]` interface — NOT the deprecated `CustomValidator` alias
- Registration: `builder.WebhookManagedBy(mgr, &logicv1.Type{}).WithValidator(&validator{}).Complete()`
- No defaulting/mutating webhooks in this scope
- No license headers on new files (automated script handles this)
- `config/webhook/` and `config/certmanager/` directories do not exist yet — must be created
- The webhook server is already scaffolded in `cmd/main.go:132-134` — no manager changes needed
- cert-manager is already installed in e2e test environment
- `make manifests` regenerates webhook manifests from kubebuilder markers — the `manifests.yaml` created in Task 1 will be overwritten and that's expected

---

### Task 1: Webhook Infrastructure + LogicFlowDefinition Webhook

Creates `config/webhook/`, `config/certmanager/`, uncomments kustomize wiring, and implements the `LogicFlowDefinition` validating webhook with two rules: (1) `ParseFlow()` must succeed on Create, (2) `spec` is immutable on Update.

**Files:**
- Create: `api/v1/logicflowdefinition_webhook.go`
- Create: `api/v1/logicflowdefinition_webhook_test.go`
- Create: `config/webhook/manifests.yaml` (placeholder — `make manifests` regenerates)
- Create: `config/webhook/service.yaml`
- Create: `config/webhook/kustomization.yaml`
- Create: `config/webhook/kustomizeconfig.yaml`
- Create: `config/certmanager/certificate.yaml`
- Create: `config/certmanager/kustomization.yaml`
- Create: `config/certmanager/kustomizeconfig.yaml`
- Create: `config/default/manager_webhook_patch.yaml`
- Modify: `config/default/kustomization.yaml` (uncomment webhook + certmanager sections)
- Modify: `config/crd/kustomization.yaml` (uncomment webhook configuration)
- Modify: `cmd/main.go:232-233` (register webhook before scaffold marker)

**Interfaces:**
- Consumes: `LogicFlowDefinitionSpec.ParseFlow()` from `api/v1/logicflowdefinition_types.go:47-56`
- Produces: `LogicFlowDefinitionValidator` struct implementing `admission.Validator[*logicv1.LogicFlowDefinition]`

#### Step 1: Create config/webhook directory

Create `config/webhook/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/managed-by: kustomize
  name: webhook-service
  namespace: system
spec:
  ports:
    - port: 443
      protocol: TCP
      targetPort: 9443
  selector:
    control-plane: controller-manager
```

Create `config/webhook/kustomization.yaml`:

```yaml
resources:
- manifests.yaml
- service.yaml
```

Create `config/webhook/kustomizeconfig.yaml`:

```yaml
# the following config is for teaching kustomize where to look at when substituting vars.
# It requires kustomize v2.1.0 or newer to work properly.
nameReference:
- kind: Service
  version: v1
  fieldSpecs:
  - kind: MutatingWebhookConfiguration
    group: admissionregistration.k8s.io
    path: webhooks/clientConfig/service/name
  - kind: ValidatingWebhookConfiguration
    group: admissionregistration.k8s.io
    path: webhooks/clientConfig/service/name

namespace:
- kind: MutatingWebhookConfiguration
  group: admissionregistration.k8s.io
  path: webhooks/clientConfig/service/namespace
  create: true
- kind: ValidatingWebhookConfiguration
  group: admissionregistration.k8s.io
  path: webhooks/clientConfig/service/namespace
  create: true
```

Create `config/webhook/manifests.yaml` (placeholder, `make manifests` regenerates from markers):

```yaml
# This file is a placeholder. Run `make manifests` to generate from kubebuilder markers.
```

- [ ] Create all four files above.

#### Step 2: Create config/certmanager directory

Create `config/certmanager/certificate.yaml`:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/managed-by: kustomize
  name: serving-cert
  namespace: system
spec:
  dnsNames:
  - $(SERVICE_NAME).$(SERVICE_NAMESPACE).svc
  - $(SERVICE_NAME).$(SERVICE_NAMESPACE).svc.cluster.local
  issuerRef:
    kind: Issuer
    name: selfsigned-issuer
  secretName: webhook-server-cert
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/managed-by: kustomize
  name: selfsigned-issuer
  namespace: system
spec:
  selfSigned: {}
```

Create `config/certmanager/kustomization.yaml`:

```yaml
resources:
- certificate.yaml
```

Create `config/certmanager/kustomizeconfig.yaml`:

```yaml
# This file is for teaching kustomize how to do var substitution
nameReference:
- kind: Issuer
  group: cert-manager.io
  fieldSpecs:
  - kind: Certificate
    group: cert-manager.io
    path: spec/issuerRef/name
```

- [ ] Create all three files above.

#### Step 3: Create manager_webhook_patch.yaml

Create `config/default/manager_webhook_patch.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    spec:
      containers:
      - name: manager
        ports:
        - containerPort: 9443
          name: webhook-server
          protocol: TCP
        volumeMounts:
        - mountPath: /tmp/k8s-webhook-server/serving-certs
          name: cert
          readOnly: true
      volumes:
      - name: cert
        secret:
          defaultMode: 420
          secretName: webhook-server-cert
```

- [ ] Create the file above.

#### Step 4: Uncomment kustomize webhook sections

In `config/default/kustomization.yaml`, uncomment these lines:

1. Resource lines (around lines 23-25):
   ```yaml
   - ../webhook
   - ../certmanager
   ```

2. Webhook patch (around lines 53-55):
   ```yaml
   - path: manager_webhook_patch.yaml
     target:
       kind: Deployment
   ```

3. The `replacements:` block and the following sections for cert-manager CA injection (lines 59-186). Uncomment **only** the blocks relevant to ValidatingWebhook and webhook service — NOT the MutatingWebhookConfiguration, DefaultingWebhook, or ConversionWebhook blocks.

   Specifically uncomment:
   - The `replacements:` key itself
   - The webhook service `name` and `namespace` source/target blocks (lines 120-155)
   - The ValidatingWebhookConfiguration CA injection blocks (lines 157-186)

In `config/crd/kustomization.yaml`, uncomment the webhook configuration (lines 18-19):

```yaml
configurations:
- kustomizeconfig.yaml
```

- [ ] Uncomment all listed sections. Leave MutatingWebhookConfiguration, DefaultingWebhook, and ConversionWebhook blocks commented.

#### Step 5: Write the failing webhook test

Create `api/v1/logicflowdefinition_webhook_test.go`:

```go
package v1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func validFlowRaw() runtime.RawExtension {
	return runtime.RawExtension{
		Raw: []byte(`{
			"document": {
				"dsl": "1.0.0",
				"namespace": "examples",
				"name": "hello-world",
				"version": "1.0.0"
			},
			"do": [
				{
					"setGreeting": {
						"set": {
							"greeting": ".\"Hello, \" + .name + \"!\""
						}
					}
				}
			]
		}`),
	}
}

func invalidFlowRaw() runtime.RawExtension {
	return runtime.RawExtension{
		Raw: []byte(`{"not": "a valid workflow"}`),
	}
}

func TestLogicFlowDefinitionValidator_ValidateCreate(t *testing.T) {
	v := &LogicFlowDefinitionValidator{}
	ctx := context.Background()

	tests := []struct {
		name    string
		obj     *LogicFlowDefinition
		wantErr bool
	}{
		{
			name: "valid flow passes",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: "my-runtime"},
					Flow:       validFlowRaw(),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid flow rejected",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: "my-runtime"},
					Flow:       invalidFlowRaw(),
				},
			},
			wantErr: true,
		},
		{
			name: "empty flow rejected",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: "my-runtime"},
					Flow:       runtime.RawExtension{},
				},
			},
			wantErr: true,
		},
		{
			name: "empty runtimeRef.name rejected",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: ""},
					Flow:       validFlowRaw(),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateCreate(ctx, tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogicFlowDefinitionValidator_ValidateUpdate(t *testing.T) {
	v := &LogicFlowDefinitionValidator{}
	ctx := context.Background()

	base := &LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "my-def", Namespace: "default"},
		Spec: LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "my-runtime"},
			Flow:       validFlowRaw(),
		},
	}

	tests := []struct {
		name    string
		oldObj  *LogicFlowDefinition
		newObj  *LogicFlowDefinition
		wantErr bool
	}{
		{
			name:    "no-op update allowed",
			oldObj:  base,
			newObj:  base.DeepCopy(),
			wantErr: false,
		},
		{
			name:   "flow change rejected",
			oldObj: base,
			newObj: func() *LogicFlowDefinition {
				n := base.DeepCopy()
				n.Spec.Flow.Raw = []byte(`{"different": true}`)
				return n
			}(),
			wantErr: true,
		},
		{
			name:   "runtimeRef change rejected",
			oldObj: base,
			newObj: func() *LogicFlowDefinition {
				n := base.DeepCopy()
				n.Spec.RuntimeRef.Name = "other-runtime"
				return n
			}(),
			wantErr: true,
		},
		{
			name:   "metadata-only update allowed",
			oldObj: base,
			newObj: func() *LogicFlowDefinition {
				n := base.DeepCopy()
				n.Labels = map[string]string{"team": "platform"}
				return n
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateUpdate(ctx, tt.oldObj, tt.newObj)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogicFlowDefinitionValidator_ValidateDelete(t *testing.T) {
	v := &LogicFlowDefinitionValidator{}
	_, err := v.ValidateDelete(context.Background(), &LogicFlowDefinition{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}
```

- [ ] Create the test file.

#### Step 6: Run test to verify it fails

Run: `go test ./api/v1/ -run TestLogicFlowDefinitionValidator -v`

Expected: Compilation failure — `LogicFlowDefinitionValidator` does not exist yet.

- [ ] Verify compilation failure.

#### Step 7: Write the webhook implementation

Create `api/v1/logicflowdefinition_webhook.go`:

```go
package v1

import (
	"context"
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowdefinition,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=create;update,versions=v1,name=vlogicflowdefinition-v1.kb.io,admissionReviewVersions=v1

type LogicFlowDefinitionValidator struct{}

var _ admission.Validator[*LogicFlowDefinition] = &LogicFlowDefinitionValidator{}

func (v *LogicFlowDefinitionValidator) ValidateCreate(_ context.Context, obj *LogicFlowDefinition) (admission.Warnings, error) {
	if obj.Spec.RuntimeRef.Name == "" {
		return nil, fmt.Errorf("spec.runtimeRef.name is required")
	}
	if _, err := obj.Spec.ParseFlow(); err != nil {
		return nil, fmt.Errorf("spec.flow: %w", err)
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateUpdate(_ context.Context, oldObj, newObj *LogicFlowDefinition) (admission.Warnings, error) {
	if !reflect.DeepEqual(oldObj.Spec, newObj.Spec) {
		return nil, fmt.Errorf("spec is immutable; delete and recreate to change the workflow definition")
	}
	return nil, nil
}

func (v *LogicFlowDefinitionValidator) ValidateDelete(_ context.Context, _ *LogicFlowDefinition) (admission.Warnings, error) {
	return nil, nil
}
```

- [ ] Create the file.

#### Step 8: Fix the test — add missing import

The test file needs `corev1` imported for `corev1.LocalObjectReference`. Update the import in `logicflowdefinition_webhook_test.go`:

```go
import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)
```

- [ ] Add the `corev1` import.

#### Step 9: Run tests to verify they pass

Run: `go test ./api/v1/ -run TestLogicFlowDefinitionValidator -v`

Expected: All tests pass.

- [ ] Verify all tests pass.

#### Step 10: Register webhook in main.go

In `cmd/main.go`, add the webhook registration just before the `// +kubebuilder:scaffold:builder` marker (line 233):

```go
	if err := builder.WebhookManagedBy(mgr, &logicv1.LogicFlowDefinition{}).
		WithValidator(&logicv1.LogicFlowDefinitionValidator{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "LogicFlowDefinition")
		os.Exit(1)
	}
```

Add the `builder` import:

```go
	"sigs.k8s.io/controller-runtime/pkg/builder"
```

- [ ] Add registration and import.

#### Step 11: Regenerate manifests

Run: `make manifests`

This regenerates `config/webhook/manifests.yaml` from the `+kubebuilder:webhook` marker. Verify the generated file contains a `ValidatingWebhookConfiguration` with the correct path.

- [ ] Run `make manifests` and verify output.

#### Step 12: Run full test suite

Run: `make test`

Expected: All existing tests still pass, plus the new webhook tests.

- [ ] Verify full test suite passes.

#### Step 13: Commit

```bash
git add api/v1/logicflowdefinition_webhook.go \
        api/v1/logicflowdefinition_webhook_test.go \
        cmd/main.go \
        config/webhook/ \
        config/certmanager/ \
        config/default/manager_webhook_patch.yaml \
        config/default/kustomization.yaml \
        config/crd/kustomization.yaml
git commit -m "feat: add LogicFlowDefinition validating webhook with infrastructure scaffolding

Validates flow document parsing at admission time using ParseFlow().
Enforces spec immutability on updates (delete and recreate pattern).
Scaffolds config/webhook/ and config/certmanager/ for cert-manager TLS."
```

- [ ] Commit.

---

### Task 2: LogicFlowRuntime Validating Webhook

Implements cross-field validation for `LogicFlowRuntime`: (1) security type must match sub-struct presence (`API_KEY` requires `apiKey`, `OIDC` requires `oidc`), (2) image variant must be consistent with persistence config (reuses existing `ValidateRunnerImage` logic). The image validation logic is moved from `internal/controller/` to `api/v1/` so the webhook can call it without importing the controller package.

**Files:**
- Create: `api/v1/logicflowruntime_webhook.go`
- Create: `api/v1/logicflowruntime_webhook_test.go`
- Create: `api/v1/validation.go`
- Modify: `internal/controller/quarkus_config.go:70-83` (delegate to `api/v1` validation)
- Modify: `internal/controller/quarkus_constants.go` (export image constants needed by validation)
- Modify: `cmd/main.go` (register second webhook)

**Interfaces:**
- Consumes: `ValidateRunnerImage()` — moved to `api/v1/validation.go`
- Consumes: `RuntimeSecuritySpec`, `PersistenceOptionsSpec` from `api/v1/logicflowruntime_types.go`
- Produces: `LogicFlowRuntimeValidator` struct implementing `admission.Validator[*logicv1.LogicFlowRuntime]`

#### Step 1: Move image validation to api/v1

Create `api/v1/validation.go`. This extracts the image validation logic so both the webhook and the controller can use it without circular imports:

```go
package v1

import (
	"fmt"
	"strings"
)

const (
	FlowRunnerRegistry     = "quay.io/quarkiverse"
	FlowRunnerImage        = "quarkus-flow-runner"
	ImageVariantMinimal    = "minimal"
	ImageVariantStandard   = "standard"
)

func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, FlowRunnerRegistry+"/"+FlowRunnerImage+":")
}

func hasPersistence(p *PersistenceOptionsSpec) bool {
	return p != nil && p.PostgreSQL != nil
}

func ValidateRunnerImage(image string, persistence *PersistenceOptionsSpec) error {
	if !isKnownRunnerImage(image) {
		return nil
	}

	if strings.HasSuffix(image, "-"+ImageVariantMinimal) && hasPersistence(persistence) {
		return fmt.Errorf("image %q does not support persistence; use the %s variant or remove persistence config", image, ImageVariantStandard)
	}
	if strings.HasSuffix(image, "-"+ImageVariantStandard) && !hasPersistence(persistence) {
		return fmt.Errorf("image %q requires persistence configuration; set spec.persistence or use the %s variant", image, ImageVariantMinimal)
	}

	return nil
}

func ValidateSecuritySpec(sec RuntimeSecuritySpec) error {
	switch sec.Type {
	case RuntimeSecurityAPIKey:
		if sec.APIKey == nil || len(sec.APIKey.Keys) == 0 {
			return fmt.Errorf("spec.security.apiKey.keys is required when security type is API_KEY")
		}
	case RuntimeSecurityOIDC:
		if sec.OIDC == nil {
			return fmt.Errorf("spec.security.oidc is required when security type is OIDC")
		}
		if sec.OIDC.AuthServerUrl == "" {
			return fmt.Errorf("spec.security.oidc.authServerUrl is required when security type is OIDC")
		}
		if sec.OIDC.ClientId == "" {
			return fmt.Errorf("spec.security.oidc.clientId is required when security type is OIDC")
		}
	}
	return nil
}
```

- [ ] Create the file.

#### Step 2: Update the controller to use the moved function

In `internal/controller/quarkus_config.go`, replace the `ValidateRunnerImage` function body (lines 70-83) to delegate to the moved function:

```go
func ValidateRunnerImage(image string, persistence *logicv1.PersistenceOptionsSpec) error {
	return logicv1.ValidateRunnerImage(image, persistence)
}
```

Also update `isKnownRunnerImage` and `hasPersistence` to delegate:

```go
func isKnownRunnerImage(image string) bool {
	return strings.HasPrefix(image, QuarkusFlowRegistry+"/"+QuarkusFlowRunner+":")
}
```

Note: `isKnownRunnerImage` in the controller uses the controller's own constants (`QuarkusFlowRegistry`, `QuarkusFlowRunner`) while `ValidateRunnerImage` uses the API package's. Since both sets must match, update `quarkus_constants.go` to reference the API constants:

```go
const (
	QuarkusFlowRegistry = logicv1.FlowRunnerRegistry
	QuarkusFlowRunner   = logicv1.FlowRunnerImage
	// ... rest unchanged
)
```

This ensures the values stay in sync.

- [ ] Update `quarkus_config.go` to delegate `ValidateRunnerImage`.
- [ ] Update `quarkus_constants.go` to reference `api/v1` constants.

#### Step 3: Write the failing webhook test

Create `api/v1/logicflowruntime_webhook_test.go`:

```go
package v1

import (
	"context"
	"fmt"
	"testing"
)

func TestLogicFlowRuntimeValidator_ValidateCreate(t *testing.T) {
	v := &LogicFlowRuntimeValidator{}
	ctx := context.Background()

	tests := []struct {
		name    string
		obj     *LogicFlowRuntime
		wantErr bool
	}{
		{
			name: "empty spec passes (defaults to NONE, no image)",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{},
			},
			wantErr: false,
		},
		{
			name: "API_KEY with apiKey spec passes",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					Security: RuntimeSecuritySpec{
						Type: RuntimeSecurityAPIKey,
						APIKey: &APIKeyAuthSpec{
							Keys: []APIKeySpec{{
								Name:      "test-key",
								SecretRef: SecretKeySelector{Name: "my-secret"},
								Roles:     []RuntimeSecurityRole{RuntimeSecurityRoleInvoker},
							}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "API_KEY without apiKey spec rejected",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					Security: RuntimeSecuritySpec{
						Type: RuntimeSecurityAPIKey,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "OIDC with oidc spec passes",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					Security: RuntimeSecuritySpec{
						Type: RuntimeSecurityOIDC,
						OIDC: &OIDCAuthSpec{
							AuthServerUrl: "https://keycloak.example.com/realms/flow",
							ClientId:      "flow-client",
							ClientSecret:  SecretKeySelector{Name: "oidc-secret"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "OIDC without oidc spec rejected",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					Security: RuntimeSecuritySpec{
						Type: RuntimeSecurityOIDC,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "OIDC with empty authServerUrl rejected",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					Security: RuntimeSecuritySpec{
						Type: RuntimeSecurityOIDC,
						OIDC: &OIDCAuthSpec{
							AuthServerUrl: "",
							ClientId:      "flow-client",
							ClientSecret:  SecretKeySelector{Name: "oidc-secret"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "minimal image with persistence rejected",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					RuntimeSpec: RuntimeSpec{
						ApplicationSpec: ApplicationSpec{
							Image: fmt.Sprintf("%s/%s:0.15.1-%s", FlowRunnerRegistry, FlowRunnerImage, ImageVariantMinimal),
						},
						Persistence: &PersistenceOptionsSpec{
							PostgreSQL: &PostgreSQLPersistenceOptions{
								SecretRef: PostgreSQLSecretOptions{Name: "pg-secret"},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "standard image without persistence rejected",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					RuntimeSpec: RuntimeSpec{
						ApplicationSpec: ApplicationSpec{
							Image: fmt.Sprintf("%s/%s:0.15.1-%s", FlowRunnerRegistry, FlowRunnerImage, ImageVariantStandard),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "custom image with persistence passes (no validation)",
			obj: &LogicFlowRuntime{
				Spec: LogicFlowRuntimeSpec{
					RuntimeSpec: RuntimeSpec{
						ApplicationSpec: ApplicationSpec{
							Image: "my-registry.io/custom-runner:latest",
						},
						Persistence: &PersistenceOptionsSpec{
							PostgreSQL: &PostgreSQLPersistenceOptions{
								SecretRef: PostgreSQLSecretOptions{Name: "pg-secret"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateCreate(ctx, tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogicFlowRuntimeValidator_ValidateUpdate(t *testing.T) {
	v := &LogicFlowRuntimeValidator{}
	ctx := context.Background()

	t.Run("same validations run on update", func(t *testing.T) {
		oldObj := &LogicFlowRuntime{Spec: LogicFlowRuntimeSpec{}}
		newObj := &LogicFlowRuntime{
			Spec: LogicFlowRuntimeSpec{
				Security: RuntimeSecuritySpec{
					Type: RuntimeSecurityAPIKey,
				},
			},
		}
		_, err := v.ValidateUpdate(ctx, oldObj, newObj)
		if err == nil {
			t.Error("ValidateUpdate() expected error for API_KEY without keys")
		}
	})
}

func TestLogicFlowRuntimeValidator_ValidateDelete(t *testing.T) {
	v := &LogicFlowRuntimeValidator{}
	_, err := v.ValidateDelete(context.Background(), &LogicFlowRuntime{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}
```

- [ ] Create the test file.

#### Step 4: Run test to verify it fails

Run: `go test ./api/v1/ -run TestLogicFlowRuntimeValidator -v`

Expected: Compilation failure — `LogicFlowRuntimeValidator` does not exist yet.

- [ ] Verify compilation failure.

#### Step 5: Write the webhook implementation

Create `api/v1/logicflowruntime_webhook.go`:

```go
package v1

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-logic-kubesmarts-org-v1-logicflowruntime,mutating=false,failurePolicy=fail,sideEffects=None,groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=create;update,versions=v1,name=vlogicflowruntime-v1.kb.io,admissionReviewVersions=v1

type LogicFlowRuntimeValidator struct{}

var _ admission.Validator[*LogicFlowRuntime] = &LogicFlowRuntimeValidator{}

func (v *LogicFlowRuntimeValidator) ValidateCreate(_ context.Context, obj *LogicFlowRuntime) (admission.Warnings, error) {
	return v.validate(obj)
}

func (v *LogicFlowRuntimeValidator) ValidateUpdate(_ context.Context, _, newObj *LogicFlowRuntime) (admission.Warnings, error) {
	return v.validate(newObj)
}

func (v *LogicFlowRuntimeValidator) ValidateDelete(_ context.Context, _ *LogicFlowRuntime) (admission.Warnings, error) {
	return nil, nil
}

func (v *LogicFlowRuntimeValidator) validate(obj *LogicFlowRuntime) (admission.Warnings, error) {
	if err := ValidateSecuritySpec(obj.Spec.Security); err != nil {
		return nil, err
	}
	if obj.Spec.Image != "" {
		if err := ValidateRunnerImage(obj.Spec.Image, obj.Spec.Persistence); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
```

- [ ] Create the file.

#### Step 6: Run tests to verify they pass

Run: `go test ./api/v1/ -run TestLogicFlowRuntimeValidator -v`

Expected: All tests pass.

- [ ] Verify all tests pass.

#### Step 7: Register webhook in main.go

In `cmd/main.go`, add the second webhook registration just before the `// +kubebuilder:scaffold:builder` marker, after the LogicFlowDefinition webhook:

```go
	if err := builder.WebhookManagedBy(mgr, &logicv1.LogicFlowRuntime{}).
		WithValidator(&logicv1.LogicFlowRuntimeValidator{}).
		Complete(); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "LogicFlowRuntime")
		os.Exit(1)
	}
```

- [ ] Add registration.

#### Step 8: Regenerate manifests and run full test suite

Run:

```bash
make manifests
make test
```

Verify `config/webhook/manifests.yaml` now contains a `ValidatingWebhookConfiguration` with two webhooks (one for each CRD). All tests pass.

- [ ] Verify manifests and full test suite.

#### Step 9: Remove TODO(webhook) comment

In `internal/controller/quarkus_config.go`, remove the `TODO(webhook)` comment from the `ValidateRunnerImage` function since the webhook is now implemented:

Before:
```go
// ValidateRunnerImage checks that a known runner image variant is consistent with the persistence config.
// Custom images (different registry/repo) skip validation.
// TODO(webhook): wire into a ValidatingAdmissionWebhook for LogicFlowRuntime to reject invalid CRs at admission time.
```

After:
```go
// ValidateRunnerImage delegates to the api/v1 validation for backward compatibility.
```

- [ ] Remove the TODO comment.

#### Step 10: Commit

```bash
git add api/v1/logicflowruntime_webhook.go \
        api/v1/logicflowruntime_webhook_test.go \
        api/v1/validation.go \
        internal/controller/quarkus_config.go \
        internal/controller/quarkus_constants.go \
        cmd/main.go \
        config/webhook/manifests.yaml
git commit -m "feat: add LogicFlowRuntime validating webhook

Cross-validates security type against apiKey/oidc sub-struct presence.
Cross-validates image variant against persistence config using
ValidateRunnerImage() moved to api/v1 for shared access."
```

- [ ] Commit.

---

### Task 3: E2e Webhook Validation Tests

Adds e2e tests verifying that the admission webhooks reject invalid CRs at API server level. These tests apply invalid YAML and expect the API server to return admission errors.

**Files:**
- Create: `test/e2e/webhook_test.go`

**Interfaces:**
- Consumes: Deployed operator with webhook certs (the existing e2e deploy target handles this if kustomize config is wired correctly)

**Note:** This task assumes the e2e deploy pipeline (`make deploy`) uses `config/default/kustomization.yaml` with the newly uncommented webhook sections. cert-manager must be installed first (already true in the e2e setup). The webhook needs a few seconds after deploy to have its cert injected — add a wait for webhook readiness before running validation tests.

#### Step 1: Write e2e webhook test file

Create `test/e2e/webhook_test.go`:

```go
package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func webhookValidationTests() {

	Context("LogicFlowDefinition webhook", func() {

		It("should reject a definition with an invalid flow document", func() {
			invalidDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: invalid-flow-def
  namespace: %s
spec:
  runtimeRef:
    name: some-runtime
  flow:
    not: "a valid workflow"`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for invalid flow")
			Expect(string(output)).To(ContainSubstring("spec.flow"))
		})

		It("should reject a definition with empty runtimeRef.name", func() {
			emptyRefDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: empty-ref-def
  namespace: %s
spec:
  runtimeRef:
    name: ""
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(emptyRefDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for empty runtimeRef.name")
			Expect(string(output)).To(ContainSubstring("runtimeRef.name"))
		})

		It("should reject spec update on an existing definition", func() {
			// Create a valid definition first
			validDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: immutable-def
  namespace: %s
spec:
  runtimeRef:
    name: some-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: immutable-test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(validDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "expected valid definition to be created: %s", string(output))

			// Try to update spec
			updatedDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: immutable-def
  namespace: %s
spec:
  runtimeRef:
    name: other-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: immutable-test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(updatedDef, namespace))
			output, err = cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected spec update to be rejected")
			Expect(string(output)).To(ContainSubstring("immutable"))

			// Cleanup
			_ = exec.Command("kubectl", "delete", "logicflowdefinition", "immutable-def", "-n", namespace, "--ignore-not-found").Run()
		})
	})

	Context("LogicFlowRuntime webhook", func() {

		It("should reject API_KEY security type without apiKey spec", func() {
			invalidRT := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: invalid-security-rt
  namespace: %s
spec:
  security:
    type: API_KEY`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidRT, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for API_KEY without keys")
			Expect(string(output)).To(ContainSubstring("apiKey"))
		})

		It("should reject OIDC security type without oidc spec", func() {
			invalidRT := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: invalid-oidc-rt
  namespace: %s
spec:
  security:
    type: OIDC`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidRT, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for OIDC without config")
			Expect(string(output)).To(ContainSubstring("oidc"))
		})
	})
}
```

- [ ] Create the test file.

#### Step 2: Wire into e2e test suite

In `test/e2e/e2e_test.go`, add a call to `webhookValidationTests()` inside the `Describe("Manager", ...)` block, after the existing runtime lifecycle tests:

```go
		webhookValidationTests()
```

Add the necessary imports (`os/exec`, `strings`) if not already present.

- [ ] Wire test function into suite.

#### Step 3: Run e2e tests

Run: `make test-e2e`

Expected: All existing tests pass plus the new webhook validation tests.

- [ ] Verify all e2e tests pass.

#### Step 4: Commit

```bash
git add test/e2e/webhook_test.go test/e2e/e2e_test.go
git commit -m "test: add e2e tests for webhook admission validation

Covers invalid flow rejection, spec immutability enforcement,
and security type cross-field validation at API server level."
```

- [ ] Commit.
