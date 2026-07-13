# Authentication

Files: [`auth/`](../auth/) (`guard.go`, `remember.go`, `password_reset.go`,
`token.go`, `errors.go`),
[`app/Providers/AuthServiceProvider.go`](../app/Providers/AuthServiceProvider.go),
[`app/Http/Controllers/AuthController.go`](../app/Http/Controllers/AuthController.go),
[`app/Http/Middleware/AuthMiddleware.go`](../app/Http/Middleware/AuthMiddleware.go),
[`app/Models/User.go`](../app/Models/User.go),
[`database/migrations/2026_07_13_000001_create_users_table.go`](../database/migrations/2026_07_13_000001_create_users_table.go),
[`database/migrations/2026_07_13_000002_create_password_reset_tokens_table.go`](../database/migrations/2026_07_13_000002_create_password_reset_tokens_table.go)

Golite's `auth` package is the equivalent of `Illuminate\Auth`:
session-based authentication (register/login/logout), "remember me"
persistent login, and password reset — built entirely on packages this
project already had (hashing, encryption, the session engine, GORM)
rather than introducing a separate credential store.

## `Guard` — the authentication engine

```go
// auth/guard.go
type Guard struct { /* unexported db, hasher */ }

func NewGuard(db *gorm.DB, hasher hashing.Hasher) *Guard

func (g *Guard) Register(name, email, password string) (*models.User, error)
func (g *Guard) Attempt(email, password string) (*models.User, error)

func (g *Guard) Login(sess *gosession.Session, user *models.User)
func (g *Guard) Logout(sess *gosession.Session)
func (g *Guard) Check(sess *gosession.Session) bool
func (g *Guard) User(sess *gosession.Session) *models.User
```

`Guard` holds no per-request state — every method takes whatever
request-scoped `*gosession.Session` it needs, so one `Guard` (built once
by `AuthServiceProvider`, bound into the container as `"auth"`) is shared
safely across every concurrent request, the same shape as
[`hashing.Manager`](hashing.md) and [`logging.Manager`](logging.md).

- **`Register`** hashes the password (via the injected `hashing.Hasher`)
  and inserts the user. It returns `ErrEmailTaken` on a duplicate email —
  detected from **the database's own unique-index violation** (MySQL
  error 1062), not a preceding `SELECT`, which would leave a
  check-then-insert race between two concurrent registrations for the
  same email.
- **`Attempt`** returns the same `ErrInvalidCredentials` for both "no
  such email" and "wrong password" — deliberately indistinguishable, so
  nothing about the error itself tells an attacker which one failed.
- **`Login`/`Logout`/`Check`** store/clear/check one session key
  (`auth_user_id`) — Golite's equivalent of Laravel's own
  `login_<guard>_<hash>` session key.
- **`User`** resolves the authenticated user by re-querying the
  database from that session key. Its type-switch handles a real,
  non-obvious wrinkle: within the *same* request `Login` set the key in,
  the session still holds a native Go `uint`; on every *later* request,
  the value round-tripped through the session driver's JSON encode/decode
  (see [sessions.md](sessions.md)) and comes back as `float64` — both are
  handled.

## Wiring: `AuthServiceProvider`, routes, and the `"auth"` middleware

```go
app.Register(&providers.AppServiceProvider{})
app.Register(&providers.DatabaseServiceProvider{})
app.Register(&providers.AuthServiceProvider{}) // needs "db" + "hash", both bound above
app.Register(&providers.RouteServiceProvider{})
```

`AuthServiceProvider.Register` builds a `*auth.Guard` from the already-bound
`"db"` and `"hash"` services and binds it as `"auth"`. **Like
`DatabaseServiceProvider`, a missing dependency here is non-fatal** — if
MySQL wasn't reachable at boot, `"db"` was never bound, and `Register`
just logs a warning and leaves `"auth"` unbound too, rather than
panicking (see [database.md](database.md)).

