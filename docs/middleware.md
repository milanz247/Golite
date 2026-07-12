# Middleware

Files: [`app/Http/Kernel.go`](../app/Http/Kernel.go),
[`app/Http/Middleware/LoggerMiddleware.go`](../app/Http/Middleware/LoggerMiddleware.go),
[`app/Http/Middleware/MethodSpoofingMiddleware.go`](../app/Http/Middleware/MethodSpoofingMiddleware.go)

## Shape of a middleware

A middleware is just a function that returns a `HandlerFunc`:

```go
type HandlerFunc func(*Context)
```

There's no separate middleware type — a middleware and a route handler are
the same shape (`func(*Context)`), which is what makes it possible to build
a single flat chain out of both.

## How the chain runs — `Context.Next()`

```go
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}
```

This is a **recursive** pipeline, not an iterating loop: `Next()` advances
the index by one and, if there's a handler there, calls it directly —
once. If that handler calls `c.Next()` itself, execution recurses one level
deeper; when it returns, control unwinds back through every ancestor call,
letting each middleware run code both *before* and *after* the rest of the
chain (classic "onion" middleware).

**Not calling `c.Next()` genuinely stops the chain** — nothing further down
ever runs, no `Abort()` call needed:

```go
func Authenticate() apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		if c.Request.Header.Get("Authorization") == "" {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return // nothing downstream runs
		}
		c.Next()
	}
}
```

This matters and was specifically verified: an earlier iterative
implementation of `Next()` (the classic Gin-style `for` loop) kept advancing
to the next handler regardless of whether the current one called `Next()`,
which meant an "unauthorized" middleware that just returned still let the
controller run afterward, double-writing the response. The recursive form
above doesn't have that failure mode.

Handlers appended to `c.handlers` *while* the chain is running — which is
exactly what `Kernel.dispatch` does once it resolves a route (see below) —
are picked up correctly, since each `Next()` call re-checks the current
`len(c.handlers)`.

## `LoggerMiddleware`

```go
func Logger() apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		start := time.Now()
		method := c.Request.Method
		path := c.Request.URL.Path

		c.Next()

		log.Printf("%s %s completed in %s", method, path, time.Since(start))
	}
}
```

`Logger()` is a **factory** — it returns the actual `HandlerFunc`, so
middleware can take configuration as constructor arguments without changing
the `HandlerFunc` signature. It calls `c.Next()` to run everything
downstream, then logs the elapsed time *after* `Next()` returns — i.e.
after the whole rest of the chain (remaining global middleware, routing,
route-specific middleware, and the controller) has finished.

## `MethodSpoofingMiddleware`

```go
func MethodSpoofing() apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		if c.Request.Method == http.MethodPost {
			if override := resolveOverride(c.Request); spoofableMethods[override] {
				c.Request.Method = override
			}
		}
		c.Next()
	}
}
```

HTML forms only support `GET` and `POST`. `MethodSpoofing` lets a `POST`
request simulate `PUT`, `PATCH`, or `DELETE` by checking, in order:

1. an `X-HTTP-Method-Override` header, or
2. a hidden `_method` form field (read via `r.PostFormValue("_method")`,
   which parses the body as a form as needed and leaves non-form bodies —
   e.g. JSON — untouched).

If either names `PUT`, `PATCH`, or `DELETE`, it overwrites
`c.Request.Method` before calling `c.Next()`.

**This must run as global middleware, and before routing** — which is
exactly what registering it via `Kernel.UseMiddleware` guarantees (see
[request-lifecycle.md](request-lifecycle.md)): `Kernel.ServeHTTP` always
appends its own routing dispatch as the *last* handler in the chain, after
every global middleware, so by the time a route is matched, `Request.Method`
already reflects any override. Registered in
[`public/main.go`](../public/main.go):

```go
app.Kernel.UseMiddleware(appMiddleware.MethodSpoofing(), appMiddleware.Logger())
```

(Spoofing is registered before `Logger` so the logged method reflects the
overridden verb, not the original `POST`.)

## Registering global middleware

```go
func (k *Kernel) UseMiddleware(middleware ...HandlerFunc) {
	k.mu.Lock()
	k.globalMiddleware = append(k.globalMiddleware, middleware...)
	k.mu.Unlock()
}
```

`UseMiddleware` is variadic and thread-safe. Every registered global
middleware runs on **every** request — matched route, 405, fallback, or
plain 404 — because `Kernel.ServeHTTP` always builds the chain as
`[...globalMiddleware, k.dispatch]`, and `dispatch` is what actually
resolves routing (see [request-lifecycle.md](request-lifecycle.md)).

## Middleware aliases

Route- and group-level middleware (`Route::middleware("auth")` in Laravel)
are referenced **by string name**, not by a direct `HandlerFunc` value.
Golite resolves those names against a small alias registry on the kernel:

```go
func (k *Kernel) AliasMiddleware(name string, mw HandlerFunc)
```

```go
kernel.AliasMiddleware("auth", func(c *apphttp.Context) {
	if c.Request.Header.Get("Authorization") == "" {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	c.Next()
})

kernel.Prefix("admin").Middleware("auth").Group(func(admin *apphttp.RouteGroup) {
	admin.GET("/users", handler) // runs the "auth" alias before handler
})
```

`RouteGroup.Middleware(names ...string)` and
`RouteDefinition.Middleware(names ...string)` both just accumulate alias
names; resolution to actual `HandlerFunc`s happens per-request in
`Kernel.dispatch`, via `Kernel.resolveMiddleware`, which looks each name up
in `middlewareAliases` under a read lock. An alias that was never
registered is silently skipped — there's no ordering requirement between
`AliasMiddleware` and the routes that reference it, since resolution is
deferred to request time rather than done at route-registration time.

Route-specific middleware runs *after* whatever its enclosing group(s)
already contributed (group middleware is copied into the route's
`middlewareNames` at creation; `.Middleware()` on the route appends more),
and always after every *global* middleware, since it only enters the chain
once `dispatch` has already matched a route.

## Writing your own middleware

1. Add a new file under `app/Http/Middleware/`, e.g. `AuthMiddleware.go`.
2. Follow the factory pattern used by `Logger`/`MethodSpoofing`:

   ```go
   package middleware

   import (
       "net/http"

       apphttp "Golite/app/Http"
   )

   func Authenticate() apphttp.HandlerFunc {
       return func(c *apphttp.Context) {
           if c.Request.Header.Get("Authorization") == "" {
               c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
               return // not calling c.Next() stops the chain here
           }
           c.Next()
       }
   }
   ```

3. Register it either **globally** (runs on every request, before routing):

   ```go
   app.Kernel.UseMiddleware(appMiddleware.Logger(), appMiddleware.Authenticate())
   ```

   or as a **named alias** for use on specific routes/groups:

   ```go
   app.Kernel.AliasMiddleware("auth", appMiddleware.Authenticate())
   // then, in routes/web.go:
   kernel.Prefix("admin").Middleware("auth").Group(func(r *apphttp.RouteGroup) { ... })
   ```
