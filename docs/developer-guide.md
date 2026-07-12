# Developer Guide

Practical, task-oriented notes for working in Golite day to day. For the
conceptual background behind each of these, see:
[architecture.md](architecture.md), [bootstrapping.md](bootstrapping.md),
[request-lifecycle.md](request-lifecycle.md),
[service-container.md](service-container.md),
[service-providers.md](service-providers.md), [routing.md](routing.md),
[middleware.md](middleware.md), [configuration.md](configuration.md).

## Requirements

- Go 1.20+
- A `.env` file in the project root (copy the variables listed in
  [configuration.md](configuration.md) if you don't have one)

## Running the app

```bash
go run ./public/main.go
```

You should see:

```
[AppServiceProvider] booted
[RouteServiceProvider] web routes mapped
[Golite] Golite is running on :8080 (local environment)
```

Then, in another terminal:

```bash
curl -i http://127.0.0.1:8080/user
```

## Building and checking

```bash
go build ./...   # compile everything
go vet ./...     # static analysis
```

Run both before committing — they catch import cycles, broken type
assertions, and unused code cheaply.

## Common tasks

### Add a new route + controller

1. Create `app/Http/Controllers/PostController.go`:

   ```go
   package controllers

   import (
       "net/http"

       apphttp "Golite/app/Http"
   )

   type PostController struct{}

   func NewPostController() *PostController {
       return &PostController{}
   }

   func (p *PostController) Show(c *apphttp.Context) {
       c.JSON(http.StatusOK, map[string]string{"post": c.Param("id")})
   }
   ```

2. Register the route in [`routes/web.go`](../routes/web.go), optionally
   with a parameter constraint and a name:

   ```go
   postController := controllers.NewPostController()
   kernel.GET("/posts/{id}", postController.Show).WhereNumber("id").Name("posts.show")
   ```

That's the whole change — `RouteServiceProvider` already calls
`MapWebRoutes` during boot, so no other file needs touching. See
[routing.md](routing.md) for the full set of route features (optional
parameters with defaults, `where*` constraints, named routes and URL
generation, groups, redirects, and the fallback route).

### Add a route group

```go
kernel.Prefix("admin").Middleware("auth").Name("admin.").Group(func(admin *apphttp.RouteGroup) {
    admin.GET("/users", userController.Index).Name("users") // GET /admin/users, "admin.users"
})
```

`Prefix`/`Middleware`/`Name` can be chained in any order and nested
(`admin.Prefix("posts").Group(...)` inside the closure above) — each call
returns a new `*RouteGroup` that extends the parent's attributes rather
than mutating it. See
[routing.md](routing.md#route-groups).

### Add a new service (bind + resolve)

1. Implement the service anywhere reasonable (a new package, or inline in a
   provider file for something small — see `Hasher` in
   `app/Providers/AppServiceProvider.go` for an example).
2. Bind it in a provider's `Register` method:

   ```go
   c.Bind("mailer", NewSMTPMailer(cfg.Mail))
   ```
3. Resolve it wherever you have access to the container
   (`Context.App` in a controller/middleware, or the `c
   *container.Container` parameter in a provider):

   ```go
   mailer := c.App.Make("mailer").(*Mailer)
   ```

   If the consumer would need to import the provider's package just for the
   type (and that import would create a cycle — e.g. a controller needing a
   type declared in `app/Providers`), declare a small local interface with
   just the methods you need instead of importing the concrete type. See
   `hashService` in `app/Http/Controllers/UserController.go` for the
   pattern, and [service-container.md](service-container.md#resolving-a-service-without-an-import-cycle)
   for why.

### Add a new service provider

See [service-providers.md](service-providers.md#writing-your-own-provider).

### Add global middleware

See [middleware.md](middleware.md#writing-your-own-middleware). Remember to
register it in `public/main.go` via `app.Kernel.UseMiddleware(...)` — it
won't run otherwise. Global middleware always runs *before* routing is
resolved (see [request-lifecycle.md](request-lifecycle.md)), which matters
for anything that can change which route matches, like
`MethodSpoofingMiddleware`.

### Add middleware scoped to a route or group

Register a named alias once, then reference it by string wherever needed:

```go
kernel.AliasMiddleware("auth", appMiddleware.Authenticate())

kernel.GET("/account", handler).Middleware("auth")
kernel.Prefix("admin").Middleware("auth").Group(func(r *apphttp.RouteGroup) { ... })
```

See [middleware.md](middleware.md#middleware-aliases).

### Add a new config value

See [configuration.md](configuration.md#adding-a-new-config-value).

## Known limitations / extension points

- **Optional parameters must trail the route.** `/a/{b?}/{c}` (required
  after optional) isn't specially handled — same constraint Laravel
  imposes. See [routing.md](routing.md#route-parameters).
- **Route matching is a linear scan** over the registered routes (trying
  each in registration order), not a radix/trie structure. This is the same
  approach Laravel itself uses and is fine at the route counts a
  lightweight framework expects; if the route table grows very large, a
  trie keyed by static path segments would be the natural next step.
- **The container has no auto-wiring.** `Bind`/`Make` are name + manual
  type-assertion based, on purpose — there's no reflection-based
  constructor injection like Laravel's automatic resolution. Keep bindings
  explicit.

## Import path / package naming gotcha

`app/Http`'s Go package is named `http` (matching Laravel's `Http`
namespace), which collides with the standard library's `net/http` package
name. Any file that needs both must alias one — the convention used
throughout this codebase is:

```go
import (
    "net/http"

    apphttp "Golite/app/Http"
)
```

Keep using `apphttp` for consistency if you add new files that need both.
