# Bootstrapping Process

This is the Go equivalent of reading through Laravel's
`bootstrap/app.php` + `public/index.php`. Every step below happens, in
order, each time the `public/main.go` binary starts.

## 1. `bootstrap.NewApplication()`

File: [`bootstrap/app.go`](../bootstrap/app.go)

```go
func NewApplication() *Application {
	c := container.New()
	cfg := config.LoadConfig()
	kernel := apphttp.NewKernel(c)

	c.Bind("config", cfg)
	c.Bind("kernel", kernel)

	return &Application{
		Container: c,
		Config:    cfg,
		Kernel:    kernel,
	}
}
```

This single call does four things, in order:

1. **Creates the service container** (`container.New()`) — an empty,
   thread-safe map of name → service.
2. **Loads configuration** (`config.LoadConfig()`) — reads `.env` via
   `godotenv.Load()` and falls back to defaults for `APP_NAME`, `APP_ENV`,
   `APP_PORT`. See [configuration.md](configuration.md).
3. **Creates the HTTP kernel** (`apphttp.NewKernel(c)`) — the router +
   global middleware stack, bound to the same container so every request
   can resolve services out of it.
4. **Binds `"config"` and `"kernel"` into the container itself**, so any
   later provider or controller can resolve them with `container.Make(...)`
   instead of needing them passed in explicitly.

The returned `*Application` struct is Golite's equivalent of Laravel's
`$app` — it holds the container, the config, and the kernel.

## 2. Registering providers

File: [`public/main.go`](../public/main.go)

```go
app := bootstrap.NewApplication()

app.Register(&providers.AppServiceProvider{})
app.Register(&providers.RouteServiceProvider{})
```

`Application.Register` does exactly what Laravel does when a provider is
added to `config/app.php`'s `providers` array: it appends the provider to an
internal list **and calls `Register(container)` on it immediately**:

```go
func (app *Application) Register(p providers.ServiceProvider) {
	app.providers = append(app.providers, p)
	p.Register(app.Container)
}
```

At this stage, providers should only **bind** things into the container —
they should not assume any other provider has finished registering yet.
`AppServiceProvider.Register` binds a `"hash"` service:

```go
func (p *AppServiceProvider) Register(c *container.Container) {
	c.Bind("hash", NewHasher())
}
```

`RouteServiceProvider.Register` intentionally does nothing — routing needs
the rest of the app to exist first, so it defers all work to `Boot`.

## 3. Registering global middleware

```go
app.Kernel.UseMiddleware(appMiddleware.Logger())
```

Middleware is attached directly to the kernel before `Boot()` runs, so it's
guaranteed to wrap every route, including any routes registered by
`RouteServiceProvider` during boot.

## 4. `app.Boot()`

```go
func (app *Application) Boot() {
	for _, p := range app.providers {
		p.Boot(app.Container)
	}
}
```

This is the **last thing that happens before the server starts accepting
requests** (see [request-lifecycle.md](request-lifecycle.md)). By this
point every provider has already run `Register`, so it's safe for a
provider's `Boot` to resolve services bound by *other* providers.

`RouteServiceProvider.Boot` does exactly that — it resolves the kernel
that was bound to the container back in step 1, and maps the web routes
onto it:

```go
func (p *RouteServiceProvider) Boot(c *container.Container) {
	kernel := c.Make("kernel").(*apphttp.Kernel)
	routes.MapWebRoutes(kernel)
}
```

## 5. Starting the server

```go
http.ListenAndServe(app.Config.App.Port, app.Kernel)
```

`app.Kernel` implements `http.Handler` (see
[request-lifecycle.md](request-lifecycle.md)), so it's passed directly to
the standard library's `http.ListenAndServe`. From this point on, every
incoming request is dispatched through `Kernel.ServeHTTP`.

## Summary: the exact order of operations

1. `container.New()`
2. `config.LoadConfig()`
3. `apphttp.NewKernel(c)`
4. Bind `"config"`, `"kernel"` into the container
5. For each provider, in registration order: `provider.Register(container)`
6. Attach global middleware to the kernel
7. For each provider, in registration order: `provider.Boot(container)`
8. `http.ListenAndServe(port, kernel)`

Steps 5 happens once per `app.Register(...)` call (immediately), while step
7 happens once, in a batch, right before step 8 — this is the "Register()
immediately, Boot() right before the server starts" rule from Laravel,
enforced by `Application` itself rather than by convention.
