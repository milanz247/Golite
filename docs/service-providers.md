# Service Providers

Files: [`app/Providers/ServiceProvider.go`](../app/Providers/ServiceProvider.go),
[`app/Providers/AppServiceProvider.go`](../app/Providers/AppServiceProvider.go),
[`app/Providers/RouteServiceProvider.go`](../app/Providers/RouteServiceProvider.go)

Service providers are the central place where the application is assembled
— exactly like Laravel. Every provider implements:

```go
type ServiceProvider interface {
	Register(c *container.Container)
	Boot(c *container.Container)
}
```

- **`Register(c)`** — bind services into the container. Must not depend on
  any other provider having run yet, because registration order between
  providers isn't guaranteed to matter (each provider only registers what
  it owns).
- **`Boot(c)`** — run logic that depends on every provider's bindings
  already being registered (see [bootstrapping.md](bootstrapping.md) for
  exactly when `Register` vs `Boot` run).

## `AppServiceProvider`

The default place for core, app-wide bindings — Golite's counterpart to
Laravel's `App\Providers\AppServiceProvider`.

```go
type AppServiceProvider struct{}

func (p *AppServiceProvider) Register(c *container.Container) {
	cfg := c.Make("config").(*config.Config)

	hasher := hashing.NewManager(cfg.Hash.Driver)
	hasher.Extend("bcrypt", hashing.NewBcryptHasher(cfg.Hash.BcryptCost))
	c.Bind("hash", hasher)

	c.Bind("encrypter", encryption.NewEncrypter(cfg.App.Key))

	logger := logging.NewManager(cfg.Log.Channel)
	logger.Extend("single", logging.NewSingleChannel(cfg.Log.Path))
	c.Bind("log", logger)
}

func (p *AppServiceProvider) Boot(c *container.Container) {
	fmt.Println("[AppServiceProvider] booted")
}
```

It binds Golite's core, real (not stand-in) services: `"hash"`
(`*hashing.Manager`, bcrypt-backed — see [hashing.md](hashing.md)),
`"encrypter"` (`*encryption.Encrypter` — see
[encryption.md](encryption.md)), and `"log"` (`*logging.Manager` — see
[logging.md](logging.md)), each configured from `*config.Config`, which
was already bound as `"config"` by `bootstrap.NewApplication` before any
provider runs (see [bootstrapping.md](bootstrapping.md)).

## `RouteServiceProvider`

Loads the routing engine — Golite's counterpart to Laravel's
`App\Providers\RouteServiceProvider`, which maps `routes/web.php` onto the
router.

```go
type RouteServiceProvider struct{}

func (p *RouteServiceProvider) Register(c *container.Container) {}

func (p *RouteServiceProvider) Boot(c *container.Container) {
	kernel := c.Make("kernel").(*apphttp.Kernel)
	routes.MapWebRoutes(kernel)
}
```

`Register` is deliberately empty: mapping routes requires the kernel to
already exist and be resolvable from the container, and other providers may
still want a chance to register their own bindings first — so route mapping
is deferred entirely to `Boot`.

## Registering a provider

Providers are wired up in [`public/main.go`](../public/main.go):

```go
app := bootstrap.NewApplication()

app.Register(&providers.AppServiceProvider{})
app.Register(&providers.RouteServiceProvider{})

app.Kernel.UseMiddleware(appMiddleware.Logger())

app.Boot()
```

`app.Register(p)` calls `p.Register(container)` immediately. `app.Boot()`
later calls `Boot(container)` on every registered provider, in the order
they were registered.

## Writing your own provider

1. Create a new file under `app/Providers/`, e.g. `AuthServiceProvider.go`.
2. Define a struct and implement `Register`/`Boot`:

   ```go
   package providers

   import "Golite/container"

   type AuthServiceProvider struct{}

   func (p *AuthServiceProvider) Register(c *container.Container) {
       c.Bind("auth", NewAuthManager())
   }

   func (p *AuthServiceProvider) Boot(c *container.Container) {}
   ```

3. Register it in `public/main.go`:

   ```go
   app.Register(&providers.AuthServiceProvider{})
   ```

That's it — no config file or array to edit, since providers are registered
explicitly in code rather than discovered via configuration.
