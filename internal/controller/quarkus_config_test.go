package controller

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

func intPtr(i int) *int { return &i }

func TestPersistenceEnvVars_NilReturnsNil(t *testing.T) {
	g := gomega.NewWithT(t)
	g.Expect(persistenceEnvVars(nil, "default")).To(gomega.BeNil())
	g.Expect(persistenceEnvVars(&logicv1.PersistenceOptionsSpec{}, "default")).To(gomega.BeNil())
}

func TestPersistenceEnvVars_JdbcUrlWithDefaultSecretKeys(t *testing.T) {
	g := gomega.NewWithT(t)
	p := &logicv1.PersistenceOptionsSpec{
		PostgreSQL: &logicv1.PersistencePostgreSQL{
			SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
			JdbcUrl:   "jdbc:postgresql://localhost:5432/mydb",
		},
	}

	envs := persistenceEnvVars(p, "default")
	g.Expect(envs).To(gomega.HaveLen(4))

	g.Expect(*envs[0].Name).To(gomega.Equal("QUARKUS_DATASOURCE_DB_KIND"))
	g.Expect(*envs[0].Value).To(gomega.Equal("postgresql"))

	g.Expect(*envs[1].Name).To(gomega.Equal("QUARKUS_DATASOURCE_USERNAME"))
	g.Expect(*envs[1].ValueFrom.SecretKeyRef.Name).To(gomega.Equal("pg-creds"))
	g.Expect(*envs[1].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("POSTGRESQL_USER"))

	g.Expect(*envs[2].Name).To(gomega.Equal("QUARKUS_DATASOURCE_PASSWORD"))
	g.Expect(*envs[2].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("POSTGRESQL_PASSWORD"))

	g.Expect(*envs[3].Name).To(gomega.Equal("QUARKUS_DATASOURCE_JDBC_URL"))
	g.Expect(*envs[3].Value).To(gomega.Equal("jdbc:postgresql://localhost:5432/mydb"))
}

func TestPersistenceEnvVars_CustomSecretKeys(t *testing.T) {
	g := gomega.NewWithT(t)
	p := &logicv1.PersistenceOptionsSpec{
		PostgreSQL: &logicv1.PersistencePostgreSQL{
			SecretRef: logicv1.PostgreSQLSecretOptions{
				Name:        "pg-creds",
				UserKey:     "DB_USER",
				PasswordKey: "DB_PASS",
			},
			JdbcUrl: "jdbc:postgresql://localhost:5432/mydb",
		},
	}

	envs := persistenceEnvVars(p, "default")
	g.Expect(*envs[1].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("DB_USER"))
	g.Expect(*envs[2].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("DB_PASS"))
}

func TestPersistenceEnvVars_ServiceRefBuildsJdbcUrl(t *testing.T) {
	g := gomega.NewWithT(t)
	p := &logicv1.PersistenceOptionsSpec{
		PostgreSQL: &logicv1.PersistencePostgreSQL{
			SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
			ServiceRef: &logicv1.PostgreSQLServiceOptions{
				SQLServiceOptions: &logicv1.SQLServiceOptions{
					Name:         "postgres",
					Namespace:    "databases",
					Port:         intPtr(5433),
					DatabaseName: "workflows",
				},
				DatabaseSchema: "runtime-schema",
			},
		},
	}

	envs := persistenceEnvVars(p, "default")
	jdbcEnv := envs[len(envs)-1]
	g.Expect(*jdbcEnv.Value).To(gomega.Equal("jdbc:postgresql://postgres.databases.svc:5433/workflows?currentSchema=runtime-schema"))
}

func TestPersistenceEnvVars_ServiceRefFallbackDefaults(t *testing.T) {
	g := gomega.NewWithT(t)
	p := &logicv1.PersistenceOptionsSpec{
		PostgreSQL: &logicv1.PersistencePostgreSQL{
			SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
			ServiceRef: &logicv1.PostgreSQLServiceOptions{
				SQLServiceOptions: &logicv1.SQLServiceOptions{
					Name: "postgres",
				},
			},
		},
	}

	envs := persistenceEnvVars(p, "my-namespace")
	jdbcEnv := envs[len(envs)-1]
	g.Expect(*jdbcEnv.Value).To(gomega.Equal("jdbc:postgresql://postgres.my-namespace.svc:5432/logicflow"))
}

