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

`.env` is listed in [`.gitignore`](../.gitignore) and is **not** committed
to the repository — it's local/per-environment configuration. Each
deployment environment should have its own `.env` (or equivalent process
environment variables).

## `config.LoadConfig()`

```go
type AppConfig struct {
	Name string
	Env  string
	Port string
}

type Config struct {
	App AppConfig
}

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
```

- `godotenv.Load()` populates the process environment from `.env` if the
  file exists. If it doesn't (e.g. in production, where real environment
  variables are set directly), Golite logs a notice and continues —
  `getEnv` still reads from whatever is in the process environment.
- `getEnv(key, fallback)` returns the fallback if the variable is unset *or
  empty*, so an accidentally blank `.env` line doesn't produce an empty
  config value.

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