`routes/web.go`'s `registerAuthRoutes` checks for exactly that before
registering a single auth route:

```go
func registerAuthRoutes(kernel *apphttp.Kernel) {
	guard, ok := kernel.Container().Make("auth").(*auth.Guard)
	if !ok {
		return // no database configured -- every other route still works
	}
	// ...
}
```

so a fresh clone with no MySQL running still boots and serves everything
that doesn't need a database; auth routes simply 404 (via the fallback
route) until one is configured.

### Routes

| Method | Path | Middleware | Handler |
|---|---|---|---|
| `GET` | `/csrf-token` | `session` | returns `{"csrf_token": "..."}` |
| `POST` | `/register` | `session`, `csrf` | `AuthController.Register` |
| `POST` | `/login` | `session`, `csrf` | `AuthController.Login` |
| `POST` | `/logout` | `session`, `csrf`, `auth` | `AuthController.Logout` |
| `POST` | `/forgot-password` | `session`, `csrf` | `AuthController.ForgotPassword` |
| `POST` | `/reset-password` | `session`, `csrf` | `AuthController.ResetPassword` |
| `GET` | `/me` | `session`, `auth` | `AuthController.Me` |

Every state-changing route carries `csrf` — cookie/session-based auth is
CSRF-vulnerable by nature, so (matching Laravel's default `web` middleware
group) it's protected the same way the framework's earlier CSRF demo
routes were. A client must `GET /csrf-token` first and echo it back via
`X-CSRF-TOKEN` (or the `_token` field) on every `POST` — see
[security-csrf.md](security-csrf.md).

