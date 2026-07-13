# HTTP Response Handling

Files: [`app/Http/Response.go`](../app/Http/Response.go),
[`app/Http/Context.go`](../app/Http/Context.go),
[`app/Http/Kernel.go`](../app/Http/Kernel.go),
[`app/Providers/AppServiceProvider.go`](../app/Providers/AppServiceProvider.go)

Golite's response layer mirrors Laravel's in full: handlers can optionally
return a value instead of writing the response themselves, a fluent
`*Response` factory (`c.Response(...)`) covers status/headers/cookies/JSON/
views/downloads/streaming, redirects carry one-shot flash data, and a
global macro registry lets you register reusable custom responses.

## Dynamic return-type serialization

```go
type ResponderFunc func(c *Context) any

func Responder(fn ResponderFunc) HandlerFunc
```

`HandlerFunc` (`func(*Context)`, no return) is unchanged and still works
exactly as it always has — it's used throughout the framework and by
every existing controller, and changing its signature would have forced
every one of them to add a bare `return nil`. `ResponderFunc` is a
**separate**, opt-in type: wrap a handler that returns a value with
`Responder` to register it like any other route:

```go
kernel.GET("/greeting", apphttp.Responder(func(c *apphttp.Context) any {
	return "Hello!" // -> text/html; charset=utf-8, 200
}))

kernel.GET("/users", apphttp.Responder(func(c *apphttp.Context) any {
	return []User{...} // -> application/json, 200
}))
```

The conversion rules (`writeAutoResponse`):

| Return value | Result |
|---|---|
| `nil` | Nothing — the handler already wrote its own response (or intentionally wrote nothing) |
| `*Response` | Sent via `Response.Send` (see below) |
| `string` | Written as-is, `Content-Type: text/html; charset=utf-8`, 200 |
| anything else (struct, map, slice, array, ...) | `json.Marshal`ed, `Content-Type: application/json`, 200 |

## The fluent `Response` factory

```go
func (c *Context) Response(content any, status ...int) *Response
```

Starts a `*Response` — the same auto-conversion rules above apply to
`content` unless a specialized method (`Json`/`View`/`Download`/`File`/
`StreamDownload`) is chained afterward, and `status` defaults to 200.
Every method returns the same `*Response` for chaining:

```go
c.Response(map[string]string{"status": "created"}, http.StatusCreated).
	Header("X-Powered-By", "Golite").
	WithHeaders(map[string]string{"X-Request-Id": "demo-123"}).
	Cookie("last_visit", time.Now().Format(time.RFC3339), 60).
	WithoutCookie("stale_session")
```

- **`Status(code int)`** — override the response's HTTP status.
- **`Header(key, value string)`** / **`WithHeaders(map[string]string)`** —
  set one or several response headers.
