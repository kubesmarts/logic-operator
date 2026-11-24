// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package v1

type DBMigrationStrategyType string

const (
	DBMigrationStrategyService DBMigrationStrategyType = "service"
	DBMigrationStrategyJob     DBMigrationStrategyType = "job"
	DBMigrationStrategyNone    DBMigrationStrategyType = "none"
)

// TLSMode is the PostgreSQL sslmode.
// +kubebuilder:validation:Enum=disable;allow;prefer;require;verify-ca;verify-full
type TLSMode string

const (
	// TLSModeDisable - no SSL connection will be negotiated
	TLSModeDisable TLSMode = "disable"
	// TLSModeAllow - try non-SSL first, then SSL if required by server
	TLSModeAllow TLSMode = "allow"
	// TLSModePrefer - try SSL first, then non-SSL if SSL not available (default)
	TLSModePrefer TLSMode = "prefer"
	// TLSModeRequire - only SSL, but don't verify server certificate
	TLSModeRequire TLSMode = "require"
	// TLSModeVerifyCA - only SSL, verify server certificate against CA
	TLSModeVerifyCA TLSMode = "verify-ca"
	// TLSModeVerifyFull - only SSL, verify server certificate and hostname
	TLSModeVerifyFull TLSMode = "verify-full"
)

// PersistencePostgreSQL configures a PostgreSQL connection; the operator builds the JDBC URL from ServiceRef.
// +kubebuilder:validation:MinProperties=2
// +kubebuilder:validation:MaxProperties=3
type PersistencePostgreSQL struct {
	// SecretRef references the database user credentials.
	SecretRef PostgreSQLSecretOptions `json:"secretRef"`
	// ServiceRef references the PostgreSQL Service.
	ServiceRef PostgreSQLServiceOptions `json:"serviceRef"`
	// TLS configures SSL parameters appended to the JDBC URL.
	// +optional
	TLS *TLSConnection `json:"tls,omitempty"`
}

// TLSConnection configures TLS/SSL for PostgreSQL connections.
// verify-ca and verify-full require the CA certificate to be available to the application.
type TLSConnection struct {
	// Enabled adds sslmode to the JDBC URL.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// TLSMode is the PostgreSQL sslmode.
	// +optional
	// +kubebuilder:default=prefer
	TLSMode TLSMode `json:"tlsMode,omitempty"`
}

// PostgreSQLSecretOptions references a Secret with PostgreSQL credentials.
type PostgreSQLSecretOptions struct {
	// Name of the credentials Secret.
	Name string `json:"name"`
	// UserKey is the Secret key holding the user; defaults to POSTGRESQL_USER.
	// +optional
	UserKey string `json:"userKey,omitempty"`
	// PasswordKey is the Secret key holding the password; defaults to POSTGRESQL_PASSWORD.
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

// PostgreSQLServiceOptions references the Kubernetes Service exposing PostgreSQL.
type PostgreSQLServiceOptions struct {
	// Name of the Service.
	Name string `json:"name"`
	// Namespace of the Service; when omitted, Name resolves within the same namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Port of the Service.
	// +optional
	Port *int `json:"port,omitempty"`
	// DatabaseName is the PostgreSQL database.
	// +optional
	DatabaseName string `json:"databaseName,omitempty"`
	// DatabaseSchema is the PostgreSQL schema; defaults to the database default (typically "public").
	// +optional
	DatabaseSchema string `json:"databaseSchema,omitempty"`
}

// DatastoreSpec configures a PostgreSQL datastore with separate DML (operational) and DDL (migration) credentials.
type DatastoreSpec struct {
	// PostgreSQL configures the PostgreSQL connection; SecretRef is the DML (operational) credential.
	// +optional
	PostgreSQL *PersistencePostgreSQL `json:"postgresql,omitempty"`
	// MigrationSecretRef references the DDL credential; defaults to PostgreSQL.SecretRef.
	// +optional
	MigrationSecretRef *PostgreSQLSecretOptions `json:"migrationSecretRef,omitempty"`
	// DBMigrationStrategy selects who owns the schema: service (the workload migrates itself),
	// job (the operator runs a logic-db-migrator Job before rollout), or none (externally managed).
	// +optional
	// +kubebuilder:default:=service
	// +kubebuilder:validation:Enum=service;job;none
	DBMigrationStrategy DBMigrationStrategyType `json:"dbMigrationStrategy,omitempty"`
}

// RuntimePersistenceSpec configures workflow state and an optional separate Quartz datasource.
type RuntimePersistenceSpec struct {
	// Runtime configures the datastore for Quarkus Flow workflow state.
	// +optional
	Runtime DatastoreSpec `json:"runtime,omitempty"`
	// Quartz configures a separate datasource for Quartz tables; defaults to Runtime.
	// Its tables are migrated only when its dbMigrationStrategy is job.
	// +optional
	Quartz DatastoreSpec `json:"quartz,omitempty"`
}

// OperationalSecret returns the DML (operational) database credential from the datastore.
func OperationalSecret(d DatastoreSpec) PostgreSQLSecretOptions {
	if d.PostgreSQL != nil {
		return d.PostgreSQL.SecretRef
	}
	return PostgreSQLSecretOptions{}
}

// MigrationSecret returns the DDL (schema migration) credential from the datastore.
// When MigrationSecretRef is nil, it falls back to the operational secret (SecretRef).
func MigrationSecret(d DatastoreSpec) PostgreSQLSecretOptions {
	if d.MigrationSecretRef != nil {
		return *d.MigrationSecretRef
	}
	// Fall back to operational secret
	return OperationalSecret(d)
}
