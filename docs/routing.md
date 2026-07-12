# Routing

File: [`app/Http/Kernel.go`](../app/Http/Kernel.go), demonstrated end to end
in [`routes/web.go`](../routes/web.go).

Golite's router is a native regex-based matcher (no external routing
library) that mirrors Laravel's `Route` facade: HTTP verb helpers, required
and optional parameters, regex constraints, named routes with URL
generation, nested groups, a redirect shortcut, and a fallback route.

## HTTP verb helpers

```go
kernel.GET("/user", handler)
kernel.POST("/posts", handler)
kernel.PUT("/posts/{post}", handler)
kernel.PATCH("/posts/{post}", handler)
kernel.DELETE("/posts/{post}", handler)
kernel.OPTIONS("/posts", handler)
```

Plus the dynamic/multi-method equivalents of `Route::match` and
`Route::any`:

```go
kernel.Match([]string{http.MethodGet, http.MethodPost}, "/posts/search", handler)
kernel.Any("/ping", handler) // GET, POST, PUT, PATCH, DELETE, OPTIONS
```

Every one of these returns a `*apphttp.RouteDefinition`, so constraints,
naming, and per-route middleware can be chained directly off the
registration call (see below).

Internally, all of them funnel through a single `Kernel.addRoute(methods,
uri, handler, prefix, namePrefix, groupMiddleware)`, which is also what
`RouteGroup`'s verb methods call — there's exactly one code path that
parses a URI, compiles its matcher, and appends it to the route table.

## Route parameters

URIs can contain `{param}` (required) and `{param?}` (optional) segments:

```go
kernel.GET("/posts/{post}/comments/{comment?}", handler)
```

Each segment is parsed into a `routeSegment` (static text, or a named,
possibly-optional parameter) and compiled into a single anchored regular
expression per route, using Go named capture groups — e.g. `/posts/{post}`
compiles to `^/posts/(?P<post>[^/]+)$`, and a trailing optional parameter
wraps its own `/segment` pair in a non-capturing optional group:
`^/posts(?:/(?P<post>[^/]+))?$`.

Inside a handler, resolved parameters are available via `Context`:

```go
func (c *apphttp.Context) {
	id := c.Param("post")        // a single parameter
	all := c.Params()            // every resolved parameter, as a copy
}
```

### Default values for optional parameters

```go
kernel.GET("/greet/{name?}", func(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]string{"greeting": "Hello, " + c.Param("name")})
}).Defaults(map[string]string{"name": "Guest"})
```

`GET /greet` → `"Hello, Guest"`; `GET /greet/Milan` → `"Hello, Milan"`.
`Defaults` fills in a value whenever the matched optional segment was
absent — both when reading `c.Param`/`c.Params()` in a handler and when
generating a URL for the route (see below).

> **Limitation:** as in Laravel, optional parameters should be the
> trailing segment(s) of a route. An optional segment followed by a
> required one is ambiguous and not specially handled.

## Regex constraints (`where*`)

```go
kernel.GET("/articles/{id}", handler).Where("id", `[0-9]+`)

kernel.GET("/products/{category}/{sku}", handler).WhereMap(map[string]string{
	"category": "[a-z-]+",
	"sku":      "[A-Z0-9]+",
})

kernel.GET("/users/{id}", handler).WhereNumber("id")           // [0-9]+
kernel.GET("/authors/{name}", handler).WhereAlpha("name")       // [A-Za-z]+
kernel.GET("/tags/{slug}", handler).WhereAlphaNumeric("slug")   // [A-Za-z0-9]+
kernel.GET("/categories/{c}", handler).WhereIn("c", []string{"news", "sports", "tech"})
```

Constraints aren't a separate validation step — `Where`/`WhereMap` rebuild
the route's compiled regex (via `recompile()`) with the constraint pattern
substituted directly into that parameter's capture group. A request whose
value doesn't satisfy the constraint therefore just **doesn't match the
regex**, which means the router treats it exactly like any other
non-matching route: it falls through to try the next route, and if nothing
else matches, the fallback/404 path runs. There's no special "constraint
failed" branch to maintain — 404-on-violation falls out of how matching
already works.

## Named routes and URL generation

```go
kernel.GET("/user/{id}/profile", handler).Name("profile")

kernel.Route("profile", map[string]any{"id": 1})       // "/user/1/profile"
apphttp.Route("profile", map[string]any{"id": 1})      // same, via the global helper
```

`Name` registers the route (by its fully-qualified name — see group name
prefixes below) into the kernel's `namedRoutes` map. Two ways to generate a
URL from a name:

- **`Kernel.Route(name, params)`** — the natural choice when you already
  have a `*Kernel` (e.g. inside a handler via a closure, or wherever the
  application wires things up).
- **`apphttp.Route(name, params)`** — a package-level function mirroring
  Laravel's global `route()` helper. It operates on whichever `*Kernel` was
  most recently constructed via `NewKernel` (tracked in a package-level
  variable set from `NewKernel`), which is fine for Golite's single-kernel
  application model.