func TestPersistenceEnvVars_TLSAppendsSslMode(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("with schema query param", func(t *testing.T) {
		g = gomega.NewWithT(t)
		p := &logicv1.PersistenceOptionsSpec{
			PostgreSQL: &logicv1.PersistencePostgreSQL{
				SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
				ServiceRef: &logicv1.PostgreSQLServiceOptions{
					SQLServiceOptions: &logicv1.SQLServiceOptions{Name: "postgres"},
					DatabaseSchema:    "myschema",
				},
				TLS: &logicv1.TLSConnection{Enabled: true, TLSMode: logicv1.TLSModeVerifyFull},
			},
		}
		envs := persistenceEnvVars(p, "default")
		g.Expect(*envs[len(envs)-1].Value).To(gomega.ContainSubstring("?currentSchema=myschema&sslmode=verify-full"))
	})

	t.Run("without query params uses question mark", func(t *testing.T) {
		g = gomega.NewWithT(t)
		p := &logicv1.PersistenceOptionsSpec{
			PostgreSQL: &logicv1.PersistencePostgreSQL{
				SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
				JdbcUrl:   "jdbc:postgresql://localhost:5432/mydb",
				TLS:       &logicv1.TLSConnection{Enabled: true, TLSMode: logicv1.TLSModeRequire},
			},
		}
		envs := persistenceEnvVars(p, "default")
		g.Expect(*envs[len(envs)-1].Value).To(gomega.Equal("jdbc:postgresql://localhost:5432/mydb?sslmode=require"))
	})

	t.Run("defaults to prefer when mode empty", func(t *testing.T) {
		g = gomega.NewWithT(t)
		p := &logicv1.PersistenceOptionsSpec{
			PostgreSQL: &logicv1.PersistencePostgreSQL{
				SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
				JdbcUrl:   "jdbc:postgresql://localhost:5432/mydb",
				TLS:       &logicv1.TLSConnection{Enabled: true},
			},
		}
		envs := persistenceEnvVars(p, "default")
		g.Expect(*envs[len(envs)-1].Value).To(gomega.Equal("jdbc:postgresql://localhost:5432/mydb?sslmode=prefer"))
	})
}

