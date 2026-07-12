# Request Lifecycle

Once `public/main.go` calls `http.ListenAndServe(app.Config.App.Port,
app.Kernel)`, every incoming HTTP request follows the same path through the
framework. This is Golite's equivalent of Laravel's front-controller +
pipeline flow (`public/index.php` → `Kernel::handle()` → global middleware
pipeline → router → route middleware → controller → response →
`Kernel::terminate()`) — and, crucially, **routing happens *after* global
middleware**, not before, so middleware like method spoofing can influence
which route matches.

## Step by step

File: [`app/Http/Kernel.go`](../app/Http/Kernel.go)

```go
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	global := make([]Middleware, len(k.GlobalMiddleware))
	copy(global, k.GlobalMiddleware)
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(global)+1)
	for _, mw := range global {
		chain = append(chain, toHandler(mw, nil))
	}
	chain = append(chain, k.dispatch)

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()

	k.terminate(ctx)
}
```

1. **The Go standard library calls `Kernel.ServeHTTP`** for every request,
   because `Kernel` implements `http.Handler`.
2. **Chain assembly.** Every registered global middleware
   (`Kernel.UseMiddleware` — e.g. `MethodSpoofing`, `Logger`) is wrapped
   into a `HandlerFunc` via `toHandler` (see
   [middleware.md](middleware.md#how-the-chain-runs--contextnext)), in
   registration order, followed by exactly one more handler:
   `Kernel.dispatch` itself. Note that **no routing has happened yet** —
   the route table isn't even consulted here.
3. **Context creation.** A fresh `*Context` is created per request, wrapping
   `http.ResponseWriter`, `*http.Request`, the shared `*container.Container`
   (as `Context.App`), and the assembled chain.
4. **`ctx.Next()` starts the pipeline** and runs to completion — every
   global middleware, routing, any route-specific middleware, and the
   controller.
5. **`k.terminate(ctx)` runs after the chain returns** — see
   [Termination](#termination-after-the-response-is-sent) below.

## How `Context.Next()` runs the chain

File: [`app/Http/Context.go`](../app/Http/Context.go)

```go
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}
```

This is a recursive "onion" pipeline: each `Next()` call advances the index
by one and, if there's a handler there, calls it directly. Handlers in
`c.handlers` are always plain `HandlerFunc`s — middleware gets there via
`toHandler`, which wraps a `Middleware` into a `HandlerFunc` that records
it as executed and calls `mw.Handle(c, c.Next, params...)`, passing
`Context.Next` itself as the `next` callback. If a middleware's `Handle`
doesn't call `next()` (e.g. failed auth), nothing further down the chain
ever runs — see
[middleware.md](middleware.md#how-the-chain-runs--contextnext) for why
this specific (recursive, not iterating) form matters.

## `Kernel.dispatch`: where routing actually happens

```go
func (k *Kernel) dispatch(c *Context) {
	route, params, pathMatched, allowed := k.match(c.Request.Method, c.Request.URL.Path)

	if route != nil {
		c.params = params
		for _, rm := range k.resolveRouteMiddleware(route) {
			c.handlers = append(c.handlers, toHandler(rm.mw, rm.params))
		}
		c.handlers = append(c.handlers, route.handler)
		c.Next()
		return
	}

	if pathMatched && len(allowed) > 0 {
		c.Writer.Header().Set("Allow", strings.Join(allowed, ", "))
		c.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "405 method not allowed"})
		return
	}

	k.mu.RLock()
	fallback := k.fallback
	k.mu.RUnlock()
	if fallback != nil {
		fallback.handler(c)
		return
	}

	c.JSON(http.StatusNotFound, map[string]string{"error": "404 not found"})
}
```

`dispatch` is always the *last* handler in the initial chain, so by the
time it runs, every global middleware has already executed — including
`MethodSpoofingMiddleware`, so `c.Request.Method` already reflects any
`_method`/`X-HTTP-Method-Override` override. It then:

1. **Matches** the current method + path against the route table
   (`Kernel.match`, see [routing.md](routing.md)).
2. **On a match**, resolves that route's middleware — expanding
   `MiddlewareGroups`, dropping anything excluded via `WithoutMiddleware`,
   resolving each remaining name (via `RouteMiddleware` or the service
   container), and sorting by `MiddlewarePriority` (see
   [middleware.md](middleware.md#middleware-priority)) — wraps each into a
   `HandlerFunc` via `toHandler`, and **splices them, plus the route's own
   handler, onto the end of `c.handlers`** — the *same* slice the outer
   `Next()` calls are iterating — then calls `c.Next()` again to continue
   into them.
3. **On a path match but method mismatch**, responds `405 Method Not
   Allowed` with an `Allow` header (see
   [routing.md](routing.md#405-vs-404)).
4. **On no match at all**, runs the registered `Kernel.Fallback` handler if
   one exists, or a built-in `404` JSON response otherwise.

### Why splicing into the same `Context` works

Because `Next()` re-reads `len(c.handlers)` on every call rather than
capturing it once, appending new handlers mid-flight and calling `c.Next()`
again seamlessly continues the *same* pipeline — route-specific middleware
and the controller run as a natural extension of the global-middleware
chain, with no separate `Context` or nested dispatch mechanism needed.

## Termination — after the response is sent

```go
func (k *Kernel) terminate(c *Context) {
	for _, mw := range c.executed {
		if t, ok := mw.(TerminableMiddleware); ok {
			t.Terminate(c)
		}
	}
}
```

`toHandler` appends every middleware to `c.executed` right before calling
its `Handle` — so by the time `ctx.Next()` returns in `ServeHTTP`, having
already written the response, `c.executed` holds exactly the middleware
that actually ran for this request, in the order they ran (a middleware
that never got reached because something upstream short-circuited first
never appears here). `k.terminate` then calls `Terminate` on whichever of
those implement `TerminableMiddleware` — post-response cleanup or logging
that shouldn't delay the response itself. See
[middleware.md](middleware.md#terminable-middleware).

## Example: `GET /user`

Registered in [`routes/web.go`](../routes/web.go) via
`kernel.GET("/user", userController.Show).Name("user.show")`, with
`MethodSpoofing` and `Logger` as global middleware:

```
ServeHTTP  (chain = [MethodSpoofing, Logger, dispatch])
  -> ctx.Next()
       -> MethodSpoofing.Handle(ctx, next)     (not a POST, no-op)
            -> next()
                 -> Logger.Handle(ctx, next)                (start timer)
                      -> next()
                           -> dispatch(ctx)
                                -> k.match("GET", "/user")           // route found
                                -> c.handlers += [userController.Show]
                                -> ctx.Next()
                                     -> UserController.Show(ctx)
                                          -> ctx.App.Make("hash")    // resolve service from container
                                          -> ctx.App.Make("config")  // resolve config from container
                                          -> ctx.JSON(200, {...})    // write response
                      <- back in Logger, log elapsed duration
  <- ctx.Next() (from ServeHTTP) returns
  -> k.terminate(ctx)  // no TerminableMiddleware ran here, so this is a no-op
```

### Example: a spoofed `PUT` via a form

`POST /posts/5` with body `_method=PUT`, matched against
`kernel.PUT("/posts/{post}", handler).WhereNumber("post")`:

```
ServeHTTP  (chain = [MethodSpoofing, Logger, dispatch])
  -> MethodSpoofing.Handle(ctx, next)
       -> sees POST + _method=PUT -> c.Request.Method = "PUT"
       -> next()
            -> Logger.Handle(ctx, next) -> next()
                 -> dispatch(ctx)
                      -> k.match("PUT", "/posts/5")   // matches; Method was already overridden
                      -> params = {"post": "5"}
                      -> runs the route's handler
```

Because `dispatch` — and therefore route matching — only runs *after*
`MethodSpoofing`, the override is visible to `k.match`, which is the whole
point of registering spoofing as global middleware rather than, say, route
middleware.

### Example: terminable middleware via a container-resolved audit log

`GET /admin/users`, inside a group with `.Middleware("web")` (which expands
to `["auth", "audit"]`), where `"audit"` was bound into the service
container rather than aliased:

```
dispatch(ctx)
  -> k.resolveRouteMiddleware(route)
       -> expand "web" -> ["auth", "audit"]
       -> resolve "auth" via RouteMiddleware, "audit" via kernel.Container().Make("audit")
       -> sort by MiddlewarePriority = ["auth", "role", "audit"]  -> [auth, audit]
  -> c.handlers += [toHandler(auth), toHandler(audit), route.handler]
  -> ctx.Next()
       -> auth.Handle(ctx, next)   -> next()
            -> audit.Handle(ctx, next)   // records c.Set("audit.start", now)
                 -> next()
                      -> route.handler(ctx)   // writes the response
<- ctx.Next() (from ServeHTTP) returns
-> k.terminate(ctx)
     -> auth doesn't implement TerminableMiddleware -> skipped
     -> audit.Terminate(ctx) -> logs elapsed time using c.Get("audit.start")
```

See [service-container.md](service-container.md) for how `ctx.App.Make(...)`
resolves services, and [routing.md](routing.md) /
[middleware.md](middleware.md) for the full detail on how routes and
middleware are registered.