`AuthController`'s actions are **method-injected**, exactly like
`UserController.Show` used to demonstrate (see
[controllers.md](controllers.md#method-injection--apphttpinject)):

```go
func (a *AuthController) Register(c *apphttp.Context, guard *auth.Guard) { ... }
func (a *AuthController) Login(c *apphttp.Context, guard *auth.Guard, encrypter *encryption.Encrypter) { ... }
```

wired up via `apphttp.Inject(kernel.Container(), authController.Register)`.

### Trying it with curl

```bash
JAR=cookies.txt
BASE=http://127.0.0.1:8080

TOKEN=$(curl -s -c $JAR -b $JAR $BASE/csrf-token | grep -o '"csrf_token":"[^"]*"' | cut -d'"' -f4)

curl -s -c $JAR -b $JAR -X POST $BASE/register -H "X-CSRF-TOKEN: $TOKEN" \
  --data-urlencode "name=Jane Doe" --data-urlencode "email=jane@example.com" \
  --data-urlencode "password=secret123" --data-urlencode "password_confirmation=secret123"
# -> {"user": {"id": 1, "name": "Jane Doe", "email": "jane@example.com", ...}}, and $JAR now has a valid session

curl -s -c $JAR -b $JAR $BASE/me
# -> the same user, proving Register auto-logs-in
```

This exact flow — plus login (right and wrong password), duplicate-email
rejection, logout, remember-me surviving with the session cookie
stripped entirely, and the full forgot/reset-password cycle including
single-use token consumption — was run against a real local MySQL
instance while building this feature, not just compiled.

## "Remember me" — persistent login

```go
// auth/remember.go
func (g *Guard) IssueRememberToken(user *models.User) (string, error)
func (g *Guard) UserByRememberToken(userID uint, rawToken string) (*models.User, error)
func (g *Guard) ClearRememberToken(user *models.User) error

func EncodeRememberCookie(encrypter *encryption.Encrypter, userID uint, token string) (string, error)
func DecodeRememberCookie(encrypter *encryption.Encrypter, cookieValue string) (userID uint, token string, err error)
```

When `Login` receives a truthy `remember` field (`c.Boolean("remember")`
— `"1"`/`"true"`/`"on"`/`"yes"`), `AuthController.Login` calls
`IssueRememberToken`, which generates a random 32-byte token, stores only
its SHA-256 hash on the user row (`users.remember_token`), and returns
the raw value. The controller encodes `userID|rawToken` and sets it as a
30-day, `HttpOnly` cookie named `remember_web`.

### Why this doesn't use `Context.SetCookie`

Golite already has encrypted-cookie support (`Context.SetCookie`/`Cookie`,
see [http-requests.md](http-requests.md)) — but it's built on
`Kernel.appKey`, which is **deliberately regenerated every process
restart** (see [architecture.md](architecture.md)). A remember-me
cookie's entire purpose is surviving a *browser* restart; that only
matters if it also survives a *server* restart, which `Kernel.appKey`
explicitly doesn't guarantee. `EncodeRememberCookie`/`DecodeRememberCookie`
use the **persisted** `encryption.Encrypter` (`APP_KEY`-backed — see
[encryption.md](encryption.md)) instead, via a raw `http.SetCookie` call
in the controller/middleware rather than `Context.SetCookie`.

### `AuthMiddleware` — the fallback path

```go
// app/Http/Middleware/AuthMiddleware.go
func (a *Auth) Handle(c *apphttp.Context, next func(), _ ...string) {
	sess := c.Session()
	if !a.guard.Check(sess) && !a.attemptRememberLogin(c, sess) {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	next()
}
```

If the session has no authenticated user, the `"auth"` middleware looks
for the `remember_web` cookie, decodes and validates it against the
stored hash, and — if valid — calls `guard.Login(sess, user)` to
transparently re-establish a full session before continuing, the same
fallback Laravel's own `SessionGuard` performs internally. Verified
directly: stripping every cookie except `remember_web` from a browser's
jar still reaches a protected route successfully, with no session cookie
present at all going in.

**Tokens are single-use per login, not per request.** `IssueRememberToken`
overwrites the previous hash, so logging in with `remember=1` from a
second device invalidates the first device's remember cookie. `Logout`
calls `ClearRememberToken`, so a stolen-but-unused cookie stops working
immediately rather than merely at its 30-day expiry.

## Password reset

```go
// auth/password_reset.go
type PasswordResetToken struct {
	Email     string
	TokenHash string
	CreatedAt time.Time
}

func (g *Guard) CreatePasswordResetToken(email string) (string, error)
func (g *Guard) ResetPassword(email, token, newPassword string) error
```

`PasswordResetToken` lives in the `auth` package, not `app/Models` —
unlike `User`, application code never queries it directly; it's purely
`Guard`'s own bookkeeping. `CreatePasswordResetToken` replaces any
previous token for that email and stores only its hash (same
`newRandomToken`/hash-comparison shape as remember-me tokens — see
`auth/token.go`). `ResetPassword` checks the token's hash **and** that
it's within `passwordResetTokenTTL` (1 hour, Laravel's own default),
updates the password, and deletes the token — so a used or expired token
can never succeed twice.

> **There is no mail system in Golite yet.** `AuthController.ForgotPassword`
> returns the raw token directly in the JSON response, standing in for
> "emailed to the user." This is fine for local development but means
> the endpoint necessarily reveals whether an email is registered (a
> `404` vs. a token) — a real deployment needs actual email delivery
> wired up, and the controller changed to return an identical response
> either way, before this is safe to expose publicly.

## The `User` model and its migrations

```go
// app/Models/User.go
type User struct {
	Model
	Name          string
	Email         string `gorm:"uniqueIndex"`
	Password      string `json:"-"`
	RememberToken string `json:"-"` // SHA-256 hash, never the raw token
}
```

Two migrations back this feature (see [database.md](database.md) for how
the migration system itself works):
`2026_07_13_000001_create_users_table.go` (from `User`'s own struct tags)
and `2026_07_13_000002_create_password_reset_tokens_table.go` (from
`auth.PasswordResetToken`'s).
