# Routing

Files: [`app/Http/Kernel.go`](../app/Http/Kernel.go),
[`routes/web.go`](../routes/web.go)

## The route table

The `Kernel` stores routes in a single map keyed by `"METHOD /path"`:

```go
type Kernel struct {
	container  *container.Container
	middleware []HandlerFunc

	mu     sync.RWMutex
	routes map[string]HandlerFunc
}

func routeKey(method, path string) string {
	return method + " " + path
}
```

Registration helpers:

```go
func (k *Kernel) GET(path string, handler HandlerFunc)
func (k *Kernel) POST(path string, handler HandlerFunc)
func (k *Kernel) Handle(method, path string, handler HandlerFunc)
```

`GET`/`POST` are thin wrappers around `Handle`, which takes a write lock and
inserts into the map:

```go
func (k *Kernel) Handle(method, path string, handler HandlerFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.routes[routeKey(method, path)] = handler
}
```

Routes only need to be registered once at boot, but the map is guarded by a
`sync.RWMutex` so registration and per-request lookups are both safe if
routes are ever added dynamically.

> **Note:** matching is exact-path only — there is no support for route
> parameters (`/user/{id}`) or wildcards yet. Add path-parameter parsing to
> `routeKey`/lookup logic in `Kernel` if you need it (see
> [developer-guide.md](developer-guide.md) for extension points).

## `routes/web.go`

This is Golite's equivalent of Laravel's `routes/web.php` — a single
function that maps every web route onto the kernel:

```go
package routes

import (
	apphttp "Golite/app/Http"
	"Golite/app/Http/Controllers"
)

func MapWebRoutes(kernel *apphttp.Kernel) {
	userController := controllers.NewUserController()

	kernel.GET("/user", userController.Show)
}
```

It's called from `RouteServiceProvider.Boot` (see
[service-providers.md](service-providers.md)), which resolves the kernel
out of the container and passes it in.

## Adding a new route

1. Write (or reuse) a controller method with the signature
   `func(c *apphttp.Context)` under `app/Http/Controllers/`.
2. Register it in `routes/web.go`:

   ```go
   func MapWebRoutes(kernel *apphttp.Kernel) {
       userController := controllers.NewUserController()
       kernel.GET("/user", userController.Show)

       postController := controllers.NewPostController()
       kernel.GET("/posts", postController.Index)
       kernel.POST("/posts", postController.Store)
   }
   ```

No other file needs to change — `RouteServiceProvider` already calls
`MapWebRoutes` for you during boot.

See [middleware.md](middleware.md) for how middleware wraps every route
(Golite currently only supports global middleware — see that doc's
"Extending to per-route middleware" section for how to add scoped
middleware), and [request-lifecycle.md](request-lifecycle.md) for how a
matched route is actually dispatched.
