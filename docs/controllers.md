# Controllers & Resource Routing

Files: [`app/Http/Controllers/Controller.go`](../app/Http/Controllers/Controller.go),
[`app/Http/Resource.go`](../app/Http/Resource.go),
[`app/Http/Injection.go`](../app/Http/Injection.go) (`Inject`, method injection),
[`container/container.go`](../container/container.go) (`Container.ResolveType`),
[`app/Http/Controllers/PostController.go`](../app/Http/Controllers/PostController.go),
[`app/Http/Controllers/UserController.go`](../app/Http/Controllers/UserController.go),
[`app/Http/Controllers/CommentController.go`](../app/Http/Controllers/CommentController.go),
[`app/Http/Controllers/ProfileController.go`](../app/Http/Controllers/ProfileController.go),
[`app/Http/Controllers/ProvisionServerController.go`](../app/Http/Controllers/ProvisionServerController.go)

Golite's controller layer mirrors Laravel's in full: a base `Controller`
every controller can embed for declaring its own middleware, constructor
dependency injection resolved from the service container, single-action
(invokable) controllers, and a `Route::resource`-equivalent router that
inspects a controller via reflection and wires up the standard RESTful
routes — including nested, shallow, and singleton variants.

## The base `Controller`

```go
type Controller struct {
	mu    sync.Mutex
	rules []*MiddlewareRule
}

func (c *Controller) Middleware(name string) *MiddlewareRule
func (c *Controller) MiddlewareForAction(action string) []string
```

Every custom controller embeds this, and declares middleware from its
constructor — mirroring Laravel's own `$this->middleware(...)` pattern:

```go
func NewPostController(hasher Hasher) *PostController {
	c := &PostController{hasher: hasher}
	c.Middleware("auth").Except("index", "show")
	return c
}
```

`Middleware(name)` returns a `*MiddlewareRule`, which can be scoped with
`.Only(...)`/`.Except(...)` (variadic — `.Only("index")` or
`.Only("index", "show")` both work); with neither, the rule applies to
every action. Multiple `.Middleware(...)` calls accumulate.

