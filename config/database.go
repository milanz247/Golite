package config

import "time"

// DatabaseConfig mirrors Laravel's config('database.connections.mysql'):
// MySQL connection credentials, plus the connection-pool tuning GORM's
// underlying *sql.DB exposes (MaxIdleConns/MaxOpenConns/ConnMaxLifetime)
// — see app/Providers/DatabaseServiceProvider.go and docs/database.md.
type DatabaseConfig struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
	Charset  string

	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

// loadDatabaseConfig reads DB_* variables, called from LoadConfig
// (config/app.go). Defaults assume a local MySQL install with a "golite"
// database and no root password — convenient for a fresh clone, not a
// production posture; every value is overridable via .env.
func loadDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		Host:     getEnv("DB_HOST", "127.0.0.1"),
		Port:     getEnv("DB_PORT", "3306"),
		Database: getEnv("DB_DATABASE", "golite"),
		Username: getEnv("DB_USERNAME", "root"),
		Password: getEnv("DB_PASSWORD", ""),
		Charset:  getEnv("DB_CHARSET", "utf8mb4"),

		MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
		MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 100),
		ConnMaxLifetime: time.Duration(getEnvInt("DB_CONN_MAX_LIFETIME_MINUTES", 60)) * time.Minute,
	}
}
