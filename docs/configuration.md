# Configuration

Files: [`config/app.go`](../config/app.go), [`.env`](../.env)

## `.env`

Golite reads configuration from a `.env` file in the project root, using
[`github.com/joho/godotenv`](https://github.com/joho/godotenv) — the same
idea as Laravel's `.env` + `config()` layer.

```
APP_NAME=Golite
APP_ENV=local
APP_PORT=:8080
DB_HOST=127.0.0.1
DB_PORT=3306
```

See [`.env.example`](../.env.example) for the full set Golite reads,
including the optional `APP_DEBUG`, `APP_KEY`, `LOG_*`, and `HASH_*`
variables described below.

`.env` is listed in [`.gitignore`](../.gitignore) and is **not** committed
to the repository — it's local/per-environment configuration. Each
deployment environment should have its own `.env` (or equivalent process
environment variables).

## `config.LoadConfig()`

```go
type AppConfig struct {
	Name  string
	Env   string
	Port  string
	Debug bool
	Key   []byte // AES-256 key for encryption.Encrypter, decoded from APP_KEY
}

type LogConfig struct {
	Channel string
	Path    string
	Days    int
}

type HashConfig struct {
	Driver     string
	BcryptCost int
}

type DatabaseConfig struct {
	Host, Port, Database, Username, Password, Charset string
	MaxIdleConns, MaxOpenConns                         int
	ConnMaxLifetime                                    time.Duration
}

type Config struct {
	App  AppConfig
	Log  LogConfig
	Hash HashConfig
	DB   DatabaseConfig
}

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
		DB: loadDatabaseConfig(), // DB_* — see database.md
	}
}
```

`DatabaseConfig` lives in its own file, `config/database.go` (`loadDatabaseConfig`,
reading `DB_HOST`/`DB_PORT`/`DB_DATABASE`/`DB_USERNAME`/`DB_PASSWORD`/`DB_CHARSET`
plus the connection-pool tuning `DB_MAX_IDLE_CONNS`/`DB_MAX_OPEN_CONNS`/
`DB_CONN_MAX_LIFETIME_MINUTES`) rather than inline in `app.go` alongside
the others — see [database.md](database.md) for the full picture
(`DatabaseServiceProvider`, models, migrations, the `artisan` CLI).

- `godotenv.Load()` populates the process environment from `.env` if the
  file exists. If it doesn't (e.g. in production, where real environment
  variables are set directly), Golite logs a notice and continues —
  `getEnv` still reads from whatever is in the process environment.
- `getEnv(key, fallback)` / `getEnvBool` / `getEnvInt` all return the
  fallback if the variable is unset, empty, or fails to parse, so an
  accidentally blank or malformed `.env` line doesn't produce a broken
  config value.
- `App.Debug` defaults to `true` unless `APP_ENV=production` (override
  either way with `APP_DEBUG`) — it gates whether error responses include
  raw error detail; see [error-handling.md](error-handling.md).
- `App.Key` is decoded from `APP_KEY` (`base64:...` or bare base64, 32
  bytes). If absent or invalid, `loadAppKey` generates an ephemeral key for
  that process only and logs a warning — see
  [encryption.md](encryption.md#app_key-and-configloadconfig).

`LoadConfig()` is called once, in `bootstrap.NewApplication()` (see
[bootstrapping.md](bootstrapping.md)), and the resulting `*Config` is:

- stored on `Application.Config`, and
- bound into the container as `"config"`, so any provider or controller can
  resolve it with `c.Make("config").(*config.Config)` (see
  [service-container.md](service-container.md)).

## Adding a new config value

1. Add the variable to `.env`:

   ```
   MAIL_HOST=smtp.example.com
   ```

2. Add a field (and, for a new section, a new struct) in `config/app.go`:

   ```go
   type MailConfig struct {
       Host string
   }

   type Config struct {
       App  AppConfig
       Mail MailConfig
   }

   func LoadConfig() *Config {
       ...
       return &Config{
           App: AppConfig{...},
           Mail: MailConfig{
               Host: getEnv("MAIL_HOST", ""),
           },
       }
   }
   ```

3. Read it anywhere the config is resolvable:

   ```go
   cfg := c.App.Make("config").(*config.Config)
   fmt.Println(cfg.Mail.Host)
   ```
