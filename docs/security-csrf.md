# CSRF Protection

Files: [`app/Http/Session.go`](../app/Http/Session.go),
[`app/Http/Context.go`](../app/Http/Context.go),
[`app/Http/Middleware/VerifyCsrfToken.go`](../app/Http/Middleware/VerifyCsrfToken.go),
[`app/Http/Kernel.go`](../app/Http/Kernel.go)

Golite implements Laravel's CSRF architecture end to end: a session-bound
token generated with `crypto/rand`, a `VerifyCsrfToken` middleware that
checks it on state-changing requests, wildcard path exclusions, and an
`XSRF-TOKEN` cookie kept in sync for JS clients (Axios, Angular) — the
"double submit cookie" pattern.

## Why CSRF protection needs a session

A CSRF token only works if it's tied to something that persists across
requests from the same browser — issue a fresh token on every request and
there's nothing for a subsequent `POST` to match against. Laravel's token
lives on `$request->session()->token()`; Golite needed the same foundation,
so this feature also introduces Golite's first session mechanism.

### `Session` and `SessionStore`

```go
type Session struct {
	ID string
	// ...
}

func (s *Session) Get(key string) string
func (s *Session) Put(key, value string)
func (s *Session) Token() string // generates + persists a token on first call

type SessionStore struct { /* ... */ }

func NewSessionStore() *SessionStore
```

`SessionStore` is a thread-safe, **in-memory** map of session ID →
`*Session` — Golite's minimal equivalent of Laravel's session driver
abstraction (`file`/`database`/`redis`/...). `Session.Token()` generates a
32-byte `crypto/rand` value, base64url-encodes it, and stores it once per
session (a second call returns the same token; a losing side of a
first-request race keeps whichever token was stored first, so concurrent
requests for a brand-new session always agree).

> **Limitation:** sessions live only in process memory and are lost on
> restart, and there's no expiry/garbage collection. Fine for a single
> lightweight-framework process; a real deployment needing persistence or
> multi-instance sharing would swap `SessionStore` for one backed by
> Redis/a database, without changing `Context.Session()`'s API.

### `Context.Session()` and `Context.CsrfToken()`

```go
func (c *Context) Session() *Session
func (c *Context) CsrfToken() string
```

`Context.Session()` reads the `golite_session` cookie, looks the ID up in
the kernel's `SessionStore`, and **creates a new session** (queuing a
`Set-Cookie` for it) if the cookie is missing or unknown. The result is
cached on the `Context`, so repeated calls within one request are cheap and
consistent. `CsrfToken()` is just `Session().Token()` — use it to render a
hidden field or meta tag:

```go
kernel.GET("/comments", func(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
	// in an HTML-rendering handler, this would instead be something like:
	//   <input type="hidden" name="_token" value="{{ c.CsrfToken() }}">
	//   <meta name="csrf-token" content="{{ c.CsrfToken() }}">
})
```

The session cookie itself is `HttpOnly`, `SameSite=Lax`, and `Secure` when
the request is (or is reported by a trusted proxy to be) HTTPS — see
`IsSecureRequest` in `Session.go`.

## `VerifyCsrfToken`

```go
type VerifyCsrfToken struct {
	Except []string
}

func NewVerifyCsrfToken(except ...string) *VerifyCsrfToken
```

Registered like any other [middleware](middleware.md):

```go
kernel.AliasMiddleware("csrf", middleware.NewVerifyCsrfToken("/stripe/*", "/api/v1/webhooks"))

kernel.GET("/comments", handler).Middleware("csrf")
kernel.POST("/comments", handler).Middleware("csrf")
```

`Handle` does, in order:

1. **Safe methods pass through untouched.** `GET`, `HEAD`, `OPTIONS` never
   require a token (`csrfSafeMethods`).
2. **Excluded paths pass through untouched** — see below.
3. **Otherwise, the submitted token must match the session's token**, read
   from, in order:
   1. the `_token` form field (`r.PostFormValue("_token")`),
   2. the `X-CSRF-TOKEN` header,
   3. the `X-XSRF-TOKEN` header — what Axios/Angular send automatically,
      populated client-side from the `XSRF-TOKEN` cookie this same
      middleware sets.
4. **A missing or mismatched token responds `419`** (`StatusPageExpired` —
   not a registered IANA status, so Golite defines the constant itself,
   same as Laravel) with a JSON body, and the request goes no further.
5. **A match (or an exemption) syncs the `XSRF-TOKEN` cookie and calls
   `next()`.**

Token comparison uses `crypto/subtle.ConstantTimeCompare`, so response
timing can't leak how much of a guessed token was correct.

### Wildcard exclusions (`Except`)

```go
func matchesExceptPattern(pattern, requestPath string) bool {
	if pattern == requestPath {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(requestPath, prefix)
	}
	return false
}
```

A pattern ending in `*` matches any path with that prefix (`"/stripe/*"`
matches `/stripe/webhook`, `/stripe/webhook/retry`, ...); anything else
must match the path exactly. Typical uses are third-party webhooks that
can't supply a session-bound token — they're still state-changing
`POST`s, just from a server that was never handed a CSRF token in the
first place.