- **`Cookie(name, value string, minutes int)`** — queue a cookie,
  encrypted with the **same AES-256-GCM primitive** `Context.SetCookie`
  uses (see [http-requests.md](http-requests.md#encrypted-authenticated-cookies)),
  so a cookie set either way is readable via `Context.Cookie` either way.
  `minutes` is the lifetime; `0` makes it a session cookie.
- **`WithoutCookie(name string)`** — queue a same-named cookie with an
  already-expired `MaxAge`, which browsers delete immediately.

### Delivering a `*Response`

Two ways, both fully supported:

1. **Return it** from a `Responder`-wrapped handler — the recommended,
   Laravel-flavored style, and the only way the top-level auto-conversion
   table above applies:

   ```go
   kernel.GET("/x", apphttp.Responder(func(c *apphttp.Context) any {
   	return c.Response(...).Json(...)
   }))
   ```

2. **Call `.Send(c)` directly** from a plain `HandlerFunc` — useful when a
   route only *sometimes* wants the fluent API and otherwise writes
   directly (see `/contact`'s validation-failure branch, which redirects
   with flashed input, versus its success branch, which still calls the
   older `c.JSON` directly):

   ```go
   kernel.POST("/x", func(c *apphttp.Context) {
   	if !c.Has("email") {
   		c.Redirect("/x", http.StatusFound).WithInput().Send(c)
   		return
   	}
   	c.JSON(http.StatusOK, map[string]string{"status": "ok"})
   })
   ```

`Response` is created fresh per request and never shared or cached, so —
unlike `Session` or the middleware registries — it needs no locking.

## Redirects, with flash data

```go
func (c *Context) Redirect(to string, status ...int) *Response // default 302
func (c *Context) Back() *Response                              // Referer header, or "/"
func (c *Context) Away(url string) *Response                    // external URL
```

`Back` reads the `Referer` header and redirects there (falling back to
`"/"` if absent). `Away` behaves identically to `Redirect` in Golite:
Laravel's version exists to bypass Laravel's URL generator, which would
otherwise try to resolve a relative-looking path against the app's own
domain — Golite's `Redirect` never does that kind of local resolution in
the first place (it just sets the `Location` header to whatever string
it's given, like `http.Redirect` itself does), so there's no behavioral
difference to preserve. `Away` is kept as a distinct, explicitly-named
method purely for API parity and to make the caller's intent
self-documenting.

```go
func (r *Response) With(key string, value any) *Response // one flash message
func (r *Response) WithInput() *Response                  // flash the current input
```

Both are only meaningful on a redirect response — `Response.Send`'s
`kindRedirect` branch applies them (writing into the session, via
`Session.Flash`) **before** calling `http.Redirect`. This needs a session
already attached (`.Middleware("session")` on the route, directly or via a
group), and matters for the same Go-specific reason
[`StartSessionMiddleware` queues its cookie before `next()`](sessions.md#a-go-specific-cookie-ordering-fix-the-same-class-of-bug-csrf-hit)
and [`VerifyCsrfToken` sets its cookie before calling `next()`](security-csrf.md#the-xsrf-token-cookie-and-a-go-specific-ordering-fix):
`http.Redirect` calls `WriteHeader`, so anything that needs to land in the
session has to happen before it runs, not after.

`With` reuses the **same** one-shot flash mechanism as `Context.Flash`/
`Old` (`Session.Flash`/`Get` — see
[sessions.md](sessions.md#flash-data)) — a message flashed via
`.With("message", "Success!")` is read back with `c.Old("message")`,
exactly like flashed form input. This mirrors how Laravel actually
implements both on the same underlying session flash bucket; the practical
consequence is that a `.With("email", ...)` message and a form field
literally named `email` share the same flash key — matching Laravel's own
behavior, not a Golite-specific quirk. Verified directly: a message flashed
via `.With` is readable via `Old` on the *next* request and gone by the one
after that.

## Specialized response formats

```go
func (r *Response) Json(data any) *Response
```

Forces JSON encoding regardless of `data`'s type — including a `string`,
which the default auto-conversion would otherwise send as `text/html`.

```go
func (r *Response) View(templateName string, data map[string]any) *Response
```

Renders `ViewsDirectory/templateName.html` (default
`ViewsDirectory = "resources/views"`) with Go's native `html/template`,
setting `text/html; charset=utf-8`. Templates are **parsed once per name
and cached** (`parseView`, guarded by a `sync.RWMutex`) — not re-read from
disk on every request. There's no cache invalidation, matching the
"restart to pick up changes" tradeoff already made for sessions and the
cookie encryption key (see
[architecture.md](architecture.md#design-decisions-worth-knowing)).

```go
c.Response(nil).View("welcome", map[string]any{"Name": "World"})
```

loads `resources/views/welcome.html`:

```html
<h1>Hello, {{.Name}}!</h1>
```

```go
func (r *Response) Download(filePath string, filename ...string) *Response
func (r *Response) File(filePath string) *Response
```

Both serve a file via `http.ServeFile` (which handles range requests,
conditional GETs, and Content-Type detection for you). `Download` also
sets `Content-Disposition: attachment; filename="..."`, forcing a save-as
dialog; `File` doesn't, so a browser displays a compatible type (an image,
a PDF) inline instead. `filename` (for `Download`) is what the browser
shows the user — it doesn't need to match `filePath`'s real name — and
defaults to `filepath.Base(filePath)`.

> **Status codes don't apply cleanly to `Download`/`File`.** `http.ServeFile`
> decides the final status itself (200, 304 Not Modified, 416 Range Not
> Satisfiable, ...), so a `.Status(code)` chained before `Download`/`File`
> has no effect on it — headers set via `.Header()`/`.WithHeaders()`
> still apply, since they're set before `ServeFile` runs. Laravel's
> `response()->download()` has the same limitation.

> **Filenames aren't sanitized for path traversal.** `filePath` is
> expected to be a trusted, server-controlled path — exactly like
> Laravel's `response()->download($path)` — not derived directly from
> client input. `sanitizeFilename` *does* strip quote/CRLF characters from
> the `filename` shown to the user (the `Content-Disposition` value),
> since header-injection via a crafted filename is a more realistic risk
> than a well-known one that's the caller's job to avoid.

```go
func (r *Response) StreamDownload(callback func(w io.Writer), filename string) *Response
```

Streams `callback`'s output straight to the client as a download named
`filename`, with **no temporary file ever written to disk** — the
difference from `Download`, which needs a real file to already exist.
Useful for a dynamically generated report, export, or archive:

```go
c.Response(nil).StreamDownload(func(w io.Writer) {
	fmt.Fprintf(w, "Report generated at %s\n", time.Now().Format(time.RFC3339))
}, "report.txt")
```

## Response macros

```go
var ResponseFactory = &macroRegistry{...}

func (f *macroRegistry) Macro(name string, fn any)
func (f *macroRegistry) Call(name string, args ...any) (*Response, error)
func (c *Context) Macro(name string, args ...any) *Response
```

Laravel's macros work via PHP's `__callStatic` magic method, letting you
call a registered macro as if it were a native method
(`Response::caps($val)`). Go has no equivalent dynamic dispatch, so
`ResponseFactory.Call` — and the `Context.Macro` convenience that wraps it
— invoke a registered macro **by name, via `reflect`**, checking the
argument count and that it returns exactly one `*Response` at call time.
This is also what makes registering *any* function signature possible
(not just a fixed one) — a macro isn't required to take a single string
argument; whatever parameters it declares are what `Context.Macro` must be
called with.

Register once, typically from a service provider:

```go
// app/Providers/AppServiceProvider.go
apphttp.ResponseFactory.Macro("caps", func(val string) *apphttp.Response {
	return apphttp.NewResponse(strings.ToUpper(val))
})
```

`NewResponse` is the package-level equivalent of `Context.Response` — for
code (like a macro) that doesn't have a `*Context` on hand. It's not named
`Response` because Go doesn't allow a function and a type to share an
identifier in the same package; the same reason the registered-route type
is `RouteDefinition`, not `Route` (see
[architecture.md](architecture.md#design-decisions-worth-knowing)).

Invoke it anywhere a `Context` is available:

```go
kernel.GET("/shout", apphttp.Responder(func(c *apphttp.Context) any {
	return c.Macro("caps", "hello from a macro")
}))
```

`Context.Macro` **panics** if the macro doesn't exist, isn't a function,
takes the wrong number of arguments, or doesn't return exactly one
`*Response` — these are all registration/call-site bugs, discoverable
during development, not conditions that depend on request input, so
failing loudly rather than degrading gracefully is the appropriate
response (the same reasoning behind several other panics elsewhere in the
codebase — an invalid `Where` regex, a duplicate route parameter name).

## Where `Kernel.go` actually changed

Almost nowhere — `Responder` is a self-contained wrapper that produces a
plain `HandlerFunc`, so `dispatch`/`ServeHTTP`/`addRoute`/route-registration
are all completely unchanged. The one real edit: `Kernel.Redirect`
(`Route::redirect`) had to follow `Context.Redirect`'s new signature and
explicitly call `.Send(c)`, since its own handler is a plain `HandlerFunc`
rather than one wrapped in `Responder`:

```go
func (k *Kernel) Redirect(from, to string, status int) *RouteDefinition {
	if status == 0 {
		status = http.StatusFound
	}
	return k.addRoute(allMethods, from, func(c *Context) {
		c.Redirect(to, status).Send(c)
	}, "", "", nil)
}
```

See [routing.md](routing.md#redirects) for `Route::redirect` itself.