func TestSecurityEnvVars_None(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("default empty type", func(t *testing.T) {
		g = gomega.NewWithT(t)
		envs := securityEnvVars(logicv1.RuntimeSecuritySpec{})
		g.Expect(envs).To(gomega.HaveLen(1))
		g.Expect(*envs[0].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SECURITY_TYPE"))
		g.Expect(*envs[0].Value).To(gomega.Equal("none"))
	})

	t.Run("explicit NONE", func(t *testing.T) {
		g = gomega.NewWithT(t)
		envs := securityEnvVars(logicv1.RuntimeSecuritySpec{Type: logicv1.RuntimeSecurityNone})
		g.Expect(envs).To(gomega.HaveLen(1))
		g.Expect(*envs[0].Value).To(gomega.Equal("none"))
	})
}

func TestSecurityEnvVars_APIKey(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("single key with roles", func(t *testing.T) {
		g = gomega.NewWithT(t)
		sec := logicv1.RuntimeSecuritySpec{
			Type: logicv1.RuntimeSecurityAPIKey,
			APIKey: &logicv1.APIKeyAuthSpec{
				Keys: []logicv1.APIKeySpec{
					{
						Name:      "service-a",
						SecretRef: logicv1.SecretKeySelector{Name: "api-secret", Key: "token"},
						Roles:     []logicv1.RuntimeSecurityRole{logicv1.RuntimeSecurityRoleAdmin, logicv1.RuntimeSecurityRoleInvoker},
					},
				},
			},
		}

		envs := securityEnvVars(sec)
		g.Expect(envs).To(gomega.HaveLen(3))

		g.Expect(*envs[0].Value).To(gomega.Equal("api-key"))

		g.Expect(*envs[1].Name).To(gomega.Equal(`QUARKUS_FLOW_RUNNER_SECURITY_API_KEYS__"service-a"__SECRET`))
		g.Expect(*envs[1].ValueFrom.SecretKeyRef.Name).To(gomega.Equal("api-secret"))
		g.Expect(*envs[1].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("token"))

		g.Expect(*envs[2].Name).To(gomega.Equal(`QUARKUS_FLOW_RUNNER_SECURITY_API_KEYS__"service-a"__ROLES`))
		g.Expect(*envs[2].Value).To(gomega.Equal("flow-admin,flow-invoker"))
	})

	t.Run("multiple keys", func(t *testing.T) {
		g = gomega.NewWithT(t)
		sec := logicv1.RuntimeSecuritySpec{
			Type: logicv1.RuntimeSecurityAPIKey,
			APIKey: &logicv1.APIKeyAuthSpec{
				Keys: []logicv1.APIKeySpec{
					{Name: "a", SecretRef: logicv1.SecretKeySelector{Name: "s-a", Key: "v"}, Roles: []logicv1.RuntimeSecurityRole{logicv1.RuntimeSecurityRoleAdmin}},
					{Name: "b", SecretRef: logicv1.SecretKeySelector{Name: "s-b", Key: "v"}, Roles: []logicv1.RuntimeSecurityRole{logicv1.RuntimeSecurityRoleInvoker}},
				},
			},
		}
		envs := securityEnvVars(sec)
		g.Expect(envs).To(gomega.HaveLen(5)) // type + 2*(secret+roles)
	})
}

func TestSecurityEnvVars_OIDC(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("all fields", func(t *testing.T) {
		g = gomega.NewWithT(t)
		sec := logicv1.RuntimeSecuritySpec{
			Type: logicv1.RuntimeSecurityOIDC,
			OIDC: &logicv1.OIDCAuthSpec{
				AuthServerUrl: "https://keycloak.example.com/realms/flow",
				ClientId:      "flow-app",
				ClientSecret:  logicv1.SecretKeySelector{Name: "oidc-secret", Key: "client-secret"},
				RolesClaim:    "realm_access.roles",
			},
		}

		envs := securityEnvVars(sec)
		g.Expect(envs).To(gomega.HaveLen(5))
		g.Expect(*envs[0].Value).To(gomega.Equal("oidc"))
		g.Expect(*envs[1].Value).To(gomega.Equal("https://keycloak.example.com/realms/flow"))
		g.Expect(*envs[2].Value).To(gomega.Equal("flow-app"))
		g.Expect(*envs[3].ValueFrom.SecretKeyRef.Name).To(gomega.Equal("oidc-secret"))
		g.Expect(*envs[4].Name).To(gomega.Equal("QUARKUS_OIDC_ROLES_CLAIM"))
		g.Expect(*envs[4].Value).To(gomega.Equal("realm_access.roles"))
	})

	t.Run("default rolesClaim omitted", func(t *testing.T) {
		g = gomega.NewWithT(t)
		sec := logicv1.RuntimeSecuritySpec{
			Type: logicv1.RuntimeSecurityOIDC,
			OIDC: &logicv1.OIDCAuthSpec{
				AuthServerUrl: "https://keycloak.example.com/realms/flow",
				ClientId:      "flow-app",
				ClientSecret:  logicv1.SecretKeySelector{Name: "oidc-secret", Key: "secret"},
				RolesClaim:    "roles",
			},
		}
		envs := securityEnvVars(sec)
		g.Expect(envs).To(gomega.HaveLen(4))
		for _, env := range envs {
			g.Expect(*env.Name).NotTo(gomega.Equal("QUARKUS_OIDC_ROLES_CLAIM"))
		}
	})
}

func TestDefaultRunnerImage_AutoSelect(t *testing.T) {
	g := gomega.NewWithT(t)
	minimalExpected := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantMinimal)
	standardExpected := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantStandard)

	t.Run("no persistence selects minimal", func(t *testing.T) {
		g = gomega.NewWithT(t)
		c := corev1ac.Container().WithName(ContainerNameRunner)
		DefaultRunnerImage(nil)(c)
		g.Expect(*c.Image).To(gomega.Equal(minimalExpected))
	})

	t.Run("with persistence selects standard", func(t *testing.T) {
		g = gomega.NewWithT(t)
		c := corev1ac.Container().WithName(ContainerNameRunner)
		persistence := &logicv1.PersistenceOptionsSpec{
			PostgreSQL: &logicv1.PersistencePostgreSQL{},
		}
		DefaultRunnerImage(persistence)(c)
		g.Expect(*c.Image).To(gomega.Equal(standardExpected))
	})
}

