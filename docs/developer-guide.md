# Developer Guide

Practical, task-oriented notes for working in Golite day to day. For the
conceptual background behind each of these, see:
[architecture.md](architecture.md), [bootstrapping.md](bootstrapping.md),
[request-lifecycle.md](request-lifecycle.md),
[service-container.md](service-container.md),
[service-providers.md](service-providers.md), [routing.md](routing.md),
[middleware.md](middleware.md), [sessions.md](sessions.md),
[security-csrf.md](security-csrf.md),
[http-requests.md](http-requests.md), [controllers.md](controllers.md),
[responses.md](responses.md), [configuration.md](configuration.md).

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

1. Create `app/Http/Controllers/TagController.go`, embedding the base
   `Controller` (see [controllers.md](controllers.md)) for consistency,
   even if this particular controller declares no middleware of its own:

   ```go
   package controllers

   import (
       "net/http"

       apphttp "Golite/app/Http"
   )

   type TagController struct {
       Controller
   }

   func NewTagController() *TagController {
       return &TagController{}
   }

   func (t *TagController) Show(c *apphttp.Context) {
       c.JSON(http.StatusOK, map[string]string{"tag": c.Param("id")})
   }
   ```

2. Register the route in [`routes/web.go`](../routes/web.go), optionally
   with a parameter constraint and a name:

   ```go
   tagController := controllers.NewTagController()
   kernel.GET("/tags/{id}", tagController.Show).WhereNumber("id").Name("tags.show")
   ```

That's the whole change — `RouteServiceProvider` already calls
`MapWebRoutes` during boot, so no other file needs touching. See
[routing.md](routing.md) for the full set of route features (optional
parameters with defaults, `where*` constraints, named routes and URL
generation, groups, redirects, and the fallback route).

### Register a full RESTful resource controller

For a controller implementing several of the standard actions
(`Index`/`Create`/`Store`/`Show`/`Edit`/`Update`/`Destroy`), skip the
one-route-at-a-time approach above:

```go
postController := controllers.NewPostController(kernel.Container().Make("hash").(controllers.Hasher))

kernel.Resource("posts", postController)                  // all 7 (or however many exist)
kernel.ApiResource("posts", postController)                // 5, no Create/Edit
kernel.Resource("posts", postController).Only([]string{"index", "show"})
kernel.Resource("photos.comments", commentController).Shallow() // nested, with member routes promoted to /comments/{comment}
kernel.Singleton("profile", profileController).Creatable() // no {id}; +Create/Store
```

A controller method only gets a route if it actually exists (checked via
reflection) — no need to implement all 7 if a controller is naturally a
subset (e.g. read-only). See [controllers.md](controllers.md) for the full
route tables, nested/shallow semantics, and how controller-level
middleware (`Controller.Middleware(...).Only(...)/.Except(...)`) attaches
automatically to each generated route.

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

Register a named alias once, then reference it by string wherever needed —
plain, parameterized (`"role:editor,admin"`), or as part of a
`MiddlewareGroup`:

```go
kernel.AliasMiddleware("auth", appMiddleware.Authenticate())
kernel.AliasMiddleware("role", middleware.NewRole())
kernel.MiddlewareGroup("web", "auth", "audit")

kernel.GET("/account", handler).Middleware("auth")
kernel.GET("/posts/{p}/edit", handler).Middleware("auth", "role:editor,admin")
kernel.Middleware("web").Group(func(r *apphttp.RouteGroup) { ... })
```

A route inside a group can opt out of one specific middleware the group
would otherwise contribute:

```go
admin.GET("/health", handler).WithoutMiddleware("audit")
```

And execution order is always normalized by `kernel.MiddlewarePriority`,
regardless of the order middleware was assigned. See
[middleware.md](middleware.md) for all of the above, plus terminable
middleware (`Terminate`, for post-response cleanup) and resolving a
middleware struct straight from the service container via
`kernel.Container().Bind(...)`.

### Add a new config value

