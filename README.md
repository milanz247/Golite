# Golite

A small, Laravel-inspired web framework for Go. It keeps Laravel's request
lifecycle — a service container, service providers, an HTTP kernel with a
Register → Boot bootstrapping phase — but built with idiomatic Go: explicit
structs and interfaces instead of reflection-based magic.

## Features

- Thread-safe IoC container (`Bind` / `Make`)
- `.env`-based configuration (`github.com/joho/godotenv`)
- Service providers with a `Register()` / `Boot()` lifecycle
- An HTTP kernel (`http.Handler`) with a global middleware pipeline and
  simple route registration
- Example `LoggerMiddleware` and `UserController` wired end to end

## Requirements

- Go 1.20+

## Getting started

```bash
git clone https://github.com/milanz247/Golite.git
cd Golite
cp .env.example .env   # or create .env yourself, see below
go run ./public/main.go
```

In another terminal:

```bash
curl -i http://127.0.0.1:8080/user
```

### `.env`

```
APP_NAME=Golite
APP_ENV=local
APP_PORT=:8080
DB_HOST=127.0.0.1
DB_PORT=3306
```

`.env` is git-ignored — create your own in the project root before running
the app. See [docs/configuration.md](docs/configuration.md) for details.

## Project structure

```
Golite/
├── .env
├── go.mod / go.sum
├── config/           # Configuration loaded from .env
├── container/        # Thread-safe IoC container
├── bootstrap/        # Application struct: wires everything together
├── app/
│   ├── Providers/     # Service providers (Register/Boot)
│   └── Http/
│       ├── Kernel.go          # Router + middleware pipeline (http.Handler)
│       ├── Middleware/        # Global middleware
│       └── Controllers/       # Route handlers
├── routes/           # Route definitions (routes/web.go)
├── docs/             # Full documentation (start at docs/README.md)
└── public/           # Entry point (public/main.go)
```

## Documentation

Full framework documentation — architecture, the bootstrapping process, the
request lifecycle, the service container, providers, routing, middleware,
configuration, and a developer guide — lives in [`docs/`](docs/README.md).

## Building and testing

```bash
go build ./...
go vet ./...
```

## License

No license specified yet.
