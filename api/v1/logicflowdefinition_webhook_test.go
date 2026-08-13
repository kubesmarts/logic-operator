package v1

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testRuntimeName = "my-runtime"
	testNamespace   = "default"
	testPGSecret    = "pg-secret"
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

func testValidator() *LogicFlowDefinitionValidator {
	scheme := runtime.NewScheme()
	_ = AddToScheme(scheme)
	return &LogicFlowDefinitionValidator{
		Reader: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}
}

func TestLogicFlowDefinitionValidator_ValidateCreate(t *testing.T) {
	v := testValidator()
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
					RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
					Flow:       validFlowRaw(),
				},
			},
			wantErr: false,
		},
		{
			name: "invalid flow rejected",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
					Flow:       invalidFlowRaw(),
				},
			},
			wantErr: true,
		},
		{
			name: "empty flow rejected",
			obj: &LogicFlowDefinition{
				Spec: LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
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
	v := testValidator()
	ctx := context.Background()

	base := &LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "my-def", Namespace: testNamespace},
		Spec: LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: testRuntimeName},
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
			name:   "valid flow change allowed",
			oldObj: base,
			newObj: func() *LogicFlowDefinition {
				n := base.DeepCopy()
				n.Spec.Flow = validFlowRaw()
				return n
			}(),
			wantErr: false,
		},
		{
			name:   "invalid flow change rejected",
			oldObj: base,
			newObj: func() *LogicFlowDefinition {
				n := base.DeepCopy()
				n.Spec.Flow.Raw = []byte(`{"not": "valid"}`)
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
	v := testValidator()
	_, err := v.ValidateDelete(context.Background(), &LogicFlowDefinition{})
	if err != nil {
		t.Errorf("ValidateDelete() unexpected error: %v", err)
	}
}
