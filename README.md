# Golite

A small, Laravel-inspired web framework for Go. It keeps Laravel's request
lifecycle — a service container, service providers, an HTTP kernel with a
Register → Boot bootstrapping phase — but built with idiomatic Go: explicit
structs and interfaces instead of reflection-based magic.

## Features

- Thread-safe IoC container (`Bind` / `Make`)
- `.env`-based configuration (`github.com/joho/godotenv`)
- Service providers with a `Register()` / `Boot()` lifecycle
- An HTTP kernel (`http.Handler`) with a recursive middleware pipeline
- A native, Laravel-style routing engine (no external router): HTTP verb
  helpers (`GET`/`POST`/`PUT`/`PATCH`/`DELETE`/`OPTIONS`, `Match`, `Any`),
  required/optional `{param}` segments with defaults, regex constraints
  (`Where`, `WhereNumber`, `WhereAlpha`, `WhereAlphaNumeric`, `WhereIn`),
  named routes with URL generation, nested route groups (prefix +
  middleware + name prefix), redirects, a fallback route, and proper
  404/405 handling
- A Laravel-standard middleware system: global, named/aliased, and grouped
  middleware registries, a `MiddlewarePriority` order enforced regardless of
  assignment order, parameterized middleware (`"role:editor,admin"`),
  per-route exclusion (`WithoutMiddleware`), terminable middleware
  (post-response cleanup via `Terminate`), and middleware structs resolvable
  straight from the service container for dependency injection
- `LoggerMiddleware`, `MethodSpoofingMiddleware` (PUT/PATCH/DELETE via
  `_method` or `X-HTTP-Method-Override`, for plain HTML forms),
  `RoleMiddleware` (parameterized), and `AuditMiddleware` (terminable) as
  worked examples
- A driver-based session engine (`"memory"`/`"file"`/`"cookie"`, plus
  `Manager.Extend` for Redis/database/custom drivers), attached to every
  request by `StartSessionMiddleware`: a full expressive `Session` API
  (`Get`/`Put`/`Push`/`Pull`/`All`/`Has`/`Exists`/`Missing`/`Increment`/
  `Decrement`/`Forget`/`Flush`), one-shot flash data (`Flash`/`Now`/
  `Reflash`/`Keep`), fixation-safe `Regenerate`/`Invalidate`, and
  `.Block()` for atomic per-session locking against concurrent requests
- Laravel-style CSRF protection built on the session engine:
  `Context.CsrfToken()`, `VerifyCsrfToken` middleware (checked against the
  `_token` field, `X-CSRF-TOKEN`, or `X-XSRF-TOKEN`, in constant time),
  wildcard `Except` path exclusions, and an auto-synced `XSRF-TOKEN` cookie
  for Axios/Angular
- A full Laravel-style request API on `Context`: inspection helpers
  (`Path`/`Is`/`Url`/`FullUrl`/`Method`/`Ip`/`BearerToken`/`ExpectsJson`), a
  unified input payload merging query/JSON/form data (`All`/`Input`/`Query`/
  `Has`/`Only`/`Except`/`Boolean`/`Merge`), AES-256-GCM encrypted +
  authenticated cookies, one-shot flash data (`Flash`/`Old`, for
  form-repopulation-after-redirect), and file uploads (`HasFile`/`File`,
  `UploadedFile.Store`/`StoreAs` with content-sniffed extensions) — plus
  `TrimStrings`/`ConvertEmptyStringsToNull` input-normalization middleware
  and `TrustProxies`/`TrustHosts` for safely resolving the real client IP
  and rejecting Host header injection
- A Laravel-standard controller layer: a base `Controller` every
  controller can embed for its own per-action middleware
  (`.Middleware("auth").Except("index", "show")`), both constructor
  dependency injection *and* Laravel-style automatic method injection
  (`func (u *UserController) Show(c *apphttp.Context, hash Hasher, cfg *config.Config)`,
  wired up via `apphttp.Inject`, resolved by type from the service
  container), single-action (`Invokable`) controllers, and
  `Route::resource`/`apiResource`/`singleton` — reflection-based (a
  controller need only implement the actions it has), with nested
  (`"photos.comments"`) and `.Shallow()` routing, and
  `.Only(...)`/`.Except(...)` filtering