### The `XSRF-TOKEN` cookie, and a Go-specific ordering fix

```go
func (m *VerifyCsrfToken) syncCookie(c *apphttp.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "XSRF-TOKEN",
		Value:    c.Session().Token(),
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   apphttp.IsSecureRequest(c.Request),
	})
}
```

`HttpOnly: false` is **deliberate**, not an oversight: client-side JS has
to be able to read this cookie in order to echo it back as `X-XSRF-TOKEN`.
It carries no session identity on its own — it's only meaningful paired
with the real (`HttpOnly`) session cookie — so a leaked `XSRF-TOKEN` value
alone doesn't let an attacker impersonate the session.

`Handle` calls `syncCookie` **before** `next()`, not after:

```go
if csrfSafeMethods[c.Request.Method] || m.isExcluded(c.Request.URL.Path) || m.tokensMatch(c) {
	m.syncCookie(c)
	next()
	return
}
```

This was caught by testing: Laravel sets the cookie *after* `$next($request)`
returns, via `tap()`, because PHP's `Response` is a mutable object that
isn't actually sent to the client until the framework explicitly flushes
it — headers can still be added right up until then. Go's
`http.ResponseWriter` has no such buffering: the moment a downstream
handler calls `WriteHeader` (which `Context.JSON` does), every header set
afterward — including a `Set-Cookie` added post-`next()` — is silently
dropped. The first version of this middleware followed Laravel's ordering
exactly and the cookie never actually reached the browser. Setting it
before `next()` instead is safe here because the token value only depends
on the session, never on anything the downstream handler does.

## Wiring: the `"web"` middleware group

```go
// Kernel.go, inside NewKernel:
MiddlewareGroups: map[string][]string{
	"web": {"csrf"},
},
```

`NewKernel` seeds the `"web"` group with the *name* `"csrf"` by default,
mirroring Laravel's `Kernel.php`, which always includes
`VerifyCsrfToken::class` in `$middlewareGroups['web']`. `app/Http/Kernel.go`
can't reference the concrete `VerifyCsrfToken` type directly — `Middleware`
already imports `app/Http`, so the reverse import would be a cycle — so
the kernel only pre-registers the *name*; the name does nothing until
something aliases it to a real implementation, which
[`routes/web.go`](../routes/web.go) does:

```go
kernel.AliasMiddleware("csrf", middleware.NewVerifyCsrfToken("/stripe/*", "/api/v1/webhooks"))
```

`kernel.MiddlewarePriority` also puts `"csrf"` first, ahead of `"auth"` /
`"role"` / `"audit"`, so it always runs before anything that might depend
on session state, regardless of the order middleware is assigned on a
route or pulled in via a group.

## The demo in `routes/web.go`

```go
kernel.GET("/comments", func(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
}).Middleware("csrf").Name("comments.form")

kernel.POST("/comments", resourceHandler("comments.store")).Middleware("csrf").Name("comments.store")

kernel.POST("/stripe/webhook", resourceHandler("stripe.webhook")).Middleware("csrf").Name("stripe.webhook")
```

The intended flow, verified end to end with a cookie jar:

1. `GET /comments` — no session cookie yet, so one is created; the response
   carries `Set-Cookie: golite_session=...` (HttpOnly) and
   `Set-Cookie: XSRF-TOKEN=...` (not HttpOnly), and the body echoes the
   same token as JSON.
2. `POST /comments` with no token → **419**.
3. `POST /comments` with the token as `_token`, `X-CSRF-TOKEN`, or
   `X-XSRF-TOKEN` (using the exact value read back from the `XSRF-TOKEN`
   cookie, the real Axios flow) → **200**.
4. `POST /comments` with a wrong token, or no session cookie at all →
   **419**.
5. `POST /stripe/webhook` with no token → **200** — exempted via
   `Except`, even though the `"csrf"` middleware is attached to the route.

## Known simplifications vs. Laravel

- **The `XSRF-TOKEN` cookie is the plaintext token.** Laravel encrypts its
  cookie (via the framework's `Encrypter`) and decrypts the
  `X-XSRF-TOKEN` header before comparing, so the cookie's contents aren't
  directly readable even by something that can intercept it without also
  having the app key. Golite has no encryption service yet, so the token
  travels as-is — sufficient for the CSRF threat model itself (the
  attacker's problem is *sending* the cookie value cross-site, not reading
  it), but worth knowing if you're comparing feature-for-feature against
  Laravel.
- **No CSRF token rotation on login/logout.** Laravel regenerates the
  session (and therefore the token) on authentication changes to prevent
  session fixation; Golite has no auth/session-lifecycle integration yet,
  so a session's token is stable for its entire (in-memory,
  process-lifetime) existence.
- See [routing.md](routing.md) and [middleware.md](middleware.md) for how
  `Except`'s wildcard matching and the `"csrf"` alias/group wiring relate
  to the rest of the routing and middleware systems.
