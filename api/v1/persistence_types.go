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

// TLSMode specifies the TLS connection mode for PostgreSQL connections.
// See: https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-SSLMODE-STATEMENTS
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

// PersistenceOptionsSpec configures database support for platform services and flows.
//
// This type is used at multiple levels with a cascading override pattern:
//   - logicplatform.spec.persistence: Global default for all services and flows
//   - logicplatform.spec.dataindex.persistence: Override for data index service
//   - logicplatform.spec.runtimes.persistence: Override for all flow runtimes
//   - Individual flow resources can also provide their own persistence config
//
// The operator resolves the effective configuration by checking in order:
//  1. Flow/service-specific persistence (most specific)
//  2. Application-type persistence (e.g., runtimes.persistence)
//  3. Platform-level persistence (global default)
//
// For services, this allows configuring generic database connectivity if the service
// does not come with its own configuration. For flows, the operator will add the
// necessary JDBC properties to the flow's application.properties so it can communicate
// with the persistence service.
//
// Example (platform-level default):
//
//	persistence:
//	  postgresql:
//	    secretRef:
//	      name: postgres-credentials
//	    serviceRef:
//	      name: postgres
//	      namespace: databases
//	      databaseName: logicflow
//	  dbMigrationStrategy: service
//
// +optional
// +kubebuilder:validation:MaxProperties=2
type PersistenceOptionsSpec struct {
	// Connect configured services to a postgresql database.
	// +optional
	PostgreSQL *PersistencePostgreSQL `json:"postgresql,omitempty"`

	// DB Migration approach for the target application. Use the following values as described.
	// job: use job based approach provided by the Logic operator.
	// service: service itself shall migrate the db and will not use Logic operator.
	// none: no database migration functionality needed.
	// +optional
	// +kubebuilder:default:=service
	DBMigrationStrategy string `json:"dbMigrationStrategy,omitempty"`
}

// PersistencePostgreSQL configures a PostgreSQL database connection.
//
// There are two ways to configure the connection (mutually exclusive):
//  1. Application Reference (serviceRef): Points to a Kubernetes Application for PostgreSQL
//  2. JDBC URL (jdbcUrl): Direct connection string
//
// The serviceRef approach is recommended as it allows the operator to construct
// the JDBC URL dynamically based on the service endpoint.
//
// Example (using service reference):
//
//	postgresql:
//	  secretRef:
//	    name: postgres-credentials
//	    userKey: POSTGRESQL_USER
//	    passwordKey: POSTGRESQL_PASSWORD
//	  serviceRef:
//	    name: postgres
//	    namespace: databases
//	    port: 5432
//	    databaseName: logicflow
//	    databaseSchema: workflows
//
// Example (using JDBC URL):
//
//	postgresql:
//	  secretRef:
//	    name: postgres-credentials
//	  jdbcUrl: "jdbc:postgresql://postgres.databases.svc:5432/logicflow?currentSchema=workflows"
//
// +kubebuilder:validation:MinProperties=2
// +kubebuilder:validation:MaxProperties=2
type PersistencePostgreSQL struct {
	// Secret reference to the database user credentials
	SecretRef PostgreSQLSecretOptions `json:"secretRef"`
	// Application reference to postgresql datasource. Mutually exclusive to jdbcUrl.
	// +optional
	ServiceRef *PostgreSQLServiceOptions `json:"serviceRef,omitempty"`
	// PostgreSql JDBC URL. Mutually exclusive to serviceRef.
	// e.g. "jdbc:postgresql://host:port/database?currentSchema=workflows"
	// +optional
	JdbcURL string `json:"jdbcUrl,omitempty"`
	// TLS configuration for PostgreSQL connections.
	// When enabled, the operator will append SSL parameters to the JDBC URL.
	// +optional
	TLS *TLSConnection `json:"tls,omitempty"`
}

// TLSConnection configures TLS/SSL settings for PostgreSQL connections.
//
// When enabled, the operator appends SSL parameters to the JDBC URL based on the mode:
//   - disable: sslmode=disable
//   - allow: sslmode=allow
//   - prefer: sslmode=prefer (PostgreSQL default if not specified)
//   - require: sslmode=require
//   - verify-ca: sslmode=verify-ca (requires CA certificate)
//   - verify-full: sslmode=verify-full (requires CA certificate and hostname verification)
//
// For verify-ca and verify-full modes, ensure the PostgreSQL server certificate
// is properly configured and the CA certificate is available to the application.
//
// Example:
//
//	tls:
//	  enabled: true
//	  tlsMode: require
type TLSConnection struct {
	// Enabled determines whether to use TLS/SSL for PostgreSQL connections.
	// If true, SSL parameters will be added to the JDBC URL based on tlsMode.
	// Defaults to false.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// TLSMode specifies the SSL connection mode for PostgreSQL.
	// Defaults to "prefer" when enabled is true.
	// See: https://www.postgresql.org/docs/current/libpq-ssl.html
	// +optional
	// +kubebuilder:default=prefer
	TLSMode TLSMode `json:"tlsMode,omitempty"`
}

// PostgreSQLSecretOptions use credential secret for postgresql connection.
type PostgreSQLSecretOptions struct {
	// Name of the postgresql credentials secret.
	Name string `json:"name"`
	// Defaults to POSTGRESQL_USER
	// +optional
	UserKey string `json:"userKey,omitempty"`
	// Defaults to POSTGRESQL_PASSWORD
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

type SQLServiceOptions struct {
	// Name of the postgresql k8s service.
	Name string `json:"name"`
	// Namespace of the postgresql k8s service. Defaults to the LogicPlatform's local namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Port to use when connecting to the postgresql k8s service. Defaults to 5432.
	// +optional
	Port *int `json:"port,omitempty"`
	// Name of postgresql database to be used. Defaults to "logicflow"
	// +optional
	DatabaseName string `json:"databaseName,omitempty"`
}

// PostgreSQLServiceOptions use k8s service to configure postgresql jdbc url.
type PostgreSQLServiceOptions struct {
	*SQLServiceOptions `json:",inline"`
	// Schema of postgresql database to be used. When empty, the database default schema is used (typically "public").
	// +optional
	DatabaseSchema string `json:"databaseSchema,omitempty"`
}