- A Laravel-standard response layer: handlers can optionally return a
  value (`apphttp.Responder`) instead of writing the response themselves —
  a string auto-sends as `text/html`, a struct/map/slice as JSON — and a
  fluent `*Response` factory (`c.Response(...)`) covers
  `Status`/`Header`/`WithHeaders`/`Cookie`/`WithoutCookie`, forced `Json`,
  `View` (Go `html/template`, cached), `Download`/`File`/`StreamDownload`,
  redirects (`Redirect`/`Back`/`Away`) with one-shot flash data
  (`With`/`WithInput`), and a global macro registry
  (`ResponseFactory.Macro(...)`) for reusable custom responses
- A general-purpose `encryption.Encrypter` (AES-256-GCM), bound as
  `"encrypter"` from a persisted `APP_KEY` — Laravel's `Crypt` facade
  equivalent, deliberately independent from the cookie/session engine's own
  ephemeral key
- A driver-based `hashing.Manager` (bcrypt by default), bound as `"hash"` —
  `Make`/`Check`/`NeedsRehash`, plus `Extend` for custom drivers
- Laravel-style validation: pipe-separated rule strings
  (`"required|email|min:3"`, 17 built-ins, `Extend` for custom rules)
  against a `map[string]any` payload, and `Context.Validate` for an
  automatic 422 JSON response on failure
- `panic`/`recover`-based error handling: `exceptions.HttpException` +
  `abort()`-style constructors, a global `RecoverMiddleware` that renders
  JSON error responses (with debug-gated detail) from anywhere downstream,
  and `ShouldReport` to keep expected 4xx conditions out of the log
- A driver-based `logging.Manager` with PSR-3 levels and
  `"single"`/`"daily"`/`"stack"` channels, bound as `"log"`, writing to
  `storage/logs/`

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
the app. See [.env.example](.env.example) for the full set of variables
(including `APP_KEY`, `LOG_CHANNEL`, and `HASH_DRIVER` — all optional, with
sane defaults) and [docs/configuration.md](docs/configuration.md) for
details.

## Project structure

```
Golite/
├── .env
├── go.mod / go.sum
├── config/           # Configuration loaded from .env
├── container/        # Thread-safe IoC container
├── encryption/       # AES-256-GCM Encrypter (Crypt facade equivalent, persisted APP_KEY)
├── hashing/          # Driver-based Hasher/Manager (bcrypt default)
├── validation/       # Rule-string Validator, built-in + custom rules
├── logging/          # Driver-based logging Manager (single/daily/stack channels)
├── bootstrap/        # Application struct: wires everything together
├── app/
│   ├── Providers/     # Service providers (Register/Boot)
│   ├── Exceptions/    # HttpException, abort()-style helpers, panic -> JSON rendering
│   └── Http/
│       ├── Kernel.go          # Regex router, groups, named routes, middleware registries + pipeline (http.Handler)
│       ├── Resource.go        # Route::resource/apiResource/singleton, Invokable controllers
│       ├── Response.go        # Response factory, Responder/auto-conversion, macros, view rendering
│       ├── Context.go         # Per-request Context: params, JSON/Redirect, Session, CsrfToken, cookies, files, Validate
│       ├── Input.go           # Unified input payload (query + JSON/form body)
│       ├── Cookie.go          # AES-256-GCM cookie encryption
│       ├── UploadedFile.go    # Uploaded file handling
│       ├── SessionBlock.go    # RouteDefinition.Block: atomic per-session locking
│       ├── Session/           # Driver-based session engine (memory/file/cookie + custom drivers)
│       ├── Middleware/        # Global, aliased, grouped, parameterized, terminable, session, CSRF, panic-recovery & normalization middleware
│       └── Controllers/       # Base Controller + route handlers (resource, nested, singleton, invokable demos)
├── routes/           # Route definitions (routes/web.go)
├── resources/views/  # html/template files for Response.View
├── docs/             # Full documentation (start at docs/README.md)
└── public/           # Entry point (public/main.go)
```

## Documentation

Full framework documentation — architecture, the bootstrapping process, the
request lifecycle, the service container, providers, routing, middleware,
CSRF protection, HTTP request handling, controllers & resource routing,
response handling, encryption, hashing, validation, error handling,
logging, configuration, and a developer guide — lives in
[`docs/`](docs/README.md).

## Building and testing

```bash
go build ./...
go vet ./...
```

## License

No license specified yet.