func TestDefaultRunnerImage_PreservesUserImage(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("explicit image not overwritten", func(t *testing.T) {
		g = gomega.NewWithT(t)
		c := corev1ac.Container().WithName(ContainerNameRunner).WithImage("my-registry/custom-runner:1.0")
		DefaultRunnerImage(nil)(c)
		g.Expect(*c.Image).To(gomega.Equal("my-registry/custom-runner:1.0"))
	})

	t.Run("explicit image not overwritten even with persistence", func(t *testing.T) {
		g = gomega.NewWithT(t)
		c := corev1ac.Container().WithName(ContainerNameRunner).WithImage("my-registry/custom-runner:1.0")
		persistence := &logicv1.PersistenceOptionsSpec{PostgreSQL: &logicv1.PersistencePostgreSQL{}}
		DefaultRunnerImage(persistence)(c)
		g.Expect(*c.Image).To(gomega.Equal("my-registry/custom-runner:1.0"))
	})
}

func TestValidateRunnerImage(t *testing.T) {
	g := gomega.NewWithT(t)
	minimalImage := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantMinimal)
	standardImage := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantStandard)
	persistence := &logicv1.PersistenceOptionsSpec{PostgreSQL: &logicv1.PersistencePostgreSQL{}}

	t.Run("minimal with persistence errors", func(t *testing.T) {
		g = gomega.NewWithT(t)
		err := ValidateRunnerImage(minimalImage, persistence)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("does not support persistence"))
	})

	t.Run("standard without persistence errors", func(t *testing.T) {
		g = gomega.NewWithT(t)
		err := ValidateRunnerImage(standardImage, nil)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("requires persistence configuration"))
	})

	t.Run("minimal without persistence is valid", func(t *testing.T) {
		g = gomega.NewWithT(t)
		err := ValidateRunnerImage(minimalImage, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("standard with persistence is valid", func(t *testing.T) {
		g = gomega.NewWithT(t)
		err := ValidateRunnerImage(standardImage, persistence)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("custom image skips validation", func(t *testing.T) {
		g = gomega.NewWithT(t)
		err := ValidateRunnerImage("my-registry/custom-runner:1.0", persistence)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = ValidateRunnerImage("my-registry/custom-runner:1.0", nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})
}

func TestDefaultProbes_SetsWhenNil(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")

	DefaultProbes()(c)

	g.Expect(c.LivenessProbe).NotTo(gomega.BeNil())
	g.Expect(*c.LivenessProbe.HTTPGet.Path).To(gomega.Equal("/q/health/live"))
	g.Expect(*c.LivenessProbe.InitialDelaySeconds).To(gomega.Equal(int32(30)))
	g.Expect(*c.LivenessProbe.PeriodSeconds).To(gomega.Equal(int32(10)))

	g.Expect(c.ReadinessProbe).NotTo(gomega.BeNil())
	g.Expect(*c.ReadinessProbe.HTTPGet.Path).To(gomega.Equal("/q/health/ready"))
	g.Expect(*c.ReadinessProbe.InitialDelaySeconds).To(gomega.Equal(int32(10)))
	g.Expect(*c.ReadinessProbe.PeriodSeconds).To(gomega.Equal(int32(5)))
}

func TestDefaultProbes_PreservesUserOverride(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test").
		WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().WithPath("/custom/live")).
			WithInitialDelaySeconds(60)).
		WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(corev1ac.HTTPGetAction().WithPath("/custom/ready")).
			WithInitialDelaySeconds(5))

	DefaultProbes()(c)

	g.Expect(*c.LivenessProbe.HTTPGet.Path).To(gomega.Equal("/custom/live"))
	g.Expect(*c.LivenessProbe.InitialDelaySeconds).To(gomega.Equal(int32(60)))
	g.Expect(*c.ReadinessProbe.HTTPGet.Path).To(gomega.Equal("/custom/ready"))
	g.Expect(*c.ReadinessProbe.InitialDelaySeconds).To(gomega.Equal(int32(5)))
}

func TestDeploymentAndServiceAreWiredTogether(t *testing.T) {
	g := gomega.NewWithT(t)

	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = "my-runtime"
	rt.Namespace = "prod"
	rt.Labels = map[string]string{"team": "platform"}

	childLabels := ChildLabels(rt)
	selLabels := SelectorLabels(rt.Name)

	deploySpec := ToDeploymentSpec(ContainerNameRunner, &rt.Spec.ApplicationSpec, childLabels, selLabels)
	svc := QuarkusService(rt, logicv1.LogicFlowRuntimeKind)

	// Service selector must be a subset of the Deployment's pod template labels
	for k, v := range svc.Spec.Selector {
		g.Expect(deploySpec.Template.Labels).To(gomega.HaveKeyWithValue(k, v),
			"service selector label %s=%s missing from deployment pod template labels", k, v)
	}

	// Deployment selector must match Service selector (both use SelectorLabels)
	g.Expect(deploySpec.Selector.MatchLabels).To(gomega.Equal(svc.Spec.Selector))

	// Both reference the same owner
	g.Expect(svc.OwnerReferences).To(gomega.HaveLen(1))
	g.Expect(*svc.OwnerReferences[0].Name).To(gomega.Equal(rt.Name))
	g.Expect(*svc.OwnerReferences[0].Kind).To(gomega.Equal(logicv1.LogicFlowRuntimeKind))

	// Service exposes port 80 targeting QuarkusPort
	g.Expect(svc.Spec.Ports).To(gomega.HaveLen(1))
	g.Expect(*svc.Spec.Ports[0].Port).To(gomega.Equal(int32(80)))
	g.Expect(svc.Spec.Ports[0].TargetPort.IntValue()).To(gomega.Equal(int(QuarkusPort)))
	g.Expect(*svc.Spec.Ports[0].Name).To(gomega.Equal("http"))
}

func TestWithFlowSourcePath_SetsEnvVar(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")

	WithFlowSourcePath()(c)

	g.Expect(c.Env).To(gomega.HaveLen(1))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SOURCE_PATH"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal(WorkflowMountPath))
}

func TestWithFlowSourcePath_OverridesUserValue(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test").
		WithEnv(corev1ac.EnvVar().WithName("QUARKUS_FLOW_RUNNER_SOURCE_PATH").WithValue("/custom/path"))

	WithFlowSourcePath()(c)

	var count int
	for _, e := range c.Env {
		if e.Name != nil && *e.Name == "QUARKUS_FLOW_RUNNER_SOURCE_PATH" {
			count++
			g.Expect(*e.Value).To(gomega.Equal(WorkflowMountPath))
		}
	}
	g.Expect(count).To(gomega.Equal(1), "expected exactly one QUARKUS_FLOW_RUNNER_SOURCE_PATH env var")
}

func TestWithFlowSourcePath_PreservesOtherEnvVars(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test").
		WithEnv(
			corev1ac.EnvVar().WithName("OTHER_VAR").WithValue("keep"),
			corev1ac.EnvVar().WithName("QUARKUS_FLOW_RUNNER_SOURCE_PATH").WithValue("/bad"),
			corev1ac.EnvVar().WithName("ANOTHER_VAR").WithValue("also-keep"),
		)

	WithFlowSourcePath()(c)

	g.Expect(c.Env).To(gomega.HaveLen(3))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("OTHER_VAR"))
	g.Expect(*c.Env[1].Name).To(gomega.Equal("ANOTHER_VAR"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SOURCE_PATH"))
	g.Expect(*c.Env[2].Value).To(gomega.Equal(WorkflowMountPath))
}

func testConfigMaps() []corev1.ConfigMap {
	return []corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "lfd-order-flow"},
			Data:       map[string]string{"order-flow.json": "{}"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "lfd-payment-processor"},
			Data:       map[string]string{"payment-processor.json": "{}"},
		},
	}
}

