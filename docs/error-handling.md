# Error Handling

Files: [`app/Exceptions/exceptions.go`](../app/Exceptions/exceptions.go),
[`app/Exceptions/Handler.go`](../app/Exceptions/Handler.go),
[`app/Http/Middleware/RecoverMiddleware.go`](../app/Http/Middleware/RecoverMiddleware.go),
[`public/main.go`](../public/main.go) (registration)

Go has no `throw`/`catch`. Golite's `app/Exceptions` package and
`middleware.Recover` are its equivalent of Laravel's `App\Exceptions`
namespace and the exception handler every request implicitly runs
inside — built on the language's actual equivalent for "abort the
current request from deep in a call stack": `panic`/`recover`.

## Why `panic`/`recover` is the right fit here

`Context.Next` is already recursive (see
[middleware.md](middleware.md#how-the-chain-runs--contextnext)): each
middleware's `Handle` calls `next()`, which calls the next handler
directly, all the way down to the route closure, and back up again. A
regular Go `panic` unwinds through **exactly that same call chain** — so
recovering once, in the outermost middleware, catches a panic from
literally anywhere downstream: any other middleware, a controller
constructor, or the final route handler. No special support was needed
in the router or `Context` itself for this to work.

Without `Recover`, a panicking handler is instead caught by `net/http`'s
own per-connection `recover()` (inside `net/http`'s `conn.serve()`, since
`Kernel` is just an `http.Handler`), which logs a stack trace and drops
the connection with **no response body at all** — indistinguishable from
a client-side network failure.

## `HttpException` and `abort()`

```go
// app/Exceptions/exceptions.go
type HttpException struct {
	Status  int
	Message string
	Err     error // optional wrapped cause, included in responses only when debug mode is on
}

func Abort(status int, message string) *HttpException // Laravel's abort($code, $message)
func NotFound(message string) *HttpException           // 404, defaults message to "Not Found"
func Forbidden(message string) *HttpException           // 403
func Unauthorized(message string) *HttpException        // 401
func BadRequest(message string) *HttpException           // 400
```

Pair any of these with `panic` to actually short-circuit the request:

```go
kernel.GET("/posts/{post}", func(c *apphttp.Context) {
	post := findPost(c.Param("post"))
	if post == nil {
		panic(exceptions.NotFound("that post doesn't exist"))
	}
	c.JSON(http.StatusOK, post)
})
```

## `middleware.Recover` and `exceptions.Render`

```go
func Recover(logger logging.Logger, debug bool) apphttp.Middleware
```

`Recover`'s deferred `recover()` catches whatever was panicked with and
hands it to `exceptions.Render`, which maps it to a JSON response:

| Recovered value | Response |
|---|---|
| `*exceptions.HttpException` | Its own `Status` + `{"error": Message}` |
| `*validation.Exception` (from [`Context.Validate`](validation.md)) | `422` + `{"message": ..., "errors": {field: [...]}}` |
| any other `error`, or any other panic value | `500` + `{"error": "Server Error"}` |

`debug` (from `config.App.Debug` — true unless `APP_ENV=production` or
`APP_DEBUG=false`) controls whether the underlying error detail is
additionally included under `"debug"` in the response body. **Production
deployments must never leak internals** (driver errors, file paths,
raw panic values) to the client — this is exactly what `Debug` gates.

`Recover` must be registered **first** in the global middleware stack —
see `public/main.go`:

```go
app.Kernel.UseMiddleware(
	appMiddleware.Recover(app.Container.Make("log").(logging.Logger), app.Config.App.Debug),
	appMiddleware.NewTrustHosts(),
	// ... everything else
)
```

Anything registered *before* `Recover` runs outside its deferred
`recover()` and would still crash the connection on panic, since
`Recover`'s own `defer` only guards the calls it makes to `next()` and
everything `next()` in turn calls.

## What gets logged — `exceptions.ShouldReport`

Not every recovered panic is a genuine application error. A failed
validation, an intentional `404`, or a demo `abort(418)` are expected,
client-driven outcomes — logging every one of them at `"error"` level
would bury real `500`s in noise. `exceptions.ShouldReport` mirrors
Laravel's `Handler::$dontReport` convention:

```go
func ShouldReport(recovered any) bool {
	switch e := recovered.(type) {
	case *HttpException:
		return e.Status >= http.StatusInternalServerError // 4xx: not reported
	case *validation.Exception:
		return false // never reported
	default:
		return true // plain errors, unrecognized panics: reported
	}
}
```

`Recover` checks this before calling `logger.Error` (see
[logging.md](logging.md)), so `storage/logs/golite.log` only accumulates
entries for panics that were `500`-and-up, or weren't one of Golite's own
typed exceptions to begin with.

## Demo routes

`GET /errors/abort/{code}`, `GET /errors/not-found`, and `GET
/errors/boom`, handled by
[`ErrorDemoController`](../app/Http/Controllers/ErrorDemoController.go)
(`Abort`/`NotFound`/`Boom`, wired up in
[`routes/web.go`](../routes/web.go)) demonstrate an
arbitrary-status `HttpException`, the `NotFound` helper, and a plain
`error` panic falling through to the generic `500` branch, respectively —
along with `POST /register`'s validation failure (see
[validation.md](validation.md)) as the fourth recognized case.
