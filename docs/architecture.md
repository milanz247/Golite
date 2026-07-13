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
│   └── app.go                    # AppConfig/LogConfig/HashConfig + Config, loaded from .env
├── container/
│   └── container.go              # Thread-safe IoC container (Bind / Make)
├── encryption/
│   └── encrypter.go              # AES-256-GCM Encrypter, persisted APP_KEY (Crypt facade equivalent)
├── hashing/
│   ├── hasher.go                     # Hasher interface
│   ├── bcrypt.go                     # BcryptHasher (golang.org/x/crypto/bcrypt)
│   └── manager.go                    # Driver-based Manager, bound as "hash"
├── validation/
│   ├── validator.go                  # Validator: Make/Fails/Passes/Errors/Validated
│   ├── rules.go                      # Built-in rules + the Extend registry
│   ├── errors.go                     # Errors (MessageBag-equivalent) + Exception
│   └── util.go                       # dot-notation lookup, emptiness check
├── logging/
│   ├── logger.go                     # Level enum, Entry, Channel/Logger interfaces
│   ├── single_channel.go             # "single" driver
│   ├── daily_channel.go              # "daily" driver, with pruning
│   ├── stack_channel.go              # "stack" driver (fan-out)
│   └── manager.go                    # Driver-based Manager, bound as "log"
├── bootstrap/
│   └── app.go                    # Application struct: wires container, config, providers, kernel
├── app/
│   ├── Providers/
│   │   ├── ServiceProvider.go        # ServiceProvider interface (Register/Boot)
│   │   ├── AppServiceProvider.go     # Binds core app services ("hash", "encrypter", "log")
│   │   └── RouteServiceProvider.go   # Maps routes onto the kernel during Boot
│   ├── Exceptions/
│   │   ├── exceptions.go             # HttpException, Abort/NotFound/Forbidden/Unauthorized/BadRequest
│   │   └── Handler.go                # Render (panic -> JSON response), ShouldReport
│   └── Http/
│       ├── Kernel.go                 # Kernel, Middleware, RouteDefinition, RouteGroup — the router
│       ├── Resource.go               # Route::resource/apiResource/singleton, Invokable, ControllerMiddleware
│       ├── Response.go               # Response factory, Responder/auto-conversion, macros, view rendering
│       ├── Context.go                # Context struct + methods (JSON, Redirect, Session, CsrfToken, files, ...)
│       ├── Input.go                  # Unified input payload: All/Input/Query/Has/Only/Except/Boolean/Merge
│       ├── Cookie.go                 # AES-256-GCM cookie encryption primitives
│       ├── UploadedFile.go           # UploadedFile: IsValid/Path/Extension/Store/StoreAs
│       ├── SessionBlock.go           # RouteDefinition.Block: per-session locking, coordinated w/ StartSessionMiddleware
│       ├── Session/                  # Driver-based session engine (own package: avoids an app/Http import cycle)
│       │   ├── SessionHandler.go         # Handler interface (PHP's SessionHandlerInterface, adapted)
│       │   ├── SessionManager.go         # Manager: driver registry, Load/Save, Extend, per-ID Lock
│       │   ├── Session.go                # Session: Get/Put/Push/Pull/Increment/Flash/Regenerate/...
│       │   ├── MemorySessionHandler.go   # Default driver: concurrent-safe in-memory map
│       │   ├── FileSessionHandler.go     # storage/sessions/*.json, one file per session
│       │   ├── CookieSessionHandler.go   # Stateless driver: payload lives in the cookie itself
│       │   ├── Lock.go                   # Per-session-ID *sync.Mutex registry (TryLock-polled, timeout-bounded)
│       │   └── crypto.go                 # AES-256-GCM + ID generation, duplicated from Cookie.go to avoid the cycle
│       ├── Middleware/
│       │   ├── RecoverMiddleware.go         # Global panic recovery, registered first; see app/Exceptions
│       │   ├── LoggerMiddleware.go          # Global, "after"-style middleware
│       │   ├── MethodSpoofingMiddleware.go  # Global, "before"-style: PUT/PATCH/DELETE spoofing for HTML forms
│       │   ├── RoleMiddleware.go            # Parameterized, struct-based (e.g. "role:editor,admin")
│       │   ├── AuditMiddleware.go           # Terminable: Handle + Terminate, DI-resolvable via the container
│       │   ├── StartSessionMiddleware.go    # Attaches a Session to every request; saves it back afterward
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
| `app/Http/Session/` (`Manager`, `Session`, `Handler`) | `Illuminate\Session\SessionManager` + `Store` + a driver |
| `resources/views/*.html`                  | `resources/views/*.blade.php`                |
| `app/Http/Middleware/*.go`                | `app/Http/Middleware/*.php`                  |
| `app/Http/Controllers/Controller.go`      | `Illuminate\Routing\Controller`              |
| `app/Http/Controllers/*.go`               | `app/Http/Controllers/*.php`                 |
| `routes/web.go`                           | `routes/web.php`                             |
| `public/main.go`                          | `public/index.php`                           |
| `config/app.go`                           | `config/app.php`                             |
| `encryption.Encrypter`                    | `Illuminate\Encryption\Encrypter` / `Crypt`  |
| `hashing.Manager`                         | `Illuminate\Hashing\HashManager` / `Hash`    |
| `validation.Validator`                    | `Illuminate\Validation\Validator`            |
| `app/Exceptions/` (`HttpException`, `Render`) | `App\Exceptions\Handler` + Laravel's exceptions |
| `app/Http/Middleware/RecoverMiddleware.go`| the implicit exception-handling wrapper every Laravel request runs inside |
| `logging.Manager`                         | `Illuminate\Log\LogManager` / `Log`          |

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
  type (`*hashing.Manager`, bound by `AppServiceProvider` — see
  [hashing.md](hashing.md)) never needs to be imported by the controller,
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
- **`RouteDefinition.WithMiddleware` attaches a concrete `Middleware`
  instance directly to a route, bypassing name-based resolution.**
  `.Block()` needs this: each call carries its own route-specific
  configuration (the lock timeout), so it can't be a reusable named
  singleton the way `.Middleware("auth")` aliases are. `WithMiddleware`
  entries are spliced into `resolveRouteMiddleware`'s output alongside the
  named ones, before priority sorting, so `MiddlewarePriority` still places
  them correctly — but `WithoutMiddleware` can't target them, since they
  bypass name resolution entirely. See
  [middleware.md](middleware.md#routedefinitionwithmiddleware--attaching-a-middleware-instance-not-just-a-name)
  and [sessions.md](sessions.md#session-blocking-block).
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
  restart, like the default `"memory"` session driver.** Golite is a
  single-process framework today, and the `"file"`/custom-driver options
  exist for anyone who needs persistence; a key that doesn't survive a
  restart is an honest reflection of the default rather than a half-real
  "persistent" key. Decryption failure (including from a stale pre-restart
  cookie) returns `ErrInvalidCookie`, not a panic — it fails safe. See
  [http-requests.md](http-requests.md#kernelappkey-generated-per-process-not-loaded-from-config).
- **`StartSessionMiddleware` queues the session cookie *before* `next()`
  runs, not after — the same Go-specific header-ordering constraint
  `VerifyCsrfToken` hit, but this time not optional to work around, since a
  session that never sends its cookie never persists at all.** Caught the
  same way: live end-to-end testing showed the cookie simply never reached
  the browser. Regenerate/Invalidate need the client to see a *new* ID in
  the same response, which the early-queued cookie can't reflect on its
  own — solved with `Context.RegenerateSession`/`InvalidateSession`, which
  update the already-queued cookie in place from inside the handler, before
  it writes its own response. See
  [sessions.md](sessions.md#a-go-specific-cookie-ordering-fix-the-same-class-of-bug-csrf-hit).
- **`RouteDefinition.Block()` reloads the session under its lock, not just
  the save.** The first version only serialized the *save*, leaving the
  *load* (done earlier by `StartSessionMiddleware`, with no lock available
  yet) racy — concurrent requests each mutated their own stale snapshot,
  and the lock just serialized the overwrites. Caught with a concurrency
  stress test (10 concurrent requests pushing to the same session's cart
  dropped 6 of them); fixed by reloading from the driver once the lock is
  actually held. See
  [sessions.md](sessions.md#why-blocking-has-to-reload-the-session-not-just-serialize-the-save).
- **Flash data ages in exactly one hook, `Manager.Load`, called once per
  request by `StartSessionMiddleware` before the handler runs.** This is
  what gives `Flash`/`Old` Laravel's real one-request-only visibility
  (verified directly: readable on request *N+1*, gone by *N+2*). See
  [sessions.md](sessions.md#flash-data).
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
- **Error handling is built on `panic`/`recover`, not a returned-error
  convention.** Go has no `throw`/`catch`, but `Context.Next` is already
  recursive (see [middleware.md](middleware.md#how-the-chain-runs--
  contextnext)), which means a `panic` unwinds through exactly the call
  chain `Next()` builds — so one `recover()`, in the outermost
  middleware (`middleware.Recover`), catches a panic from anywhere
  downstream with no changes needed to the router or `Context` itself.
  This is also why `Context.Validate` *panics* with a
  `*validation.Exception` on failure instead of returning one — it's what
  lets it mirror Laravel's `$request->validate()` throwing automatically,
  rather than forcing every handler to check an error return. See
  [error-handling.md](error-handling.md).
- **Not every recovered panic gets logged.** A failed validation or an
  intentional `404` is an expected, client-driven outcome, not an
  application error — `exceptions.ShouldReport` mirrors Laravel's
  `Handler::$dontReport` so `RecoverMiddleware` only writes genuinely
  reportable failures (5xx, or an unrecognized panic value) to the log.
  See [error-handling.md](error-handling.md#what-gets-logged--
  exceptionsshouldreport).
- **`encryption.Encrypter` and the cookie/session engine's own
  AES-256-GCM helpers are deliberately independent, not unified.**
  `Kernel.appKey` (cookie/session encryption) is intentionally
  regenerated every process restart (see the entry below); a general
  `Crypt`-equivalent service needs the opposite property — a key
  (`APP_KEY`) that persists in `.env` so encrypted application data
  survives a restart. Sharing one key/lifecycle between the two would
  have forced a compromise neither use case actually wants. See
  [encryption.md](encryption.md#why-a-separate-encrypter-from-
  cookiessessions).

See [bootstrapping.md](bootstrapping.md) for how the pieces are wired
together at startup, and [request-lifecycle.md](request-lifecycle.md) for
how a single HTTP request flows through them.
