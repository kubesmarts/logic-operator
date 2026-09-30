package controller

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// DatabaseConnector abstracts database connectivity testing.
type DatabaseConnector interface {
	Ping(ctx context.Context, host string, port int, user string, password string, dbname string) (bool, error)
}

// PostgresConnector implements DatabaseConnector for PostgreSQL.
type PostgresConnector struct{}

func (pc *PostgresConnector) Ping(ctx context.Context, host string, port int, user string, password string, dbname string) (bool, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=3",
		host, port, user, password, dbname,
	)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return false, err
	}
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := db.PingContext(testCtx); err != nil {
		return false, err
	}
	return true, nil
}
