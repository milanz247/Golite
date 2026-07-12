# Service Container

File: [`container/container.go`](../container/container.go)

Golite's `Container` is a minimal, thread-safe IoC container — the Go
equivalent of Laravel's `Illuminate\Container\Container`. It has exactly two
operations:

```go
type Container struct {
	mu       sync.RWMutex
	services map[string]any
}

func New() *Container
func (c *Container) Bind(name string, service any)
func (c *Container) Make(name string) any
```

- **`Bind(name, service)`** registers any value (a struct pointer, a
  primitive, a closure, anything) under a string key. Calling `Bind` again
  with the same name overwrites the previous binding.
- **`Make(name)`** returns whatever was bound under that name, or `nil` if
  nothing was. It's the caller's responsibility to type-assert the result
  to the expected type, e.g.:

  ```go
  cfg := c.Make("config").(*config.Config)
  ```

- **Thread safety.** `Bind` takes a write lock (`sync.RWMutex.Lock`); `Make`
  takes a read lock (`RLock`). Bindings normally only happen during startup
  (`NewApplication`, `Register`, `Boot`), while `Make` happens on every
  request, so reads are optimized to run concurrently.

## Where the container lives

One `*container.Container` is created in `bootstrap.NewApplication()` and
threaded through the whole app:

- `Application.Container` — held by the bootstrap layer.
- `Kernel.container` — passed to `apphttp.NewKernel(c)` so every request's
  `Context.App` can resolve services.
- Every `ServiceProvider.Register(c)` / `Boot(c)` call receives it directly.

Because there is exactly one container instance per running application, a
binding made anywhere (a provider, `bootstrap.NewApplication`, etc.) is
visible everywhere else that holds a reference to the same container.

## What gets bound, by whom

| Key        | Bound in                                         | Type               |
|------------|---------------------------------------------------|--------------------|
| `"config"` | `bootstrap.NewApplication`                        | `*config.Config`   |
| `"kernel"` | `bootstrap.NewApplication`                        | `*apphttp.Kernel`  |
| `"hash"`   | `providers.AppServiceProvider.Register`           | `*providers.Hasher`|

## Resolving a service without an import cycle

`UserController` needs the `"hash"` service, but it must not import
`app/Providers` (that would create a cycle — see
[architecture.md](architecture.md#design-decisions-worth-knowing)). Instead
it declares the minimal interface it actually needs and type-asserts to
that:

```go
type hashService interface {
	Make(value string) string
}

func (u *UserController) Show(c *apphttp.Context) {
	hasher := c.App.Make("hash").(hashService)
	...
}
```

Since `*providers.Hasher` has a `Make(string) string` method, it
automatically satisfies `hashService` — Go's structural typing means no
import of the concrete type is needed. Use this pattern whenever a consumer
needs a service but binding its package would create a cycle.

## Adding your own binding

```go
// in some provider's Register method
c.Bind("mailer", NewSMTPMailer(cfg.Mail))

// anywhere with access to the container
mailer := c.Make("mailer").(*Mailer)
```

See [service-providers.md](service-providers.md) for where bindings belong
in the provider lifecycle.
