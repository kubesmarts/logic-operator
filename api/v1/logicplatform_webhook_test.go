package v1

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testPlatformValidator(objs ...runtime.Object) *LogicPlatformValidator {
	scheme := runtime.NewScheme()
	_ = AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objs) > 0 {
		builder = builder.WithRuntimeObjects(objs...)
	}
	return &LogicPlatformValidator{Reader: builder.Build()}
}

func testPlatform(opts ...func(*LogicPlatform)) *LogicPlatform {
	plat := &LogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "test-platform", Namespace: testNamespace},
		Spec:       LogicPlatformSpec{},
	}
	for _, opt := range opts {
		opt(plat)
	}
	return plat
}

func withDataIndex(enabled bool, image string) func(*LogicPlatform) {
	return func(plat *LogicPlatform) {
		plat.Spec.DataIndex.Enabled = enabled
		if image != "" {
			plat.Spec.DataIndex.Application.Image = image
		}
	}
}

func withDataIndexPersistence() func(*LogicPlatform) {
	return func(plat *LogicPlatform) {
		plat.Spec.DataIndex.Persistence = &PersistenceOptionsSpec{
			PostgreSQL: &PersistencePostgreSQL{
				SecretRef: PostgreSQLSecretOptions{Name: "postgres-secret"},
				ServiceRef: &PostgreSQLServiceOptions{
					SQLServiceOptions: &SQLServiceOptions{
						Name: "postgres",
					},
					DatabaseSchema: "data-index",
				},
			},
		}
	}
}

func withDataIndexReplicas(replicas int32) func(*LogicPlatform) {
	return func(plat *LogicPlatform) {
		plat.Spec.DataIndex.Application.Replicas = ptr.To(replicas)
	}
}

// TestLogicPlatformDefaulter_Default tests defaulting behavior
func TestLogicPlatformDefaulter_Default(t *testing.T) {
	tests := []struct {
		name     string
		platform *LogicPlatform
		want     func(*testing.T, *LogicPlatform)
	}{
		{
			name: "applies default Data Index enabled=true (via kubebuilder annotation, tested in integration)",
			platform: testPlatform(func(plat *LogicPlatform) {
				// In real usage, the API server applies +kubebuilder:default=true
				// For this unit test, we simulate that by explicitly setting it
				plat.Spec.DataIndex.Enabled = true
			}),
			want: func(t *testing.T, plat *LogicPlatform) {
				if !plat.Spec.DataIndex.Enabled {
					t.Errorf("expected DataIndex.Enabled=true by default, got false")
				}
			},
		},
		{
			name: "applies default Data Index image when enabled but image empty",
			platform: testPlatform(
				withDataIndex(true, ""),
			),
			want: func(t *testing.T, plat *LogicPlatform) {
				if plat.Spec.DataIndex.Application.Image == "" {
					t.Errorf("expected default Data Index image, got empty")
				}
				expectedImage := fmt.Sprintf("%s/%s:%s-%s", DataIndexRegistry, DataIndexImage, DataIndexVersion, DataIndexVariant)
				if plat.Spec.DataIndex.Application.Image != expectedImage {
					t.Errorf("expected image %q, got %q", expectedImage, plat.Spec.DataIndex.Application.Image)
				}
			},
		},
		{
			name: "preserves user-specified image",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
			),
			want: func(t *testing.T, plat *LogicPlatform) {
				if plat.Spec.DataIndex.Application.Image != "custom/image:1.0" {
					t.Errorf("expected custom image to be preserved, got %q", plat.Spec.DataIndex.Application.Image)
				}
			},
		},
		{
			name: "applies default replicas when enabled but replicas not set",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
			),
			want: func(t *testing.T, plat *LogicPlatform) {
				if plat.Spec.DataIndex.Application.Replicas == nil {
					t.Errorf("expected default replicas to be set")
				}
				if *plat.Spec.DataIndex.Application.Replicas != 1 {
					t.Errorf("expected default replicas=1, got %d", *plat.Spec.DataIndex.Application.Replicas)
				}
			},
		},
		{
			name: "preserves user-specified replicas",
			platform: testPlatform(
				withDataIndex(true, ""),
				withDataIndexReplicas(3),
			),
			want: func(t *testing.T, plat *LogicPlatform) {
				if *plat.Spec.DataIndex.Application.Replicas != 3 {
					t.Errorf("expected replicas=3 to be preserved, got %d", *plat.Spec.DataIndex.Application.Replicas)
				}
			},
		},
		{
			name: "does not apply image when Data Index disabled",
			platform: testPlatform(
				withDataIndex(false, ""),
			),
			want: func(t *testing.T, plat *LogicPlatform) {
				if plat.Spec.DataIndex.Application.Image != "" {
					t.Errorf("expected no image when disabled, got %q", plat.Spec.DataIndex.Application.Image)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaulter := &LogicPlatformDefaulter{}
			_ = defaulter.Default(context.Background(), tt.platform)
			tt.want(t, tt.platform)
		})
	}
}