func TestWithFlowVolumeMounts_AddsOneMountPerConfigMapKey(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")
	cms := testConfigMaps()

	WithFlowVolumeMounts(cms)(c)

	g.Expect(c.VolumeMounts).To(gomega.HaveLen(2))
	g.Expect(*c.VolumeMounts[0].Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*c.VolumeMounts[0].MountPath).To(gomega.Equal(WorkflowMountPath + "/order-flow.json"))
	g.Expect(*c.VolumeMounts[0].SubPath).To(gomega.Equal("order-flow.json"))
	g.Expect(*c.VolumeMounts[0].ReadOnly).To(gomega.BeTrue())
	g.Expect(*c.VolumeMounts[1].Name).To(gomega.Equal("lfd-payment-processor"))
	g.Expect(*c.VolumeMounts[1].MountPath).To(gomega.Equal(WorkflowMountPath + "/payment-processor.json"))
	g.Expect(*c.VolumeMounts[1].SubPath).To(gomega.Equal("payment-processor.json"))
	g.Expect(*c.VolumeMounts[1].ReadOnly).To(gomega.BeTrue())
}

func TestWithFlowVolumeMounts_EmptyConfigMapsNoMounts(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")

	WithFlowVolumeMounts(nil)(c)

	g.Expect(c.VolumeMounts).To(gomega.BeEmpty())
}

