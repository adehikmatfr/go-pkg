// Package sqldb holds the shared configuration shape for every relational
// database adapter (e.g. sqldb/postgres). There is deliberately no custom
// interface here: every adapter already returns the standard library's
// *sql.DB, which is itself database/sql's own port — every driver
// ("postgres", "mysql", ...) is already swappable behind that one type, so
// wrapping it in a second interface here would only add indirection without
// adding a real capability.
package sqldb

// Config controls the connection pool. ConnMaxLifetime and ConnMaxIdleTime
// are in seconds; zero means "no limit" (database/sql's default). DSN format
// is adapter-specific (a Postgres DSN differs from a MySQL one).
type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int
	ConnMaxIdleTime int
}
