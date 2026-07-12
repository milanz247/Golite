# Request Lifecycle

Once `public/main.go` calls `http.ListenAndServe(app.Config.App.Port,
app.Kernel)`, every incoming HTTP request follows the same path through the
framework. This is Golite's equivalent of Laravel's front-controller +
pipeline flow (`public/index.php` → `Kernel::handle()` → global middleware
pipeline → router → route middleware → controller → response) — and,
crucially, **routing happens *after* global middleware**, not before, so
middleware like method spoofing can influence which route matches.

## Step by step

File: [`app/Http/Kernel.go`](../app/Http/Kernel.go)

```go
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	global := make([]HandlerFunc, len(k.globalMiddleware))
	copy(global, k.globalMiddleware)
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(global)+1)
	chain = append(chain, global...)
	chain = append(chain, k.dispatch)

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()
}
```

1. **The Go standard library calls `Kernel.ServeHTTP`** for every request,
   because `Kernel` implements `http.Handler`.
2. **Chain assembly.** The chain is every registered global middleware
   (`Kernel.UseMiddleware` — e.g. `MethodSpoofing`, `Logger`), in
   registration order, followed by exactly one more handler:
   `Kernel.dispatch` itself. Note that **no routing has happened yet** —
   the route table isn't even consulted here.
3. **Context creation.** A fresh `*Context` is created per request, wrapping
   `http.ResponseWriter`, `*http.Request`, the shared `*container.Container`
   (as `Context.App`), and the assembled chain.
4. **`ctx.Next()` starts the pipeline.**

## How `Context.Next()` runs the chain

```go
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}
```

This is a recursive "onion" pipeline: each `Next()` call advances the index
by one and, if there's a handler there, calls it directly. If that handler
calls `c.Next()`, execution recurses into the next handler; when that
returns, control unwinds back to the caller, letting middleware run code
both before and after the rest of the chain. If a handler does *not* call
`c.Next()` (e.g. failed auth), nothing further down the chain ever runs —
see [middleware.md](middleware.md#how-the-chain-runs--contextnext) for why
this specific (recursive, not iterating) form matters.

## `Kernel.dispatch`: where routing actually happens

```go
func (k *Kernel) dispatch(c *Context) {
	route, params, pathMatched, allowed := k.match(c.Request.Method, c.Request.URL.Path)

	if route != nil {
		c.params = params
		c.handlers = append(c.handlers, k.resolveMiddleware(route.middlewareNamesCopy())...)
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
2. **On a match**, resolves that route's middleware aliases to actual
   `HandlerFunc`s and **splices them, plus the route's own handler, onto
   the end of `c.handlers`** — the *same* slice the outer `Next()` calls are
   iterating — then calls `c.Next()` again to continue into them.
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

## Example: `GET /user`

Registered in [`routes/web.go`](../routes/web.go) via
`kernel.GET("/user", userController.Show).Name("user.show")`, with
`MethodSpoofing` and `Logger` as global middleware:

```
ServeHTTP  (chain = [MethodSpoofing, Logger, dispatch])
  -> ctx.Next()
       -> MethodSpoofing(ctx)     (not a POST, no-op)
            -> ctx.Next()
                 -> Logger(ctx)                (start timer)
                      -> ctx.Next()
                           -> dispatch(ctx)
                                -> k.match("GET", "/user")           // route found
                                -> c.handlers += [userController.Show]
                                -> ctx.Next()
                                     -> UserController.Show(ctx)
                                          -> ctx.App.Make("hash")    // resolve service from container
                                          -> ctx.App.Make("config")  // resolve config from container
                                          -> ctx.JSON(200, {...})    // write response
                      <- back in Logger, log elapsed duration
```

### Example: a spoofed `PUT` via a form

`POST /posts/5` with body `_method=PUT`, matched against
`kernel.PUT("/posts/{post}", handler).WhereNumber("post")`:

```
ServeHTTP  (chain = [MethodSpoofing, Logger, dispatch])
  -> MethodSpoofing(ctx)
       -> sees POST + _method=PUT -> c.Request.Method = "PUT"
       -> ctx.Next()
            -> Logger(ctx) -> ctx.Next()
                 -> dispatch(ctx)
                      -> k.match("PUT", "/posts/5")   // matches; Method was already overridden
                      -> params = {"post": "5"}
                      -> runs the route's handler
```

Because `dispatch` — and therefore route matching — only runs *after*
`MethodSpoofing`, the override is visible to `k.match`, which is the whole
point of registering spoofing as global middleware rather than, say, route
middleware.

See [service-container.md](service-container.md) for how `ctx.App.Make(...)`
resolves services, and [routing.md](routing.md) /
[middleware.md](middleware.md) for the full detail on how routes and
middleware are registered.