func TestFlowVolumes_ReturnsOneVolumePerConfigMap(t *testing.T) {
	g := gomega.NewWithT(t)
	cms := testConfigMaps()

	vols := FlowVolumes(cms)

	g.Expect(vols).To(gomega.HaveLen(2))
	g.Expect(*vols[0].Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*vols[0].ConfigMap.Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*vols[1].Name).To(gomega.Equal("lfd-payment-processor"))
	g.Expect(*vols[1].ConfigMap.Name).To(gomega.Equal("lfd-payment-processor"))
}

func TestFlowVolumes_EmptyConfigMapsReturnsEmpty(t *testing.T) {
	g := gomega.NewWithT(t)

	vols := FlowVolumes(nil)

	g.Expect(vols).To(gomega.BeEmpty())
}

func TestEffectiveReplicas_Default(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(1)))
}

func TestEffectiveReplicas_ExplicitReplicas(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{Replicas: int32Ptr(3)}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(3)))
}

func TestEffectiveReplicas_PodTemplateOverrides(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Replicas:    int32Ptr(3),
		PodTemplate: logicv1.PodTemplateSpec{Replicas: int32Ptr(5)},
	}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(5)))
}

func TestWithDurableEnvVars_SetsAllEnvVars(t *testing.T) {
	g := gomega.NewWithT(t)
	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = "my-runtime"
	c := corev1ac.Container().WithName("test")

	WithDurableEnvVars(rt)(c)

	g.Expect(c.Env).To(gomega.HaveLen(4))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal("false"))
	g.Expect(*c.Env[1].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME"))
	g.Expect(*c.Env[1].Value).To(gomega.Equal("my-runtime"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("POD_NAME"))
	g.Expect(*c.Env[2].ValueFrom.FieldRef.FieldPath).To(gomega.Equal("metadata.name"))
	g.Expect(*c.Env[3].Name).To(gomega.Equal("POD_NAMESPACE"))
	g.Expect(*c.Env[3].ValueFrom.FieldRef.FieldPath).To(gomega.Equal("metadata.namespace"))
}

func TestWithDurableEnvVars_FiltersUserDuplicates(t *testing.T) {
	g := gomega.NewWithT(t)
	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = "my-runtime"
	c := corev1ac.Container().WithName("test").
		WithEnv(
			corev1ac.EnvVar().WithName("OTHER_VAR").WithValue("keep"),
			corev1ac.EnvVar().WithName("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME").WithValue("user-pool"),
			corev1ac.EnvVar().WithName("POD_NAME").WithValue("user-pod"),
		)

	WithDurableEnvVars(rt)(c)

	g.Expect(c.Env).To(gomega.HaveLen(5))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("OTHER_VAR"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal("keep"))
	g.Expect(*c.Env[1].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME"))
	g.Expect(*c.Env[2].Value).To(gomega.Equal("my-runtime"))
	g.Expect(*c.Env[3].Name).To(gomega.Equal("POD_NAME"))
	g.Expect(*c.Env[4].Name).To(gomega.Equal("POD_NAMESPACE"))
}

func TestNewMemberLease_Fields(t *testing.T) {
	g := gomega.NewWithT(t)
	dep := &appsv1.Deployment{}
	dep.Name = "my-runtime"
	dep.UID = "dep-uid-123"

	lease := newMemberLease("flow-pool-member-my-runtime-00", "default", "my-runtime", dep)

	g.Expect(lease.Name).To(gomega.Equal("flow-pool-member-my-runtime-00"))
	g.Expect(lease.Namespace).To(gomega.Equal("default"))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", DurableManagedByValue))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/component", DurableComponentValue))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue(LabelDurablePool, "my-runtime"))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue(LabelDurableIsLeader, "false"))
	g.Expect(*lease.Spec.LeaseDurationSeconds).To(gomega.Equal(LeaseDuration))
	g.Expect(lease.Spec.HolderIdentity).To(gomega.BeNil())
	g.Expect(lease.OwnerReferences).To(gomega.HaveLen(1))
	g.Expect(lease.OwnerReferences[0].Kind).To(gomega.Equal("Deployment"))
	g.Expect(lease.OwnerReferences[0].Name).To(gomega.Equal("my-runtime"))
	g.Expect(*lease.OwnerReferences[0].Controller).To(gomega.BeFalse())
}

