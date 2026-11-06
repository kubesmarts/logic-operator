package v1

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestDatastoreSpec_OperationalSecretIsDML(t *testing.T) {
	g := gomega.NewWithT(t)

	ds := DatastoreSpec{
		PostgreSQL: &PersistencePostgreSQL{
			SecretRef: PostgreSQLSecretOptions{
				Name:        "operational-secret",
				UserKey:     "user",
				PasswordKey: "password",
			},
			ServiceRef: PostgreSQLServiceOptions{
				Name: "postgres",
			},
		},
	}

	// OperationalSecret should return the DML credential (SecretRef)
	opSecret := OperationalSecret(ds)
	g.Expect(opSecret.Name).To(gomega.Equal("operational-secret"))
	g.Expect(opSecret.UserKey).To(gomega.Equal("user"))
	g.Expect(opSecret.PasswordKey).To(gomega.Equal("password"))
}

func TestDatastoreSpec_MigrationSecretIsDDL(t *testing.T) {
	g := gomega.NewWithT(t)

	// Test 1: MigrationSecretRef is set
	ds := DatastoreSpec{
		PostgreSQL: &PersistencePostgreSQL{
			SecretRef: PostgreSQLSecretOptions{
				Name: "operational-secret",
			},
			ServiceRef: PostgreSQLServiceOptions{
				Name: "postgres",
			},
		},
		MigrationSecretRef: &PostgreSQLSecretOptions{
			Name:        "migration-secret",
			UserKey:     "ddl-user",
			PasswordKey: "ddl-password",
		},
	}

	migSecret := MigrationSecret(ds)
	g.Expect(migSecret.Name).To(gomega.Equal("migration-secret"))
	g.Expect(migSecret.UserKey).To(gomega.Equal("ddl-user"))
	g.Expect(migSecret.PasswordKey).To(gomega.Equal("ddl-password"))

	// Test 2: MigrationSecretRef is nil, falls back to SecretRef
	ds2 := DatastoreSpec{
		PostgreSQL: &PersistencePostgreSQL{
			SecretRef: PostgreSQLSecretOptions{
				Name:        "operational-secret",
				UserKey:     "user",
				PasswordKey: "password",
			},
			ServiceRef: PostgreSQLServiceOptions{
				Name: "postgres",
			},
		},
		MigrationSecretRef: nil,
	}

	migSecret2 := MigrationSecret(ds2)
	g.Expect(migSecret2.Name).To(gomega.Equal("operational-secret"))
	g.Expect(migSecret2.UserKey).To(gomega.Equal("user"))
	g.Expect(migSecret2.PasswordKey).To(gomega.Equal("password"))
}
