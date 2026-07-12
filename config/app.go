package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// AppConfig mirrors the values Laravel exposes via config('app.*'): the
// application name, environment, and the port it listens on.
type AppConfig struct {
	Name string
	Env  string
	Port string
}

// Config is the root configuration repository. Additional sections (e.g. a
// DatabaseConfig) can be added here as the framework grows.
type Config struct {
	App AppConfig
}

// LoadConfig reads variables from a .env file (à la Laravel) and falls back
// to sane defaults when a variable is absent.
func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[Config] no .env file found, falling back to system environment")
	}

	return &Config{
		App: AppConfig{
			Name: getEnv("APP_NAME", "Golite"),
			Env:  getEnv("APP_ENV", "local"),
			Port: getEnv("APP_PORT", ":8080"),
		},
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