Both return `""` if the name is unknown. Missing an optional parameter
omits that segment (and falls back to `Defaults`, if set, before doing so);
missing a *required* parameter renders a visible `{name}` placeholder
rather than silently producing a broken URL, since a lightweight framework
has no request context to raise a proper exception from.

## Route groups

```go
kernel.Prefix("admin").Middleware("auth").Name("admin.").Group(func(admin *apphttp.RouteGroup) {
	admin.GET("/users", handler).Name("users") // -> GET /admin/users, named "admin.users"

	admin.Prefix("posts").Name("posts.").Group(func(posts *apphttp.RouteGroup) {
		posts.GET("", handler).Name("index")                          // GET /admin/posts, "admin.posts.index"
		posts.GET("/{post}", handler).WhereNumber("post").Name("show") // GET /admin/posts/{post}, "admin.posts.show"
	})
})
```

`Kernel.Prefix`/`Middleware`/`Name` each start a fresh `*RouteGroup`.
`RouteGroup.Prefix`/`Middleware`/`Name` are **non-mutating** — every call
returns a *new* `RouteGroup` with the attribute merged in (prefix segments
joined with `/`, middleware appended, name prefixes concatenated). That
means a group can be reused as the base for several sibling sub-groups
without them contaminating each other, and nesting (as above, `posts`
built from `admin`) naturally inherits and extends the parent's prefix,
name prefix, and middleware list.

`RouteGroup` exposes the same verb helpers as `Kernel` (`GET`, `POST`,
`PUT`, `PATCH`, `DELETE`, `OPTIONS`, `Match`, `Any`), so route registration
inside a group closure reads identically to top-level registration — the
group's prefix/name-prefix/middleware are just applied automatically.

## Middleware on a route or group

A route can also take middleware directly, in addition to whatever its
group(s) contributed — as a single name, several names, a parameterized
spec, or a `[]string`:

```go
kernel.GET("/admin/reports", handler).Middleware("auth", "role:editor,admin")
kernel.GET("/dashboard", handler).Middleware([]string{"web", "auth"})
```

Middleware here is referenced **by name** (a string, optionally
`"name:param1,param2"`), not a direct value — see
[middleware.md](middleware.md#the-three-registries) for how names resolve
to actual middleware (aliases, groups, or the service container),
[middleware.md#middleware-parameters](middleware.md#middleware-parameters)
for how `"role:editor,admin"`-style params reach the middleware, and
[middleware.md#middleware-priority](middleware.md#middleware-priority) for
the order they end up running in regardless of how they were assigned.

A route can also opt out of a middleware its group would otherwise
contribute:

```go
kernel.Prefix("admin").Middleware("web").Group(func(admin *apphttp.RouteGroup) {
	admin.GET("/health", handler).WithoutMiddleware("audit")
})
```

See [middleware.md#excluding-middleware--withoutmiddleware](middleware.md#excluding-middleware--withoutmiddleware).

## Redirects

```go
kernel.Redirect("/home", "/user", http.StatusFound)
```

Equivalent to `Route::redirect($from, $to, $status)`. Internally this is
just `addRoute(allMethods, from, handler)` where `handler` calls
`Context.Redirect`, so it responds to every common HTTP method, and the
status defaults to `302 Found` (`http.StatusFound`) if `0` is passed.

## Fallback route

```go
kernel.Fallback(func(c *apphttp.Context) {
	c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
})
```

Equivalent to `Route::fallback($action)`. It's stored separately from the
route table (`Kernel.fallback`) and only invoked by `Kernel.dispatch` when
no route matched at all. Because it's invoked from inside `dispatch` — the
handler that's always appended last to the global-middleware chain — every
global middleware (logging, method spoofing, ...) still runs first, exactly
as the requirement calls for.

## 405 vs 404

If a request's path matches a route's pattern but not its method (e.g.
`DELETE /user`, where `/user` is only registered for `GET`), Golite
responds **405 Method Not Allowed** with an `Allow` header listing the
methods that *do* match that path — mirroring Laravel's
`MethodNotAllowedHttpException`, rather than silently falling through to a
generic 404. Only a path that matches *no* route at all reaches the
fallback/404 path. See `Kernel.match` for the two-pass logic (method+path
match first, then a path-only pass to collect `Allow` values).

## `routes/web.go`

[`routes/web.go`](../routes/web.go)'s `MapWebRoutes(kernel *apphttp.Kernel)`
demonstrates every feature above in one place — verbs, `Match`/`Any`,
optional params with defaults, every `where*` helper, nested groups, a
redirect, and a fallback — and is called from `RouteServiceProvider.Boot`
(see [service-providers.md](service-providers.md)).

See [middleware.md](middleware.md) for how the middleware pipeline itself
works, [request-lifecycle.md](request-lifecycle.md) for exactly when
route matching happens relative to global middleware, and
[controllers.md](controllers.md) for `Route::resource`/`apiResource`/
`singleton` — a layer built entirely on top of the primitives described
here (`Kernel.addRoute`, `RouteDefinition.Name`/`Middleware`), registering
several routes at once from a controller instead of one call per route.
