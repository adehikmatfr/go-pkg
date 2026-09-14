package postgres_test

import (
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/datastore/sqldb"
	"github.com/adehikmatfr/go-pkg/v2/datastore/sqldb/postgres"
)

func TestNewEmptyDSN(t *testing.T) {
	if _, err := postgres.New(&sqldb.Config{}); err == nil {
		t.Fatal("New() with empty DSN should return an error")
	}
}

func TestNewAppliesPoolSettings(t *testing.T) {
	// sql.Open only validates the DSN string; it doesn't dial the database,
	// so this is safe to run without a live Postgres instance.
	db, err := postgres.New(&sqldb.Config{
		DSN:             "postgres://user:pass@localhost:5432/db?sslmode=disable",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 60,
		ConnMaxIdleTime: 30,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	stats := db.Stats()
	if stats.MaxOpenConnections != 10 {
		t.Errorf("MaxOpenConnections = %d, want 10", stats.MaxOpenConnections)
	}
}
