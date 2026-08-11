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
							PostgreSQL: &PersistencePostgreSQL{
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
							PostgreSQL: &PersistencePostgreSQL{
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
