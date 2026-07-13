package providers

import (
	"fmt"
	"log"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"Golite/config"
	"Golite/container"
)

// DatabaseServiceProvider opens Golite's GORM/MySQL connection and binds
// it into the container under "db" — Golite's equivalent of Laravel's
// Illuminate\Database\DatabaseServiceProvider.
type DatabaseServiceProvider struct{}

// dsn builds a MySQL DSN in the "user:pass@tcp(host:port)/db?params" form
// the go-sql-driver/mysql driver (which GORM's mysql driver wraps)
// expects. parseTime=True is required for GORM to scan MySQL's DATETIME
// columns straight into time.Time.
func dsn(cfg config.DatabaseConfig) string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset,
	)
}

// OpenDatabase opens a GORM/MySQL connection from cfg and configures its
// underlying connection pool. Factored out of Register so artisan.go's
// CLI commands — which need a *gorm.DB but never boot the full HTTP
// application (no Kernel, no middleware, no other providers) — can open
// the exact same connection without duplicating DSN-building or pool
// configuration.
func OpenDatabase(cfg config.DatabaseConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn(cfg)), &gorm.Config{
		// Warn-level logging: SQL errors and slow queries surface, but
		// GORM doesn't log every successful query — the equivalent of
		// Laravel leaving query logging off by default outside local
		// debugging.
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("golite/database: failed to connect to MySQL at %s:%s: %w", cfg.Host, cfg.Port, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("golite/database: failed to access the underlying *sql.DB: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return db, nil
}

// Register opens the database connection and binds it as "db" — resolved
// exactly like any other service (`c.Make("db").(*gorm.DB)`, or via
// apphttp.Inject as a `*gorm.DB` method parameter; see
// docs/database.md#using-db-from-a-controller).
//
// A connection failure here is deliberately non-fatal: unlike
// AppServiceProvider's services, Golite's HTTP demo has always run with
// zero external dependencies, and a MySQL server isn't guaranteed to be
// running on a fresh clone. Register logs a clear warning and simply
// leaves "db" unbound rather than panicking, so the rest of the app keeps
// working — anything that actually calls c.Make("db") on a
// never-configured setup gets an immediate, obvious nil-interface panic
// at that call site instead. artisan.go's migrate/migrate:rollback
// commands, which are meaningless without a database, call OpenDatabase
// directly and exit non-zero on failure instead.
func (p *DatabaseServiceProvider) Register(c *container.Container) {
	cfg := c.Make("config").(*config.Config)

	db, err := OpenDatabase(cfg.DB)
	if err != nil {
		log.Printf("[DatabaseServiceProvider] %v — the \"db\" service will be unavailable until this is resolved (see .env's DB_* variables)", err)
		return
	}
	c.Bind("db", db)
}

// Boot does nothing — the connection is already usable once Register
// returns.
func (p *DatabaseServiceProvider) Boot(c *container.Container) {}