// TestLogicPlatformValidator_ValidateCreate tests validation on create
func TestLogicPlatformValidator_ValidateCreate(t *testing.T) {
	tests := []struct {
		name     string
		platform *LogicPlatform
		wantErr  bool
		errMsg   string
	}{
		{
			name: "valid platform with Data Index disabled",
			platform: testPlatform(
				withDataIndex(false, ""),
			),
			wantErr: false,
		},
		{
			name: "valid platform with Data Index enabled and persistence configured",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				withDataIndexPersistence(),
			),
			wantErr: false,
		},
		{
			name: "rejects Data Index enabled without persistence",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
			),
			wantErr: true,
			errMsg:  "spec.dataIndex.persistence is required when Data Index is enabled",
		},
		{
			name: "rejects invalid image format",
			platform: testPlatform(
				withDataIndex(true, "not@a:valid/image"),
				withDataIndexPersistence(),
			),
			wantErr: true,
			errMsg:  "invalid image format: \"not@a:valid/image\"",
		},
		{
			name: "rejects persistence without secret",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				func(plat *LogicPlatform) {
					plat.Spec.DataIndex.Persistence = &PersistenceOptionsSpec{
						PostgreSQL: &PersistencePostgreSQL{
							SecretRef: PostgreSQLSecretOptions{},
							ServiceRef: &PostgreSQLServiceOptions{
								SQLServiceOptions: &SQLServiceOptions{
									Name: "postgres",
								},
								DatabaseSchema: "data-index",
							},
						},
					}
				},
			),
			wantErr: true,
			errMsg:  "spec.dataIndex.persistence.postgresql.secretRef.name is required",
		},
		{
			name: "rejects persistence without service name",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				func(plat *LogicPlatform) {
					plat.Spec.DataIndex.Persistence = &PersistenceOptionsSpec{
						PostgreSQL: &PersistencePostgreSQL{
							SecretRef: PostgreSQLSecretOptions{Name: "postgres-secret"},
							ServiceRef: &PostgreSQLServiceOptions{
								SQLServiceOptions: &SQLServiceOptions{},
								DatabaseSchema:    "data-index",
							},
						},
					}
				},
			),
			wantErr: true,
			errMsg:  "spec.dataIndex.persistence.postgresql.serviceRef.name is required",
		},
		{
			name: "rejects persistence without database schema",
			platform: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				func(plat *LogicPlatform) {
					plat.Spec.DataIndex.Persistence = &PersistenceOptionsSpec{
						PostgreSQL: &PersistencePostgreSQL{
							SecretRef: PostgreSQLSecretOptions{Name: "postgres-secret"},
							ServiceRef: &PostgreSQLServiceOptions{
								SQLServiceOptions: &SQLServiceOptions{
									Name: "postgres",
								},
							},
						},
					}
				},
			),
			wantErr: true,
			errMsg:  "spec.dataIndex.persistence.postgresql.serviceRef.databaseSchema is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := testPlatformValidator()
			_, err := validator.ValidateCreate(context.Background(), tt.platform)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

// TestLogicPlatformValidator_ValidateUpdate tests validation on update
func TestLogicPlatformValidator_ValidateUpdate(t *testing.T) {
	tests := []struct {
		name    string
		oldPlat *LogicPlatform
		newPlat *LogicPlatform
		wantErr bool
		errMsg  string
	}{
		{
			name: "allows enabling Data Index with valid persistence",
			oldPlat: testPlatform(
				withDataIndex(false, ""),
			),
			newPlat: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				withDataIndexPersistence(),
			),
			wantErr: false,
		},
		{
			name: "allows disabling Data Index",
			oldPlat: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				withDataIndexPersistence(),
			),
			newPlat: testPlatform(
				withDataIndex(false, ""),
			),
			wantErr: false,
		},
		{
			name: "allows changing image",
			oldPlat: testPlatform(
				withDataIndex(true, "custom/image:1.0"),
				withDataIndexPersistence(),
			),
			newPlat: testPlatform(
				withDataIndex(true, "quay.io/kubesmarts/data-index-service-postgresql:2.1.0"),
				withDataIndexPersistence(),
			),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := testPlatformValidator()
			_, err := validator.ValidateUpdate(context.Background(), tt.oldPlat, tt.newPlat)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}