func TestMemberLeaseLabels(t *testing.T) {
	g := gomega.NewWithT(t)
	labels := memberLeaseLabels("my-pool")
	g.Expect(labels).To(gomega.HaveLen(4))
	g.Expect(labels[LabelDurablePool]).To(gomega.Equal("my-pool"))
	g.Expect(labels[LabelDurableIsLeader]).To(gomega.Equal("false"))
}

func TestDurableDeploymentStrategy_SingleReplica(t *testing.T) {
	g := gomega.NewWithT(t)
	s := durableDeploymentStrategy(1)
	g.Expect(*s.Type).To(gomega.Equal(appsv1.RecreateDeploymentStrategyType))
	g.Expect(s.RollingUpdate).To(gomega.BeNil())
}

func TestDurableDeploymentStrategy_ZeroReplica(t *testing.T) {
	g := gomega.NewWithT(t)
	s := durableDeploymentStrategy(0)
	g.Expect(*s.Type).To(gomega.Equal(appsv1.RecreateDeploymentStrategyType))
}

func TestDurableDeploymentStrategy_MultiReplica(t *testing.T) {
	g := gomega.NewWithT(t)
	s := durableDeploymentStrategy(3)
	g.Expect(*s.Type).To(gomega.Equal(appsv1.RollingUpdateDeploymentStrategyType))
	g.Expect(s.RollingUpdate).NotTo(gomega.BeNil())
	g.Expect(s.RollingUpdate.MaxUnavailable.IntValue()).To(gomega.Equal(1))
	g.Expect(s.RollingUpdate.MaxSurge.IntValue()).To(gomega.Equal(1))
}
