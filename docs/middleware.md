# Middleware

Files: [`app/Http/Kernel.go`](../app/Http/Kernel.go),
[`app/Http/Context.go`](../app/Http/Context.go),
[`app/Http/Middleware/LoggerMiddleware.go`](../app/Http/Middleware/LoggerMiddleware.go),
[`app/Http/Middleware/MethodSpoofingMiddleware.go`](../app/Http/Middleware/MethodSpoofingMiddleware.go),
[`app/Http/Middleware/RoleMiddleware.go`](../app/Http/Middleware/RoleMiddleware.go),
[`app/Http/Middleware/AuditMiddleware.go`](../app/Http/Middleware/AuditMiddleware.go),
[`app/Http/Middleware/VerifyCsrfToken.go`](../app/Http/Middleware/VerifyCsrfToken.go)

Golite's middleware system mirrors Laravel's in full: three distinct
registries (global, named/aliased, grouped), middleware parameters
(`"role:editor,admin"`), a priority order that's enforced regardless of
assignment order, per-route exclusion (`WithoutMiddleware`), terminable
middleware, and dependency-injected middleware structs resolved through the
service container.

## The `Middleware` contract

```go
type Middleware interface {
	Handle(c *Context, next func(), params ...string)
}
```

This mirrors Laravel's `$middleware->handle($request, $next, ...$params)`
directly: a middleware receives the request `Context`, a `next` callback
that continues the pipeline, and any parameters parsed from a
`"name:param1,param2"` middleware spec. A middleware that wants to
short-circuit the request (failed auth, a forbidden role, ...) simply never
calls `next`.

Being an **interface**, not a bare function type, is what lets middleware
be implemented as a **struct** — which is what makes parameters,
termination, and dependency injection possible (a closure can't have a
second method or hold injected fields). For simple, stateless middleware
that needs neither, `MiddlewareFunc` adapts a plain function — Golite's
equivalent of the standard library's `http.HandlerFunc`:

```go
type MiddlewareFunc func(c *Context, next func())

func (f MiddlewareFunc) Handle(c *Context, next func(), _ ...string) {
	f(c, next)
}
```

`HandlerFunc` (`func(*Context)`) is unrelated and unchanged: it's the shape
of a **controller action** — the terminal handler at the end of the chain,
which has no `next` to call.

## How the chain runs — `Context.Next()`

```go
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}
```

This is the same recursive "onion" pipeline as before: `Next()` advances
the index by one and, if there's a `HandlerFunc` there, calls it directly.
Middleware never appears in `c.handlers` directly, though — each resolved
`Middleware` is wrapped into a `HandlerFunc` by `toHandler`:

```go
func toHandler(mw Middleware, params []string) HandlerFunc {
	return func(c *Context) {
		c.executed = append(c.executed, mw)
		mw.Handle(c, c.Next, params...)
	}
}
```