See [configuration.md](configuration.md#adding-a-new-config-value).

### Attach a session to a route

The `"session"` middleware name is already seeded into the `"web"` group by
`NewKernel`, ahead of `"csrf"`; give it a real implementation once in
`routes/web.go` and attach it to whichever routes call `c.Session()`
(directly, or indirectly via `CsrfToken`/`Flash`/`Old`/a redirect's
`.With`/`.WithInput`):

```go
kernel.AliasMiddleware("session", middleware.NewStartSession(kernel.Sessions()))

kernel.GET("/session/visit", apphttp.Responder(func(c *apphttp.Context) any {
    return map[string]any{"visits": c.Session().Increment("visits")}
})).Middleware("session")
```

`c.Session()` panics with a clear message if `"session"` isn't attached to
the matched route — it never creates one lazily. See
[sessions.md](sessions.md) for the full `Session` API
(`Get`/`Put`/`Push`/`Pull`/`Increment`/`Regenerate`/...), flash data,
custom drivers, and `.Block()` for atomic per-session locking.

### Protect a route with CSRF

The `"csrf"` middleware name is also seeded into the `"web"` group, right
after `"session"` (CSRF needs one); give it a real implementation once in
`routes/web.go` and attach both to whichever routes need it:

```go
kernel.AliasMiddleware("csrf", middleware.NewVerifyCsrfToken("/stripe/*"))

kernel.GET("/comments", func(c *apphttp.Context) {
    c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
}).Middleware("session", "csrf")

kernel.POST("/comments", handler).Middleware("session", "csrf")
```

The client must echo the token back via the `_token` form field,
`X-CSRF-TOKEN`, or `X-XSRF-TOKEN` header on every `POST`/`PUT`/`PATCH`/
`DELETE` to a CSRF-protected route, or the request gets a `419`. See
[security-csrf.md](security-csrf.md) for the full mechanism and the
`Except` wildcard exclusions for things like payment webhooks.

### Read request input, set a cookie, or handle a file upload

```go
// merges query + JSON/form body; body wins on collision
name := c.Input("name", "anonymous")
if !c.Has("email") {
    c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "email is required"})
    return
}
subscribed := c.Boolean("newsletter") // "1"/"true"/"on"/"yes" -> true

_ = c.SetCookie("preferred_theme", "dark", 3600) // AES-256-GCM encrypted + authenticated
theme, err := c.Cookie("preferred_theme")        // ErrInvalidCookie if missing/tampered/stale key

if c.HasFile("avatar") {
    file, err := c.File("avatar")
    if err == nil && file.IsValid() {
        path, err := file.Store("storage/avatars") // auto-generated unique filename
    }
}
```

See [http-requests.md](http-requests.md) for the full API (`All`/`Query`/
`Only`/`Except`/`Merge`/..., `Flash`/`Old` for form repopulation after a
redirect, `UploadedFile`'s `Extension`/`Store`/`StoreAs`) and the global
`TrimStrings`/`ConvertEmptyStringsToNull`/`TrustProxies`/`TrustHosts`
middleware that normalize and secure that input before a handler sees it.

### Return a value from a handler instead of writing the response

Wrap the handler in `apphttp.Responder` and return whatever you want sent
— a string, a struct/map/slice (JSON), or a `*Response`:

```go
kernel.GET("/status", apphttp.Responder(func(c *apphttp.Context) any {
    return map[string]string{"status": "ok"} // -> application/json, 200
}))
```

The fluent factory covers everything else — headers, cookies, redirects
with flash data, forced JSON, rendered views, downloads, and streaming:

```go
kernel.GET("/report", apphttp.Responder(func(c *apphttp.Context) any {
    return c.Response(nil).
        Header("X-Report-Version", "2").
        StreamDownload(func(w io.Writer) {
            fmt.Fprintln(w, "generated on the fly, no temp file")
        }, "report.txt")
}))

kernel.POST("/posts", func(c *apphttp.Context) {
    if !c.Has("title") {
        c.Redirect("/posts/create", http.StatusFound).WithInput().With("error", "Title is required").Send(c)
        return
    }
    // ...
})
```

See [responses.md](responses.md) for the full API, including `View` (Go
`html/template` from `resources/views/`) and registering a custom
response macro via `apphttp.ResponseFactory.Macro(...)`.

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
- **The default `"memory"` session driver is process-local**, with no
  persistence across a restart; `"file"` and `"cookie"` are available for
  more durable use cases, and `Manager.Extend` covers anything else
  (Redis, a database). See [sessions.md](sessions.md).
- **The stateless `"cookie"` session driver only reliably reflects a
  mid-request session mutation for routes that don't themselves write a
  response afterward** — a structural limitation of Go's
  `http.ResponseWriter`, not a bug. Prefer `"memory"`/`"file"` otherwise.
  See [sessions.md](sessions.md#the-stateless-cookie-driver-a-real-limitation).
- **Cookie encryption key (`Kernel.appKey`) is also process-local** — a
  cookie set before a restart won't decrypt after one (`Context.Cookie`
  returns `ErrInvalidCookie`, not a crash). Same tradeoff as the default
  session driver, for the same reason. See [http-requests.md](http-requests.md#kernelappkey-generated-per-process-not-loaded-from-config).
- **No request size limit beyond the 32 MiB passed to
  `ParseMultipartForm`.** Add your own (e.g. `http.MaxBytesReader` around
  `Request.Body`) ahead of anything that calls `File`/`All` if you're
  fielding untrusted uploads. See [http-requests.md](http-requests.md#known-simplifications).
- **`singularize` is a heuristic, not a full English inflector**, and
  singleton resources don't support dot-notation nesting. Both are
  reasonable, documented scope cuts — see
  [controllers.md](controllers.md#singularize-a-deliberately-simple-heuristic).
- **Rendered views (`Response.View`) are parsed once and cached, with no
  invalidation** — edit a template, restart the server. Same tradeoff as
  sessions and the cookie key. See
  [responses.md](responses.md#specialized-response-formats).
- **`.Status(code)` has no effect on `Download`/`File` responses** — the
  standard library's `http.ServeFile` decides the final status itself
  (200, 304, 416, ...). Headers set via `.Header()`/`.WithHeaders()` still
  apply. See [responses.md](responses.md#specialized-response-formats).

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
