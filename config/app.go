package config

import (
	"encoding/base64"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"

	"Golite/encryption"
)

// AppConfig mirrors the values Laravel exposes via config('app.*'): the
// application name, environment, the port it listens on, whether debug
// output is allowed to leak into error responses, and the persisted
// application key used by the encryption package.
type AppConfig struct {
	Name  string
	Env   string
	Port  string
	Debug bool
	// Key is the AES-256 key for encryption.Encrypter, decoded from
	// APP_KEY. json:"-" is deliberate, not decorative: AppConfig gets
	// serialized wholesale by demo routes like UserController.Show
	// (`"app": cfg.App`), and this is a raw encryption key — it must never
	// round-trip into a JSON response, logged output, or anywhere else
	// something might serialize a Config value without thinking about it.
	Key []byte `json:"-"`
}

// LogConfig mirrors config('logging.*'): which channel is used by default,
// and where the file-backed channels ("single"/"daily") write to.
type LogConfig struct {
	Channel string
	Path    string
	Days    int // retention for the "daily" channel
}

// HashConfig mirrors config('hashing.*'): the default hashing driver and
// its parameters.
type HashConfig struct {
	Driver     string
	BcryptCost int
}

// Config is the root configuration repository. Additional sections can be
// added here as the framework grows.
type Config struct {
	App  AppConfig
	Log  LogConfig
	Hash HashConfig
	DB   DatabaseConfig
}

// LoadConfig reads variables from a .env file (à la Laravel) and falls back
// to sane defaults when a variable is absent.
func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[Config] no .env file found, falling back to system environment")
	}

	env := getEnv("APP_ENV", "local")

	return &Config{
		App: AppConfig{
			Name:  getEnv("APP_NAME", "Golite"),
			Env:   env,
			Port:  getEnv("APP_PORT", ":8080"),
			Debug: getEnvBool("APP_DEBUG", env != "production"),
			Key:   loadAppKey(),
		},
		Log: LogConfig{
			Channel: getEnv("LOG_CHANNEL", "single"),
			Path:    getEnv("LOG_PATH", "storage/logs/golite.log"),
			Days:    getEnvInt("LOG_DAILY_DAYS", 14),
		},
		Hash: HashConfig{
			Driver:     getEnv("HASH_DRIVER", "bcrypt"),
			BcryptCost: getEnvInt("HASH_BCRYPT_COST", 10),
		},
		DB: loadDatabaseConfig(),
	}
}

// loadAppKey decodes APP_KEY (expected as "base64:..." or a bare base64
// string, the same shape Laravel's own APP_KEY takes) into a 32-byte
// AES-256 key. If APP_KEY is absent or invalid, it generates a fresh key
// for this process only and logs a warning — the same "works out of the
// box, degrades gracefully" tradeoff Kernel.appKey makes for cookie
// encryption (see docs/architecture.md) — so encryption.Encrypter is
// usable immediately after a fresh clone, but values it encrypts won't
// survive a restart until a real APP_KEY is set in .env.
func loadAppKey() []byte {
	raw := os.Getenv("APP_KEY")
	if raw == "" {
		log.Println("[Config] no APP_KEY set — generated an ephemeral key for this process only; run `golite key:generate`-equivalent (base64-encode 32 random bytes) and set APP_KEY in .env to persist encrypted values across restarts")
		return encryption.GenerateKey()
	}

	if len(raw) > 7 && raw[:7] == "base64:" {
		raw = raw[7:]
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != encryption.KeySize {
		log.Println("[Config] APP_KEY is not a valid base64-encoded 32-byte key — generated an ephemeral key for this process only")
		return encryption.GenerateKey()
	}
	return key
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
