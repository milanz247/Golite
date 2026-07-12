# Middleware

Files: [`app/Http/Kernel.go`](../app/Http/Kernel.go),
[`app/Http/Middleware/LoggerMiddleware.go`](../app/Http/Middleware/LoggerMiddleware.go)

## Shape of a middleware

A middleware is just a function that returns a `HandlerFunc`:

```go
type HandlerFunc func(*Context)
```

There's no separate middleware type — a middleware and a route handler are
the same shape (`func(*Context)`), which is what makes it possible to build
a single flat chain out of both (see
[request-lifecycle.md](request-lifecycle.md#how-contextnext-runs-the-chain)).

## `LoggerMiddleware`

```go
package middleware

import (
	"log"
	"time"

	apphttp "Golite/app/Http"
)

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

- `Logger()` is a **factory** — it returns the actual `HandlerFunc`. This
  lets middleware take configuration (e.g. a logger instance, a list of
  excluded paths) as arguments without changing the `HandlerFunc` signature.
- The returned closure captures the request's start time, calls
  `c.Next()` to run everything downstream (remaining middleware, then the
  route handler), and only logs the duration *after* `Next()` returns —
  i.e. after the whole rest of the chain has finished.

## Registering global middleware

Global middleware is attached to the kernel once, in
[`public/main.go`](../public/main.go), before `app.Boot()` runs:

```go
app.Kernel.UseMiddleware(appMiddleware.Logger())
```

```go
func (k *Kernel) UseMiddleware(middleware ...HandlerFunc) {
	k.middleware = append(k.middleware, middleware...)
}
```

`UseMiddleware` is variadic, so multiple middleware can be added in one
call, and they run in the order given. Every registered global middleware
runs on **every** request, matched route or not (including the built-in 404
handler), since `Kernel.ServeHTTP` prepends `k.middleware` to the chain
before appending the route handler.

## Writing your own middleware

1. Add a new file under `app/Http/Middleware/`, e.g. `AuthMiddleware.go`.
2. Follow the same factory pattern:

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
               return // not calling c.Next() short-circuits the chain
           }
           c.Next()
       }
   }
   ```

3. Register it alongside `Logger()` in `public/main.go`:

   ```go
   app.Kernel.UseMiddleware(appMiddleware.Logger(), appMiddleware.Authenticate())
   ```

## Extending to per-route middleware

Golite currently only supports **global** middleware — every route shares
the same stack. To add per-route middleware, the natural extension point is
`Kernel.Handle`/`GET`/`POST`: change their signature to accept extra
`HandlerFunc`s, store them alongside the route's handler in the route
table, and append them (route-specific middleware, then the handler) when
building the chain in `ServeHTTP`, e.g.:

```go
func (k *Kernel) GET(path string, handler HandlerFunc, middleware ...HandlerFunc) {
	// store handler + middleware together, then in ServeHTTP:
	// chain = append(chain, k.middleware...)   // global
	// chain = append(chain, route.middleware...) // per-route
	// chain = append(chain, route.handler)
}
```

This isn't implemented yet — it's a reasonable next step if route-specific
middleware (e.g. auth on some routes but not others) becomes necessary.
