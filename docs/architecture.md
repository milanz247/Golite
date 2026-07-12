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
│       ├── Kernel.go                 # Kernel, Middleware, RouteDefinition, RouteGroup — the router
│       ├── Resource.go               # Route::resource/apiResource/singleton, Invokable, ControllerMiddleware
│       ├── Response.go               # Response factory, Responder/auto-conversion, macros, view rendering
│       ├── Context.go                # Context struct + methods (JSON, Redirect, Session, CsrfToken, files, ...)
│       ├── Input.go                  # Unified input payload: All/Input/Query/Has/Only/Except/Boolean/Merge
│       ├── Cookie.go                 # AES-256-GCM cookie encryption primitives
│       ├── UploadedFile.go           # UploadedFile: IsValid/Path/Extension/Store/StoreAs
│       ├── Session.go                # Session, SessionStore — in-memory, crypto/rand-backed
│       ├── Middleware/
│       │   ├── LoggerMiddleware.go          # Global, "after"-style middleware
│       │   ├── MethodSpoofingMiddleware.go  # Global, "before"-style: PUT/PATCH/DELETE spoofing for HTML forms
│       │   ├── RoleMiddleware.go            # Parameterized, struct-based (e.g. "role:editor,admin")
│       │   ├── AuditMiddleware.go           # Terminable: Handle + Terminate, DI-resolvable via the container
│       │   ├── VerifyCsrfToken.go           # CSRF protection: session-bound token, Except wildcards, XSRF-TOKEN cookie
│       │   ├── TrimStringsMiddleware.go             # Trims whitespace from every input string
│       │   ├── ConvertEmptyStringsToNullMiddleware.go # "" input values -> nil, key kept
│       │   ├── TrustProxiesMiddleware.go            # Resolves the real client IP from a trusted proxy's X-Forwarded-For
│       │   └── TrustHosts.go                        # Rejects requests with an untrusted Host header
│       └── Controllers/
│           ├── Controller.go                  # Base Controller: per-action middleware rules
│           ├── UserController.go              # Example controller
│           ├── PostController.go               # Full resource + DI + controller middleware
│           ├── CommentController.go             # Nested + shallow resource demo
│           ├── ProfileController.go             # Singleton resource demo
│           └── ProvisionServerController.go     # Invokable (single-action) controller demo
├── routes/
│   └── web.go                    # MapWebRoutes: registers paths onto the kernel
├── resources/
│   └── views/
│       └── welcome.html          # Sample html/template for Response.View
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
| `app/Http/Resource.go`                    | `Illuminate\Routing\ResourceRegistrar` / `PendingResourceRegistration` |
| `app/Http/Response.go` (`Response`)       | `Illuminate\Http\Response` / `RedirectResponse` |
| `app/Http/Context.go` (`Context`)         | `Illuminate\Http\Request` + response helpers |
| `app/Http/Session.go`                     | `Illuminate\Session\Store` + a driver        |
| `resources/views/*.html`                  | `resources/views/*.blade.php`                |
| `app/Http/Middleware/*.go`                | `app/Http/Middleware/*.php`                  |
| `app/Http/Controllers/Controller.go`      | `Illuminate\Routing\Controller`              |
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
  recursive form in `app/Http/Context.go` makes "don't call `Next()`" a real
  short-circuit, with no separate `Abort()` needed. See
  [middleware.md](middleware.md#how-the-chain-runs--contextnext).
- **Middleware is an interface (`Handle(c, next, params...)`), not just a
  function type.** A bare `func(*Context)` can't carry parameters, a
  `Terminate` method, or constructor-injected fields, so it can't satisfy
  parameterized (`"role:editor,admin"`), terminable, or DI-friendly
  middleware. `MiddlewareFunc` adapts a plain closure into the interface
  for the common stateless case (mirroring `http.HandlerFunc`), while
  middleware that needs any of the above is a small struct instead. See
  [middleware.md](middleware.md#the-middleware-contract).
- **A middleware instance is shared across every concurrent request, so it
  must never hold per-request state on itself.** `Context.Set`/`Get` exist
  specifically so a middleware can pass data from `Handle` to its own
  `Terminate` through the (per-request, goroutine-safe) `Context` instead —
  see `AuditMiddleware` and
  [middleware.md](middleware.md#terminable-middleware).
- **`VerifyCsrfToken` sets its cookie *before* calling `next()`, not after,
  unlike Laravel.** Laravel adds the `XSRF-TOKEN` cookie once
  `$next($request)` returns, because PHP's `Response` stays a mutable
  object until the framework explicitly flushes it. Go's
  `http.ResponseWriter` streams headers immediately — once a downstream
  handler calls `WriteHeader`, any header set afterward is silently
  dropped. This was caught by testing (the cookie simply never appeared in
  the response) and fixed by reordering; see
  [security-csrf.md](security-csrf.md#the-xsrf-token-cookie-and-a-go-specific-ordering-fix).
- **`Context.Ip()` never reads a forwarded-for header itself.** Only
  `TrustProxiesMiddleware`, after validating the immediate TCP peer is an
  actually-trusted proxy, is allowed to promote a forwarded address into
  `Request.RemoteAddr` — which is the only thing `Ip()` reads. Putting the
  trust decision in exactly one place, upstream, means every consumer of
  `Ip()` inherits it automatically instead of needing to re-implement (or
  forget to implement) the same validation. See
  [http-requests.md](http-requests.md#ip-is-deliberately-dumb--trustproxiesmiddleware-is-what-makes-it-safe).
- **`Kernel.appKey` (cookie encryption) is generated fresh every process
  restart, like `SessionStore`.** Golite is a single-process, no-persistence
  framework today; a key that doesn't survive a restart is an honest
  reflection of that rather than a half-real "persistent" key. Decryption
  failure (including from a stale pre-restart cookie) returns
  `ErrInvalidCookie`, not a panic — it fails safe. See
  [http-requests.md](http-requests.md#kernelappkey-generated-per-process-not-loaded-from-config).
- **Flash data ages in exactly one hook, `Context.Session()`, guarded by
  the same idempotency check that already makes `Session()` safe to call
  more than once per request.** This is what gives `Flash`/`Old` Laravel's
  real one-request-only visibility (verified directly: readable on request
  *N+1*, gone by *N+2*) without a separate "start of request" hook the
  Kernel would otherwise need to call explicitly. See
  [http-requests.md](http-requests.md#one-shot-semantics-visible-on-the-next-request-then-gone).
- **`Route::resource`'s "automatically inspect the controller" is real
  reflection, not a required interface.** A controller doesn't implement
  some `ResourceController` interface covering all 7 actions — `reflect`
  checks, per action, whether a same-named method with exactly the
  signature `func(*Context)` exists, and simply skips the route if not.
  This lets a controller legitimately implement a subset (the demo
  `PostController` has no `Create`/`Edit`) without needing `.Except(...)`
  to say so, at the cost of a request that *would* have hit a skipped
  action instead falling through to whatever else matches — same as real
  Laravel under `apiResource()`. See
  [controllers.md](controllers.md#automatically-inspect-the-controller--reflection-not-a-required-interface).
- **`ResourceRegistrar`/`SingletonRegistrar` re-register their entire route
  set on every fluent call, instead of deferring registration the way
  Laravel's `PendingResourceRegistration` does.** Go has no destructor to
  hook "no more chaining is coming" into, so `Only`/`Except`/`Shallow`/
  `Creatable` each remove whatever the previous call registered
  (`Kernel.removeRoutes`, by `*RouteDefinition` pointer identity) and
  rebuild from scratch. Verified directly that chaining `Only` then
  `Except` replaces rather than compounds the restriction. See
  [controllers.md](controllers.md#how-onlyexceptshallowcreatable-avoid-leaking-stale-routes).
- **`ResponderFunc` is a separate type from `HandlerFunc`, not a change to
  it.** Requirement was that handlers can *optionally* return a value;
  Go can't make a single function type's return "optional," and changing
  `HandlerFunc` itself would have forced every existing controller and
  route closure (dozens of call sites) to add a bare `return nil`. Wrapping
  a `ResponderFunc` with `Responder` opts in per-route instead, so nothing
  written before this turn needed to change. See
  [responses.md](responses.md#dynamic-return-type-serialization).
- **`Response.Send` is exported, not called only from inside the
  package.** A `*Response` can be delivered two ways: returned from a
  `Responder`-wrapped handler (auto-sent), or built and sent explicitly
  from a plain `HandlerFunc` via `.Send(c)` — see `/contact`'s
  validation-failure branch. Keeping `Send` public gives both paths full
  support rather than forcing every fluent-response route through
  `Responder`.
- **`NewResponse`, not `Response`, is the package-level constructor.**
  Same constraint as `RouteDefinition`/`Route`: Go doesn't allow a
  function and a type to share an identifier in the same package, and the
  *type* needs to stay named `Response` (referenced everywhere as
  `*Response`). A macro registered from `AppServiceProvider` — which has
  no `*Context` to call `Context.Response` on — is what actually needs the
  package-level form. See
  [responses.md](responses.md#response-macros).

See [bootstrapping.md](bootstrapping.md) for how the pieces are wired
together at startup, and [request-lifecycle.md](request-lifecycle.md) for
how a single HTTP request flows through them.
