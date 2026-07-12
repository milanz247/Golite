# Architecture Overview

Golite is a small, Laravel-inspired web framework for Go. It keeps Laravel's
mental model — a service container, service providers, an HTTP kernel, and a
Register → Boot lifecycle — but implements each piece in idiomatic Go
(structs, interfaces, and explicit dependency passing instead of magic
reflection/facades).

## Folder structure

```
Golite/
├── .env                          # Local environment variables (not committed)
├── go.mod / go.sum
├── config/
│   └── app.go                    # AppConfig + Config, loaded from .env
├── container/
│   └── container.go              # Thread-safe IoC container (Bind / Make)
├── bootstrap/
│   └── app.go                    # Application struct: wires container, config, providers, kernel
├── app/
│   ├── Providers/
│   │   ├── ServiceProvider.go        # ServiceProvider interface (Register/Boot)
│   │   ├── AppServiceProvider.go     # Binds core app services (e.g. "hash")
│   │   └── RouteServiceProvider.go   # Maps routes onto the kernel during Boot
│   └── Http/
│       ├── Kernel.go                 # Kernel, Context, HandlerFunc, RouteDefinition, RouteGroup — the router
│       ├── Middleware/
│       │   ├── LoggerMiddleware.go          # Example global middleware
│       │   └── MethodSpoofingMiddleware.go  # PUT/PATCH/DELETE spoofing for HTML forms
│       └── Controllers/
│           └── UserController.go     # Example controller
├── routes/
│   └── web.go                    # MapWebRoutes: registers paths onto the kernel
└── public/
    └── main.go                   # Entry point / front controller
```

## How the pieces map to Laravel

| Golite                                   | Laravel equivalent                          |
|-------------------------------------------|----------------------------------------------|
| `container.Container`                     | `Illuminate\Container\Container`             |
| `bootstrap.Application`                   | `Illuminate\Foundation\Application`          |
| `bootstrap/app.go`                        | `bootstrap/app.php`                          |
| `app/Providers/*ServiceProvider.go`       | `app/Providers/*ServiceProvider.php`         |
| `app/Http/Kernel.go` (`Kernel`)           | `app/Http/Kernel.php`                        |
| `app/Http/Kernel.go` (`Context`)          | `Illuminate\Http\Request` + response helpers |
| `app/Http/Middleware/*.go`                | `app/Http/Middleware/*.php`                  |
| `app/Http/Controllers/*.go`               | `app/Http/Controllers/*.php`                 |
| `routes/web.go`                           | `routes/web.php`                             |
| `public/main.go`                          | `public/index.php`                           |
| `config/app.go`                           | `config/app.php`                             |

## Design decisions worth knowing

- **`Context.App` is `*container.Container`, not `*bootstrap.Application`.**
  Importing `bootstrap` from `app/Http` would create an import cycle
  (`bootstrap → app/Http → bootstrap`). This isn't a compromise: in Laravel,
  `Application` *extends* `Container`, so `$app` and the container are the
  same object for resolution purposes (`app()->make(...)`). Naming the field
  `App` preserves that semantic in Go without the cycle.
- **Controllers never import `app/Providers`.** `UserController` declares a
  small local interface (`hashService`) and type-asserts whatever the
  container returns for `"hash"`. Go's structural typing means the concrete
  type (`providers.Hasher`) never needs to be imported by the controller,
  which avoids a `controllers → providers → routes → controllers` cycle.
- **Package `http` under `app/Http`.** It shares its name with the standard
  library's `net/http` on purpose (mirroring Laravel's `Illuminate\Http`
  namespace) so framework code reads as `http.Context`, `http.Kernel`,
  `http.HandlerFunc`. Any file that needs both packages imports Golite's as
  `apphttp` to avoid a naming collision — see any file under `app/Providers`,
  `app/Http/Controllers`, `app/Http/Middleware`, `routes`, or `public`.
- **The registered-route type is `RouteDefinition`, not `Route`.** Laravel's
  global URL helper is `route($name, $params)`; Golite mirrors it as a
  package-level function `apphttp.Route(name, params)` (see
  [routing.md](routing.md#named-routes-and-url-generation)). Go doesn't
  allow a top-level function and a top-level type to share an identifier in
  the same package, so the struct that `kernel.GET(...)` etc. return is
  named `RouteDefinition` instead, freeing up `Route` for the URL-generation
  helper.
- **`Context.Next()` is recursive, not an iterating loop.** An earlier
  iterative (Gin-style `for`) implementation kept advancing to the next
  handler regardless of whether the current one called `Next()`, so a
  middleware that returned early (e.g. failed auth) didn't actually stop
  the chain — the controller ran anyway and double-wrote the response. The
  recursive form in `app/Http/Kernel.go` makes "don't call `Next()`" a real
  short-circuit, with no separate `Abort()` needed. See
  [middleware.md](middleware.md#how-the-chain-runs--contextnext).

See [bootstrapping.md](bootstrapping.md) for how the pieces are wired
together at startup, and [request-lifecycle.md](request-lifecycle.md) for
how a single HTTP request flows through them.
