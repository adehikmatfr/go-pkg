// Package postgres is an rdbms adapter that opens a pooled *sql.DB against a
// Postgres database using the lib/pq driver. The returned *sql.DB is
// database/sql's own port, so swapping to another SQL adapter in this
// category needs no change on the consumer side beyond the constructor call.
package postgres

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/adehikmatfr/go-pkg/v2/datastore/rdbms"
)

// New opens a pooled Postgres connection. sql.Open only validates the DSN and
// configures the pool — it does not dial the database, so callers that don't
// need Postgres immediately can still start when the database is
// unreachable. Use (*sql.DB).PingContext to verify connectivity eagerly.
func New(cfg *rdbms.Config) (*sql.DB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN must not be empty")
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	db.SetConnMaxIdleTime(time.Duration(cfg.ConnMaxIdleTime) * time.Second)

	return db, nil
}