`Controller` lives entirely in `app/Http/Controllers` and imports nothing
from `app/Http` — the connection to the router is structural, not a direct
dependency: `MiddlewareForAction` happens to satisfy a small interface
(`apphttp.ControllerMiddleware`) declared in the router's own package. That
interface is what lets `app/Http/Resource.go` recognize and use *any*
controller that declares middleware this way without ever importing
`app/Http/Controllers` — which it can't, since that package already
imports `app/Http` for `Context`, and the reverse import would be a cycle.
This is the same pattern the codebase already uses for
[`Middleware`](middleware.md#the-middleware-contract) and
[`TerminableMiddleware`](middleware.md#terminable-middleware).

## Dependency injection: two flavors

Golite supports both of Laravel's controller injection styles. Which one
to reach for is a judgment call, not a rule — see the guidance at the end
of this section.

### Constructor injection

"Constructor injection" means exactly what it says: a controller's
constructor takes its dependencies as ordinary parameters, and the caller
(`routes/web.go`, typically) resolves them from the container and passes
them in:

```go
type Hasher interface {
	Make(value string) string
	Check(value, hashedValue string) bool
	NeedsRehash(hashedValue string) bool
}

func NewPostController(hasher Hasher) *PostController { /* ... */ }
```

```go
postController := controllers.NewPostController(kernel.Container().Make("hash").(controllers.Hasher))
```

`Hasher` is exported specifically so call sites outside the `controllers`
package can name it for the type assertion. `PostController` needs this
style regardless of anything else: its dependency has to be available
*before* the controller exists, since `NewPostController` also calls
`c.Middleware("auth").Except(...)` in its own body (see above) — there's
no action method to inject into yet at that point.

### Method injection — `apphttp.Inject`

For a controller whose actions each just need a service for the duration
of that one request — no constructor-time setup — Golite also supports
Laravel's other, arguably more common style: type-hinting the dependency
directly as an action parameter, resolved automatically:

```php
// Laravel
public function show(Hasher $hash, Repository $config) { ... }
```

```go
// Golite
func (u *UserController) Show(c *apphttp.Context, hash Hasher, cfg *config.Config) {
	c.JSON(http.StatusOK, map[string]any{
		"app": cfg.App,
		"user": map[string]string{"token": hash.Make("jane@example.com"), /* ... */},
	})
}
```

Wire it up with `apphttp.Inject`, which wraps the method value into an
ordinary `HandlerFunc`:

```go
kernel.GET("/user", apphttp.Inject(kernel.Container(), userController.Show)).Name("user.show")
```

`Inject` uses `reflect` to walk the handler's parameter list: the first
parameter must be `*apphttp.Context` (checked once, at route-registration
time — a mismatch panics immediately rather than at request time), and
every parameter after it is resolved on *every call* via
`container.Container.ResolveType(t)`, which returns the first bound
service whose concrete type is assignable to `t`. This is what makes
`Hasher` (an interface) and `*config.Config` (a concrete type) both work
as plain parameters with no string key on either side — the container
doesn't need to know a parameter is coming, and the handler doesn't need
to know what key it was bound under.

This is real reflection-based auto-wiring, deliberately added on top of
Golite's plain, no-magic container (`Bind`/`Make` by string name) rather
than replacing it — `ResolveType` still just linearly scans whatever's
bound, so if two services in the container happened to satisfy the same
interface shape, which one `Inject` picks is unspecified (map iteration
order). That's fine for Golite's small, mostly-singleton service set
(`"hash"`, `"encrypter"`, `"log"`, `"config"`, ...), but it's the reason
`Inject` isn't a wholesale replacement for constructor injection: a
controller that genuinely needs to disambiguate between two
implementations of the same interface should take that one explicitly, in
its constructor, instead.

### Which one to use

- **Method injection (`apphttp.Inject`)** for a controller whose actions
  independently need one or two services and nothing else — it's the
  closest match to Laravel's everyday style, and needs no constructor at
  all. `UserController`, `CryptoController`, `HashController`, and
  `LogController` all use it.
- **Constructor injection** when a dependency has to exist before any
  action runs (e.g. to configure `Controller.Middleware(...)` in the
  constructor, like `PostController`), or when a controller needs to
  guarantee *which* implementation of an ambiguous interface it gets
  rather than leaving it to `ResolveType`'s "first match" scan.

## Single-action (invokable) controllers

```go
type Invokable interface {
	Invoke(c *Context)
}

func InvokableHandler(controller Invokable) HandlerFunc {
	return controller.Invoke
}
```

A controller with exactly one action implements `Invoke` instead of a
named method, and is registered directly on a route without ever writing a
method-name string — Golite's equivalent of Laravel's single-action
controller tuple syntax (`Route::post('/server', ProvisionServerController::class)`):

```go
provisionController := controllers.NewProvisionServerController()
kernel.POST("/server", apphttp.InvokableHandler(provisionController))
```

Unlike `Resource`/`ApiResource`/`Singleton` (below), a plain verb route has
no way to know a controller was even involved — `InvokableHandler` returns
a bare `HandlerFunc`, indistinguishable from a closure by the time it
reaches `kernel.POST`. So a single-action controller's own declared
middleware isn't picked up automatically; attach it explicitly with
`ApplyControllerMiddleware`, using the exported `InvokeAction` constant
(`"__invoke"`, PHP's own magic method name) as the action label — though
since an invokable controller only ever has the one action, an unscoped
`.Middleware(...)` rule (no `.Only`/`.Except`) applies regardless of what
label is passed:

```go
apphttp.ApplyControllerMiddleware(
	kernel.POST("/server", apphttp.InvokableHandler(provisionController)),
	provisionController,
	apphttp.InvokeAction,
).Name("server.provision")
```

## `Route::resource` and `Route::apiResource`

```go
func (k *Kernel) Resource(name string, controller any) *ResourceRegistrar
func (k *Kernel) ApiResource(name string, controller any) *ResourceRegistrar
```

```go
kernel.Resource("posts", postController)
```

Registers up to 7 routes, in Laravel's canonical order (important — see
[below](#registration-order-is-not-cosmetic)):

| Verb        | URI                  | Action    | Name          | In `ApiResource`? |
|-------------|----------------------|-----------|---------------|:---:|
| GET         | `/posts`             | `index`   | `posts.index`   | ✅ |
| GET         | `/posts/create`      | `create`  | `posts.create`  | ❌ |
| POST        | `/posts`             | `store`   | `posts.store`   | ✅ |
| GET         | `/posts/{post}`      | `show`    | `posts.show`    | ✅ |
| GET         | `/posts/{post}/edit` | `edit`    | `posts.edit`    | ❌ |
| PUT/PATCH   | `/posts/{post}`      | `update`  | `posts.update`  | ✅ |
| DELETE      | `/posts/{post}`      | `destroy` | `posts.destroy` | ✅ |

`update` is registered as a **single** route matching both `PUT` and
`PATCH` (via the router's existing multi-method
[`Match`](routing.md#http-verb-helpers) support) — one named route, exactly
like Laravel, not two.

`ApiResource` uses the same table minus `create`/`edit` — the two routes
that only exist to serve an HTML `<form>`.

### "Automatically inspect the controller" — reflection, not a required interface

```go
func methodHandler(controller any, methodName string) (HandlerFunc, bool) {
	value := reflect.ValueOf(controller)
	method := value.MethodByName(methodName)
	if !method.IsValid() {
		return nil, false
	}
	methodType := method.Type()
	if methodType.NumIn() != 1 || methodType.NumOut() != 0 || methodType.In(0) != contextPointerType {
		return nil, false
	}
	return func(c *Context) { method.Call([]reflect.Value{reflect.ValueOf(c)}) }, true
}
```

For each candidate action, `Resource`/`ApiResource` looks up the
corresponding Go method (`Index`, `Store`, `Show`, ...) via `reflect`,
checking both that it exists **and** that its signature is exactly
`func(*Context)` — not just "a method with this name." A controller
doesn't need to implement all 7 (or all 5 API) actions; whichever it's
missing are silently skipped rather than registered as a route that would
panic the first time it's hit. `PostController` deliberately doesn't
implement `Create`/`Edit` to demonstrate this: registering it with
`Resource` (not `ApiResource`) still only wires up `Index`, `Store`,
`Show`, `Update`, `Destroy` — the same 5 routes `ApiResource` would give
you regardless of the controller.

**One consequence worth knowing:** a request that *would* have matched a
skipped action's URI instead falls through to whatever else matches. `GET
/posts/create`, with no `create` route registered, matches `show`'s
`/posts/{post}` pattern instead — `post` ends up being the literal string
`"create"`. This isn't a bug; it's the same thing that happens in real
Laravel under `apiResource()`, which never defines `create`/`edit` either.

### Registration order is not cosmetic

Golite's router (like Laravel's) matches routes by trying them in
registration order and taking the first hit — see
[routing.md](routing.md#the-route-table). `create` (`/posts/create`) is
registered *before* `show` (`/posts/{post}`) specifically so a static path
segment gets first refusal over a parameter that would otherwise swallow
it. `Resource`/`ApiResource` always emit the table in this fixed order,
regardless of `Only`/`Except` filtering (which only *removes* entries, it
never reorders what's left).

### `Only` / `Except`

```go
kernel.ApiResource("posts", postController).Except([]string{"destroy"})
```

Unlike the middleware-scoping `Only`/`Except` above (which take variadic
strings), these take a `[]string` directly — matching the literal call
shape they're specified with. Whichever actions remain after filtering are
exactly what gets registered; nothing about the underlying `RouteDefinition`
objects for excluded actions ever exists.

### Inside a route group

```go
kernel.Prefix("api").Name("api.").Group(func(g *apphttp.RouteGroup) {
	g.ApiResource("posts", postController).Except([]string{"destroy"})
})
```

`RouteGroup.Resource`/`ApiResource` mirror the `Kernel` versions, scoped to
the group's prefix, name prefix, and middleware — so the routes above land
at `/api/posts`, `/api/posts/{post}`, ..., named `api.posts.index`, ...,
with the group's middleware applied in addition to whatever the controller
itself declares.

## Nested resources

```go
kernel.Resource("photos.comments", commentController)
```

A dotted name nests every generated route under the parent segment(s), each
carrying its own parameter:

```
GET    /photos/{photo}/comments             photos.comments.index
GET    /photos/{photo}/comments/create      photos.comments.create
POST   /photos/{photo}/comments             photos.comments.store
GET    /photos/{photo}/comments/{comment}   photos.comments.show
GET    /photos/{photo}/comments/{comment}/edit  photos.comments.edit
PUT/PATCH /photos/{photo}/comments/{comment}    photos.comments.update
DELETE /photos/{photo}/comments/{comment}       photos.comments.destroy
```

The route **names** are always the full dotted path (`photos.comments.*`)
— only the *URI* changes with `Shallow()`, below.

### `Shallow()`

```go
kernel.Resource("photos.comments", commentController).Shallow()
```

Promotes the 4 "member" actions — `show`, `edit`, `update`, `destroy`,
the ones addressing one specific comment by its own ID — to the
resource's own top-level URI, since a comment's ID is already globally
unique and doesn't need the parent photo's ID to disambiguate it. The
"collection" actions (`index`, `create`, `store`) stay nested, since
*those* genuinely need the parent's ID (to know which photo's comments to
list or add to):

```
GET    /photos/{photo}/comments            comments.index    (still nested)
POST   /photos/{photo}/comments            comments.store    (still nested)
GET    /comments/{comment}                 comments.show     (shallow)
GET    /comments/{comment}/edit            comments.edit     (shallow)
PUT/PATCH /comments/{comment}              comments.update   (shallow)
DELETE /comments/{comment}                 comments.destroy  (shallow)
```

Verified directly: after `Shallow()`, `GET /photos/9/comments/42` (the
*non*-shallow URL a comment's show route would otherwise live at) 404s —
it was never registered — while `GET /comments/42` returns the comment.

### `singularize`: a deliberately simple heuristic

The `{param}` name for each segment (`{photo}`, `{comment}`) comes from
singularizing the resource name segment (`"photos"` → `"photo"`), the same
thing Laravel derives via `Str::singular()`. Golite's version is a small,
documented heuristic — `"ies"` → `"y"`, `"sses"/"xes"/"ches"/"shes"` → drop
`"es"`, a trailing `"s"` (not `"ss"`) → drop it, otherwise unchanged — not
a full English inflector. It gets the overwhelming majority of resource
names right (including every plural used in this codebase's own demo
routes) but will leave an irregular plural (`"children"`) unchanged rather
than mapping it to `"child"`. There's no override mechanism for the rare
case it guesses wrong; naming your resource segment already-singular-ish
if it matters is the workaround.

## Singleton resources

```go
kernel.Singleton("profile", profileController)
```

A resource with exactly one instance — no `{id}` segment at all:

```
GET       /profile        profile.show
GET       /profile/edit   profile.edit
PUT/PATCH /profile        profile.update
```

```go
kernel.Singleton("profile", profileController).Creatable()
```

Additionally registers:

```
GET   /profile/create   profile.create
POST  /profile          profile.store
```

Singleton resources don't support dot-notation nesting in this
implementation (a reasonable, documented scope cut — Laravel's own nested
singletons are a comparatively rare pattern). Like `Resource`/`ApiResource`,
only controller methods that actually exist get registered.

## How `Only`/`Except`/`Shallow`/`Creatable` avoid leaking stale routes

Laravel's `Route::resource(...)` returns a `PendingResourceRegistration`
that defers actually registering anything until PHP's object destructor
runs — which is what lets `->only(...)`/`->except(...)`/`->shallow()` be
chained *after* the initial call and still change what gets registered. Go
has no destructor to hook into, so `ResourceRegistrar`/`SingletonRegistrar`
take a different approach: **every** fluent method — including the initial
`Resource`/`ApiResource`/`Singleton` call itself — registers the *complete*
current route set immediately, first removing whatever the previous call
registered:

```go
func (r *ResourceRegistrar) Only(actions []string) *ResourceRegistrar {
	r.only = actions
	r.except = nil
	r.register()
	return r
}

func (r *ResourceRegistrar) register() {
	r.kernel.removeRoutes(r.routes) // no-op the first time
	r.routes = /* build the current allowed set from scratch */
}
```

`Kernel.removeRoutes` deletes routes by pointer identity from both the
route table and the named-route registry. Verified directly: chaining
`.Only([...])` then `.Except([...])` on the same registrar correctly
*replaces* the first restriction rather than compounding it — actions
excluded by `Only` but not by the later `Except` come back.

Since a handful of route re-registrations only ever happens once at boot
(never per-request), the "throw it away and rebuild" approach costs
nothing that matters. The one thing worth knowing: because removed routes
are re-appended at the *end* of the route table, a resource's routes shift
later, relative to unrelated routes, every time one of these fluent
methods runs — harmless unless some other route's pattern ambiguously
overlaps one of the resource's own URIs, an unusual situation regardless of
ordering.

## Demo controllers

- **`PostController`** — full resource, DI (`Hasher`), controller-level
  middleware with `.Except`, deliberately missing `Create`/`Edit`.
- **`CommentController`** — nested + shallow resource routing.
- **`ProfileController`** — singleton resource, with `Create`/`Store` for
  the `.Creatable()` demo.
- **`ProvisionServerController`** — single-action (`Invokable`).

See [`routes/web.go`](../routes/web.go) for how each is wired up.