Passing `c.Next` (a method value, type `func()`) as the `next` argument is
what lets a `Middleware.Handle` implementation call `next()` to continue
the pipeline exactly like `Context.Next()` always worked — the two are the
same mechanism, just addressed through the new interface. `toHandler` also
records the middleware into `c.executed`, which is how `Kernel.terminate`
later finds every middleware that actually ran for this request (see
[Terminable middleware](#terminable-middleware) below).

**Not calling `next()` genuinely stops the chain** — nothing further down
ever runs, no `Abort()` call needed. This was specifically verified: an
earlier iterative implementation of `Next()` kept advancing to the next
handler regardless of whether the current one called `Next()`, which meant
an "unauthorized" middleware that just returned still let the controller
run afterward, double-writing the response. The recursive form doesn't
have that failure mode.

## The three registries

Mirroring Laravel's `app/Http/Kernel.php` almost field-for-field:

```go
type Kernel struct {
	// GlobalMiddleware runs on every request, in registration order, before
	// routing is resolved — Laravel's $middleware.
	GlobalMiddleware []Middleware

	// RouteMiddleware maps a short alias (e.g. "auth") to the middleware
	// that implements it — Laravel's $routeMiddleware.
	RouteMiddleware map[string]Middleware

	// MiddlewareGroups maps a group name (e.g. "web") to an ordered list of
	// other middleware names — Laravel's $middlewareGroups.
	MiddlewareGroups map[string][]string

	// MiddlewarePriority defines the order non-global middleware run in,
	// regardless of assignment order — Laravel's $middlewarePriority.
	MiddlewarePriority []string
	// ...
}
```

These are exported fields (so they can be configured declaratively, as in
Laravel's `Kernel.php`), but should be treated as boot-time configuration —
set them via the constructor/helper methods below (which take a write lock)
before the server starts serving; reads during dispatch always go through a
lock-protected snapshot.

### Global middleware

```go
func (k *Kernel) UseMiddleware(middleware ...Middleware)
```

```go
app.Kernel.UseMiddleware(appMiddleware.MethodSpoofing(), appMiddleware.Logger())
```

Runs on **every** request — matched route, 405, fallback, or plain 404 —
because `Kernel.ServeHTTP` always builds the initial chain as
`[...GlobalMiddleware, k.dispatch]`, and `dispatch` is what actually
resolves routing. Global middleware order is never touched by
`MiddlewarePriority` — only route-resolved middleware gets sorted (see
below).

### Route middleware (aliases)

```go
func (k *Kernel) AliasMiddleware(name string, mw Middleware)
```

```go
kernel.AliasMiddleware("auth", apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
	if c.Request.Header.Get("Authorization") == "" {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	next()
}))

kernel.GET("/account", handler).Middleware("auth")
```

`RouteDefinition.Middleware(names ...any)` and `RouteGroup.Middleware(names
...any)` accept a single name (`"auth"`), several (`"auth",
"role:editor,admin"`), or one `[]string{"web", "auth"}` — `flattenMiddlewareNames`
normalizes whichever form is used. They just record the raw spec strings;
resolution happens per-request in `Kernel.dispatch`, via
`Kernel.resolveRouteMiddleware`.

### Middleware groups

```go
func (k *Kernel) MiddlewareGroup(name string, members ...any)
```

```go
kernel.MiddlewareGroup("web", "auth", "audit")

kernel.Middleware("web").Group(func(g *apphttp.RouteGroup) {
	g.GET("/dashboard", handler) // expands to auth, then audit
})
```

Referencing a group name in `.Middleware(...)` expands to its members —
recursively, if a member is itself a group name, with cycle protection —
via `Kernel.expandMiddlewareNames`.

### Dependency injection through the service container

```go
func (k *Kernel) Container() *container.Container
```

`Kernel.lookupMiddleware` checks `RouteMiddleware` first; if the name isn't
there, it falls back to `k.Container().Make(name)`, type-asserted to
`Middleware`. That means a middleware struct — built with whatever
constructor-injected dependencies it needs — can be registered **directly
through the container** instead of (or in addition to) `AliasMiddleware`:

```go
kernel.Container().Bind("audit", middleware.NewAudit())

kernel.GET("/admin/users", handler).Middleware("audit") // resolved from the container
```

This is the same container every controller already resolves services
from via `Context.App` — see [service-container.md](service-container.md).

## Middleware parameters

```go
func ParseMiddlewareSpec(spec string) (name string, params []string)
```

A middleware string is split on the *first* `:` into a base name and a
comma-separated parameter list — `"role:editor,admin"` → name `"role"`,
params `["editor", "admin"]`. `Kernel.resolveRouteMiddleware` parses every
spec this way before resolving the base name and calling
`mw.Handle(c, next, params...)`.

```go
// app/Http/Middleware/RoleMiddleware.go
type Role struct{}

func NewRole() *Role { return &Role{} }

func (m *Role) Handle(c *apphttp.Context, next func(), params ...string) {
	userRole := c.Request.Header.Get("X-User-Role")
	for _, allowed := range params {
		if strings.EqualFold(strings.TrimSpace(allowed), userRole) {
			next()
			return
		}
	}
	c.JSON(http.StatusForbidden, map[string]string{
		"error": "forbidden: requires role " + strings.Join(params, " or "),
	})
}
```

```go
kernel.GET("/posts/{post}/edit", handler).Middleware("auth", "role:editor,admin")
```

## Excluding middleware — `WithoutMiddleware`

```go
func (r *RouteDefinition) WithoutMiddleware(names ...any) *RouteDefinition
```

```go
kernel.Prefix("admin").Middleware("web").Group(func(admin *apphttp.RouteGroup) {
	// "web" pulls in auth + audit for every route in this group...
	admin.GET("/health", handler).WithoutMiddleware("audit") // ...except this one
})
```

Exclusion is by **base name**, checked after group expansion but before
resolution: `Kernel.resolveRouteMiddleware` expands the route's (and its
group's) middleware specs through `MiddlewareGroups` first, *then* drops
any whose base name is in the route's `WithoutMiddleware` set — so it
correctly reaches into and removes a specific group-contributed middleware,
not just ones added directly on the route.

## Middleware priority

```go
MiddlewarePriority []string
```

```go
kernel.MiddlewarePriority = []string{"auth", "role", "audit"}
```

After expansion and exclusion, `Kernel.resolveRouteMiddleware` stable-sorts
the remaining middleware by this list — entries whose base name appears in
`MiddlewarePriority` are ordered by that list's index; anything not listed
runs after every listed entry, in its original relative order. This is
**independent of how the middleware was assigned** — written directly on
the route, pulled in via a group, in any order — which was verified
directly: registering `"third"`, `"second"`, `"first"` as aliases (in that
order) and assigning them on a route as `.Middleware("third", "first",
"second")`, with `MiddlewarePriority = []string{"first", "second",
"third"}`, still executes `first → second → third → handler`.

Only **route-resolved** middleware is sorted this way; `GlobalMiddleware`
always runs in registration order (see above).

## Terminable middleware

```go
type TerminableMiddleware interface {
	Terminate(c *Context)
}
```

```go
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ...build chain, run it...
	ctx.Next()

	k.terminate(ctx)
}

func (k *Kernel) terminate(c *Context) {
	for _, mw := range c.executed {
		if t, ok := mw.(TerminableMiddleware); ok {
			t.Terminate(c)
		}
	}
}
```

Once the entire middleware/handler chain has returned — meaning every
write to the response has already happened — the kernel loops over
`c.executed` (populated by `toHandler` as each middleware actually started
running; a middleware short-circuited *before* it runs never appears here)
and calls `Terminate` on any that implement it, in the order they executed.
This mirrors Laravel's `Kernel::terminate()`, called right after
`$response->send()` in `public/index.php`.

**A middleware instance is shared across every concurrent request** (one
`*Audit` handles all traffic), so it must never store per-request state on
itself — that's a data race. `Context.Set`/`Context.Get` exist specifically
to pass state from `Handle` to `Terminate` through the (per-request,
goroutine-safe) `Context` instead:

```go
// app/Http/Middleware/AuditMiddleware.go
type Audit struct{}

func NewAudit() *Audit { return &Audit{} }

func (m *Audit) Handle(c *apphttp.Context, next func(), _ ...string) {
	c.Set("audit.start", time.Now())
	next()
}

func (m *Audit) Terminate(c *apphttp.Context) {
	start, ok := c.Get("audit.start")
	if !ok {
		return
	}
	log.Printf("[audit] %s %s finished in %s", c.Request.Method, c.Request.URL.Path, time.Since(start.(time.Time)))
}
```

## `LoggerMiddleware` and `MethodSpoofingMiddleware`

Both are global, stateless middleware adapted via `MiddlewareFunc`:

```go
// "after" style: runs code both before and after next()
func Logger() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		start := time.Now()
		method, path := c.Request.Method, c.Request.URL.Path
		next()
		log.Printf("%s %s completed in %s", method, path, time.Since(start))
	})
}

// "before" style: all its work happens ahead of next(), nothing after
func MethodSpoofing() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		if c.Request.Method == http.MethodPost {
			if override := resolveOverride(c.Request); spoofableMethods[override] {
				c.Request.Method = override
			}
		}
		next()
	})
}
```

`MethodSpoofing` must run as **global** middleware, and before routing —
which registering it via `Kernel.UseMiddleware` guarantees, since
`Kernel.ServeHTTP` always appends its routing `dispatch` as the *last*
handler in the chain, after every global middleware. By the time a route
is matched, `Request.Method` already reflects any override. See
[request-lifecycle.md](request-lifecycle.md).

## Writing your own middleware

For **stateless** middleware with no parameters, adapt a closure:

```go
func Authenticate() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		if c.Request.Header.Get("Authorization") == "" {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next()
	})
}
```

For **parameterized or terminable** middleware (or one that needs injected
dependencies), define a struct:

```go
type Throttle struct{ limiter *rate.Limiter } // example dependency

func NewThrottle(limiter *rate.Limiter) *Throttle {
	return &Throttle{limiter: limiter}
}

func (m *Throttle) Handle(c *apphttp.Context, next func(), params ...string) {
	if !m.limiter.Allow() {
		c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
		return
	}
	next()
}
```

Then register it either:

- **globally** — `app.Kernel.UseMiddleware(appMiddleware.Logger(), NewThrottle(limiter))`
- as a **named alias** — `kernel.AliasMiddleware("throttle", NewThrottle(limiter))`, then `.Middleware("throttle")`
- **through the container** — `kernel.Container().Bind("throttle", NewThrottle(limiter))`, then `.Middleware("throttle")`
- as a **group member** — `kernel.MiddlewareGroup("api", "throttle", "auth")`

## `VerifyCsrfToken`

Golite's fifth example middleware, and its most involved: a
parameterized-free but session-dependent struct that reads/writes cookies,
compares a token in constant time, and exempts paths via a wildcard
`Except` list. It's also seeded into the `"web"` middleware group by
`NewKernel` itself, by name only, to avoid an import cycle. Full writeup,
including a Go-specific cookie-ordering fix that was only caught by
end-to-end testing, in [security-csrf.md](security-csrf.md).
