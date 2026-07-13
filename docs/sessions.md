# Sessions

Files: [`app/Http/Session/`](../app/Http/Session/) (`SessionHandler.go`,
`SessionManager.go`, `Session.go`, `MemorySessionHandler.go`,
`FileSessionHandler.go`, `CookieSessionHandler.go`, `Lock.go`, `crypto.go`),
[`app/Http/Middleware/StartSessionMiddleware.go`](../app/Http/Middleware/StartSessionMiddleware.go),
[`app/Http/SessionBlock.go`](../app/Http/SessionBlock.go),
[`app/Http/Context.go`](../app/Http/Context.go)

Golite's session engine mirrors Laravel's in full: a driver-based
`SessionHandler` interface (PHP's `SessionHandlerInterface`, adapted to Go),
a `Manager` that loads/saves sessions through whichever driver is active and
lets you register your own, a `StartSessionMiddleware` that attaches a
session to every request, an expressive `Session` API
(`Get`/`Put`/`Push`/`Pull`/`Increment`/`Regenerate`/...), one-shot flash
data, and `.Block()` for serializing concurrent requests that share a
session.

## The `Handler` interface and built-in drivers

```go
// app/Http/Session/SessionHandler.go
type Handler interface {
	Read(id string) (string, error)
	Write(id string, data string) error
	Destroy(id string) error
	Gc(lifetime int)
}
```

This is Golite's equivalent of PHP's `SessionHandlerInterface`, adapted to
Go idioms: a session's entire state round-trips as one JSON string payload
rather than PHP's serialized scalar. Three drivers ship built in, registered
by `NewManager`:

| Driver | Storage | Use case |
|---|---|---|
| `"memory"` (default) | A concurrent-safe `map[string]memoryRecord` | Local development and testing — nothing survives a restart |
| `"file"` | One JSON file per session under `storage/sessions/` | The recommended default for a real single-process deployment |
| `"cookie"` | The encrypted, signed cookie itself — no server-side storage at all | Stateless deployments; see the [caveat](#the-stateless-cookie-driver-a-real-limitation) below before using it for anything that both mutates the session and responds in the same request |

```go
manager := gosession.NewManager("golite_session", 7200, appKey) // 7200s = 120min, Laravel's default
manager.Driver("file") // switch the active driver
```

`FileSessionHandler` validates every session ID against a strict
base64url-only pattern (`isValidSessionID`, in `crypto.go`) *before* using it
to build a filesystem path — defense in depth against path traversal, on top
of `Manager.Load` already refusing to trust a client-supplied ID that isn't
already a well-formed ID.

### Custom drivers — `Manager.Extend`

```go
func (m *Manager) Extend(name string, factory func() Handler)
```

Register any backend satisfying `Handler` — Redis, a database, DynamoDB —
typically from a service provider's `Register`/`Boot`:

```go
// e.g. in a service provider's Boot method
kernel.Sessions().Extend("redis", func() session.Handler {
	return NewRedisSessionHandler(redisClient)
})
kernel.Sessions().Driver("redis")
```

## Attaching a session to every request — `StartSessionMiddleware`

```go
kernel.AliasMiddleware("session", middleware.NewStartSession(kernel.Sessions()))
```

`NewKernel` already seeds the `"web"` middleware group with the name
`"session"`, ahead of `"csrf"` (CSRF depends on a session being present) —
`routes/web.go` only has to alias it to a real instance:

```go
MiddlewareGroups: map[string][]string{
	"web": {"session", "csrf"},
},
```

Any route that calls `c.Session()`, `c.CsrfToken()`, `c.Flash()`, `c.Old()`,
or a redirect's `.With()`/`.WithInput()` needs `"session"` (directly or via
a group that includes it) attached — `c.Session()` panics with a clear
message otherwise, rather than silently creating an ad hoc session.

### A Go-specific cookie-ordering fix (the same class of bug CSRF hit)

This is the part worth understanding before touching this code. The first
version of `StartSessionMiddleware.Handle` loaded the session, ran the rest
of the chain, and *then* wrote the `Set-Cookie` header — mirroring how
`Manager.Save` naturally wants to be called (after the handler has finished
mutating the session). Live end-to-end testing showed sessions never
actually persisted: the cookie never reached the browser at all.

The cause: Go's `http.ResponseWriter` streams headers immediately once
anything downstream calls `WriteHeader` — which is exactly what any handler
serving a real response does (`c.JSON`, a `Responder`-wrapped return value,
`Response.Send`, `http.Redirect`, ...). By the time `next()` returns control
to `StartSessionMiddleware`, the response has already been finalized, so a
`Set-Cookie` added at that point is silently dropped. This is the same
constraint documented for [`VerifyCsrfToken`](security-csrf.md#the-xsrf-token-cookie-and-a-go-specific-ordering-fix)
and [redirect flash data](responses.md#redirects-with-flash-data) — but this
time it wasn't optional to work around, since a session that never sends its
cookie is a session that never persists.

The fix, in `StartSession.Handle`:

- For every driver except the stateless `"cookie"` one, the session cookie
  is queued **before** `next()` runs, using the session's ID at load time.
  This is safe because an ID-based driver's cookie value doesn't depend on
  anything the handler does — it's stable unless `Regenerate`/`Invalidate`
  is called mid-request.
- The session's **data** is still saved after `next()` (it has to be — that's
  the only point the handler's mutations are visible), just not the cookie
  itself for the common case.
- `Regenerate()`/`Invalidate()` need the client to see the *new* ID in the
  same response, which an early-queued cookie can't reflect on its own — see
  [below](#regenerate-and-invalidate-need-a-context-level-wrapper).

### The stateless `"cookie"` driver: a real limitation

The `"cookie"` driver can't use the early-write trick above: its cookie
value *is* the encoded session payload, which isn't known until the handler
has finished mutating the session, so it's necessarily computed after
`next()` — which means it's subject to the exact silently-dropped-header
problem the early-write fix exists to avoid. In practice, this makes the
`"cookie"` driver reliable only for routes that don't themselves write a
response after mutating the session, which is true of very few real routes.
**Prefer `"memory"` or `"file"` for any route that both mutates the session
and responds in the same request.**

## The `Session` API

Attached via `c.Session()`, which panics if `StartSessionMiddleware` isn't
active on the matched route:

```go
sess := c.Session()

sess.Get("cart")                          // nil if absent
sess.Get("cart", []any{})                 // with a default value
sess.Get("cart", func() any { return computeDefault() }) // ... or a default resolver

sess.Put("last_ip", c.Ip())
sess.Push("cart", "item-123")             // append to a slice value, creating it if absent
removed := sess.Pull("cart")              // get + delete in one step

sess.All()                                // map[string]any copy of everything
sess.Has("cart")                          // present AND not nil
sess.Exists("cart")                       // present at all, even if nil
sess.Missing("cart")                      // !Exists, not !Has — matches Laravel exactly

sess.Increment("visits")                  // +1, returns the new value
sess.Decrement("visits", 5)               // -5

sess.Forget("cart", "last_ip")
sess.Flush()                              // every value, including flash state

sess.ID()
sess.Token()                              // CSRF token, generated + persisted on first call
```

`Session` guards its own map with a mutex — it's decoded fresh from the
active driver at the start of every request (`Manager.Load`), so that
mutex only protects concurrent access *within* one request (a goroutine
spawned by a handler, say), not across requests for the same ID sharing
state. That cross-request case — two concurrent requests loading their own
copy, mutating independently, whichever saves last silently winning — is
what [`.Block()`](#session-blocking-block) exists to prevent.

## Flash data

```go
sess.Flash("notice", "Post created!") // readable via Get on this request's session AND the next one
sess.Now("shown_now", "just this request") // readable only during this cycle
sess.Reflash()                             // extend every currently-visible flash key one more cycle
sess.Keep("notice")                        // extend just this one
```

Internally this tracks two key-name sets, `oldFlash` (visible this cycle)
and `newFlash` (flashed this cycle, visible next), not a prefix convention —
matching Laravel's actual internals rather than a simpler-but-different
scheme. `ageFlash` rotates them exactly once per request, from
`Manager.Load`: whatever was `old` is discarded, whatever was `new` is
promoted to `old`. So a value flashed on request *N* is invisible on *N*
itself, readable on *N+1*, and gone by *N+2* — verified directly against a
live server across three sequential requests sharing a cookie jar.

`Context.Flash()`/`Context.Old(key)` are a thin, form-specific layer on top
of this same mechanism (see [http-requests.md](http-requests.md#flash-data-and-old-input)),
and `Response.With`/`WithInput` go through it too (see
[responses.md](responses.md#redirects-with-flash-data)) — a message flashed
via `.With("notice", ...)` and one flashed via `sess.Flash("notice", ...)`
directly share the same key, exactly like Laravel.

## Regenerate and Invalidate need a Context-level wrapper

```go
func (s *Session) Regenerate() string // keep the data, assign a fresh ID — call after login
func (s *Session) Invalidate()        // fresh ID AND every value discarded — call on logout
```

Calling these directly on `c.Session()` updates the in-memory `Session`
object correctly, but — because of the [cookie-ordering fix](#a-go-specific-cookie-ordering-fix-the-same-class-of-bug-csrf-hit)
above — the cookie `StartSessionMiddleware` already queued *before* the
handler ran still carries the *old* ID, and there's no later point in the
pipeline where updating it would actually reach the client (the handler has
almost always already written its own response by the time
`StartSessionMiddleware`'s post-`next()` code resumes). Worse than a
cosmetic mismatch: since `Manager.Save` destroys the old ID's record once
the ID changes, a client left holding the stale cookie would find nothing
at all on its next request — an effective, silent loss of the session.

`Context` provides wrappers that update the already-queued cookie in place,
which only works because they run synchronously *inside* the handler,
before it writes anything of its own:

```go
func (c *Context) RegenerateSession() string
func (c *Context) InvalidateSession()
```

Call these — not `c.Session().Regenerate()`/`.Invalidate()` directly —
whenever the client needs to see the effect in the same response:

```go
kernel.POST("/session/regenerate", apphttp.Responder(func(c *apphttp.Context) any {
	return map[string]string{"new_session_id": c.RegenerateSession()}
})).Middleware("session")

kernel.POST("/logout", apphttp.Responder(func(c *apphttp.Context) any {
	c.InvalidateSession()
	return map[string]string{"status": "logged out"}
})).Middleware("session")
```

Verified directly: cookie jar shows the session ID change immediately after
`POST /session/regenerate`, session data (`visits`) survives the swap, and
`POST /logout` both rotates the ID and resets all data.

## Session blocking (`.Block()`)

```go
func (r *RouteDefinition) Block(lockSeconds ...int) *RouteDefinition
```

```go
kernel.POST("/cart/add", handler).Middleware("session").Block(5).Name("cart.add")
```

Acquires an exclusive, per-session-ID lock (`Manager.Lock`, `sync.Mutex`
polled via `TryLock` rather than a goroutine+channel+timeout, which would
otherwise leak a goroutine on timeout) before the rest of the chain runs —
serializing two AJAX requests fired close together from the same browser
tab, so one can't silently overwrite the other's session changes. `Manager`
never removes a lock once created (`lockRegistry`); this is a deliberate,
bounded tradeoff — one `*sync.Mutex` per session ID for the process's
lifetime, acceptable at the scale a lightweight framework targets.

### Why blocking has to reload the session, not just serialize the save

The first implementation only wrapped the *save* in the lock: acquire, run
`next()` (mutating whatever `Session` object `StartSessionMiddleware` had
already loaded, before any lock was available), save, release. Load-testing
with concurrent requests exposed the gap immediately — firing 10 concurrent
`POST /cart/add` requests sharing one session dropped 6 of them. The reason:
each request's `Session` object was loaded independently and concurrently,
*before* the lock existed to protect anything, so each one mutated its own
stale snapshot; the lock only serialized the *overwrites*, not the reads
that made them stale in the first place — the classic lost-update problem,
just with the write half serialized and the read half not.

The fix, in `sessionBlockMiddleware.Handle`: once the lock is actually held,
**reload** the session from the driver (`manager.Load(id)`) and re-attach it
to the `Context` before calling `next()`, so this request starts from the
freshest possible state rather than whatever was loaded before the lock was
available. Re-verified with 20 concurrent requests: all 20 items landed in
the cart.

### Why the save happens in `.Block()`'s own middleware, not `StartSessionMiddleware`'s

Golite nests route-level middleware (`.Block()`'s) *inside* whatever
middleware group wraps it (`StartSessionMiddleware`, registered into the
`"web"` group). `StartSessionMiddleware`'s own "after" phase — where it
would normally save the session — only runs once everything nested inside
it, including `.Block()`'s own "after" phase, has already returned. If the
lock were released in `.Block()` and the save happened afterward in
`StartSessionMiddleware`, the actual write would sit *outside* the critical
section, defeating the point of blocking entirely. So `.Block()`'s
middleware saves the session itself, while still holding the lock, and sets
a Context flag (`apphttp.SessionSavedKey`) telling `StartSessionMiddleware`
to skip its own redundant save.

### What `.Block()` doesn't cover

- A brand-new session (no incoming cookie) skips the lock and reload
  entirely — there's nothing yet persisted under any ID for a concurrent
  request to race against.
- It protects a single session's data against concurrent requests *to
  routes that also use `.Block()`*. A `.Block()`-guarded route racing
  against a non-blocked route mutating the same session is not protected —
  add `.Block()` to every route that can concurrently mutate a shared
  session, not just one of them.

## The demo in `routes/web.go`

A "Sessions" section demonstrates the full API against a live server, all
under `.Middleware("session")`:

- `GET /session/visit` — `Put`/`Get`/`Increment` (a visit counter + last IP).
- `GET /session/all` — `All`/`Has`/`Exists`/`Missing`.
- `POST /cart/add` — `Push`, guarded by `.Block(5)`.
- `DELETE /cart` — `Pull` + `Forget`.
- `GET /session/flash-demo` — `Flash` vs `Now`.
- `GET /session/reflash` — `Reflash`.
- `POST /session/regenerate` / `POST /logout` — `Context.RegenerateSession`/
  `InvalidateSession`.

Verified end to end with a cookie jar: visits increment across requests,
`.Block()`-guarded concurrent cart adds all land, flash data is readable
exactly one request later and gone by the one after that, and
regenerate/invalidate both update the cookie in the same response.

## Known simplifications vs. Laravel

- **No CSRF token rotation coupling.** Laravel's own login scaffolding
  calls `regenerate()` as part of authentication; Golite has no built-in
  auth system yet to wire that into automatically — `RegenerateSession`
  is available for a hand-rolled login handler to call, but nothing calls
  it for you.
- **The stateless `"cookie"` driver's limitation is structural, not a
  missing feature** — see [above](#the-stateless-cookie-driver-a-real-limitation).
  Fixing it properly would mean buffering the entire response body until
  the session is finalized, which has real costs (defeats
  `StreamDownload`'s no-temp-file streaming, adds memory overhead to every
  response) that aren't worth paying for a driver `"file"` already serves
  better in the common case.
- **`lockRegistry` never removes an entry.** Bounded, and deliberate — see
  [above](#session-blocking-block).
- See [security-csrf.md](security-csrf.md) for how CSRF verification builds
  on `c.Session().Token()`, and [http-requests.md](http-requests.md#flash-data-and-old-input)
  / [responses.md](responses.md#redirects-with-flash-data) for the
  form-repopulation and redirect-flash layers built on top of `Flash`/`Now`.
