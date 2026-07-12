# Request Lifecycle

Once `public/main.go` calls `http.ListenAndServe(app.Config.App.Port,
app.Kernel)`, every incoming HTTP request follows the same path through the
framework. This is Golite's equivalent of Laravel's front-controller +
pipeline flow (`public/index.php` → `Kernel::handle()` → middleware
pipeline → controller → response).

## Step by step

File: [`app/Http/Kernel.go`](../app/Http/Kernel.go)

```go
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	handler, ok := k.routes[routeKey(r.Method, r.URL.Path)]
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(k.middleware)+1)
	chain = append(chain, k.middleware...)

	if ok {
		chain = append(chain, handler)
	} else {
		chain = append(chain, func(c *Context) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "404 not found"})
		})
	}

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()
}
```

1. **The Go standard library calls `Kernel.ServeHTTP`** for every request,
   because `Kernel` implements `http.Handler`.
2. **Route lookup.** The kernel looks up `METHOD /path` (e.g. `"GET
   /user"`) in its route table under a read lock. Route registration
   (`GET`/`POST`/`Handle`) takes a write lock, so the route table is safe to
   read and write concurrently.
3. **Chain assembly.** A slice (`chain`) is built containing, in order:
   - every global middleware registered via `Kernel.UseMiddleware`
     (e.g. `LoggerMiddleware`), then
   - either the matched route handler, or a built-in `404` JSON handler if
     no route matched.
4. **Context creation.** A fresh `*Context` is created per request, wrapping
   `http.ResponseWriter`, `*http.Request`, the shared `*container.Container`
   (as `Context.App`), and the assembled chain.
5. **`ctx.Next()` starts the pipeline.**

## How `Context.Next()` runs the chain

```go
func (c *Context) Next() {
	c.index++
	for c.index < len(c.handlers) {
		c.handlers[c.index](c)
		c.index++
	}
}
```

This is the same iterative pattern used by frameworks like Gin. It lets
middleware wrap the rest of the chain — run code, call `c.Next()`, then run
more code — without recursion:

- `ctx.Next()` (called once by `ServeHTTP`) increments `index` to `0` and
  loops: it calls `handlers[0]` (the first middleware), then increments
  `index` to `1` and checks the loop condition again.
- If `handlers[0]` (e.g. `LoggerMiddleware`) itself calls `c.Next()`, that
  nested call increments `index` again and *continues the same loop*,
  running `handlers[1]`, `handlers[2]`, ... until the chain is exhausted.
  Control then returns to the middleware, which can run code **after** the
  rest of the chain has finished (e.g. logging the elapsed duration).
- If a handler does *not* call `c.Next()` (e.g. an auth middleware
  rejecting the request), the outer loop's `for` condition still advances
  past it on return, but nothing downstream ever runs — short-circuiting
  the pipeline, just like `abort()` in Laravel middleware.

## Example: `GET /user`

Registered in [`routes/web.go`](../routes/web.go) during
`RouteServiceProvider.Boot`:

```go
kernel.GET("/user", userController.Show)
```

With `LoggerMiddleware` as the only global middleware, an incoming `GET
/user` builds the chain `[Logger, UserController.Show]` and executes as:

```
ServeHTTP
  -> ctx.Next()
       -> Logger(ctx)                (start timer, log method + path)
            -> ctx.Next()
                 -> UserController.Show(ctx)
                      -> ctx.App.Make("hash")    // resolve service from container
                      -> ctx.App.Make("config")  // resolve config from container
                      -> ctx.JSON(200, {...})    // write response
            <- back in Logger, log elapsed duration
```

See [service-container.md](service-container.md) for how `ctx.App.Make(...)`
resolves services, and [routing.md](routing.md) /
[middleware.md](middleware.md) for how routes and middleware are registered.
