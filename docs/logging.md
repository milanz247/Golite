# Logging

Files: [`logging/logger.go`](../logging/logger.go),
[`logging/single_channel.go`](../logging/single_channel.go),
[`logging/daily_channel.go`](../logging/daily_channel.go),
[`logging/stack_channel.go`](../logging/stack_channel.go),
[`logging/manager.go`](../logging/manager.go),
[`app/Providers/AppServiceProvider.go`](../app/Providers/AppServiceProvider.go)
(the `"log"` binding)

Golite's `logging` package is the equivalent of `Illuminate\Log` —
PSR-3-leveled, driver-based logging, with the same registry shape as
[`app/Http/Session/SessionManager`](sessions.md) and
[`hashing.Manager`](hashing.md): named channels, a configurable default,
and `Extend` for registering more.

This is distinct from [`LoggerMiddleware`](middleware.md)
(`app/Http/Middleware/LoggerMiddleware.go`), which is a small, unrelated
"after"-style demo middleware that writes one plain-text access-log line
per request via the standard library's `log` package. `logging.Manager`
is the general-purpose, structured logging service application code
reaches for — including `RecoverMiddleware`, see
[error-handling.md](error-handling.md#what-gets-logged--exceptionsshouldreport).

## Levels and `Logger`

```go
// logging/logger.go
type Level int
const (
	Debug Level = iota
	Info
	Notice
	Warning
	Error
	Critical
	Alert
	Emergency
)

type Logger interface {
	Log(level Level, message string, context ...map[string]any)
	Debug(message string, context ...map[string]any)
	Info(message string, context ...map[string]any)
	// Notice/Warning/Error/Critical/Alert/Emergency, same shape
}
```

Eight severities, the same set (and order) Laravel's `Log` facade
exposes. `context` is optional, structured key/value data appended to the
log line as JSON.

## Channels

| Channel | Storage | Notes |
|---|---|---|
| `"single"` (`SingleChannel`) | One file, appended to forever | The framework's default (`LOG_CHANNEL=single`) |
| `"daily"` (`DailyChannel`) | One date-suffixed file per day, e.g. `golite-2026-07-13.log` | Prunes files older than `LOG_DAILY_DAYS` (default 14; `0` keeps them all) |
| `"stack"` (`StackChannel`) | Fans one entry out to multiple channels | Write to more than one destination per call |

All three implement:

```go
type Channel interface {
	Write(entry Entry) error
}
```

so a custom channel (Sentry, a Slack webhook, syslog, ...) is just another
`Write` implementation, registered the same way as the built-ins.

## `Manager` — the `"log"` container binding

```go
func NewManager(defaultChannel string) *Manager
func (m *Manager) Extend(name string, ch Channel)
func (m *Manager) Channel(name string) Logger // Log::channel($name)-equivalent — a *specific* channel
```

`Manager` itself implements `Logger` by delegating to its default
channel, so most code never needs `.Channel(...)` explicitly.
`AppServiceProvider.Register` wires it up from config:

```go
logger := logging.NewManager(cfg.Log.Channel) // LOG_CHANNEL, default "single"
logger.Extend("single", logging.NewSingleChannel(cfg.Log.Path))       // LOG_PATH, default storage/logs/golite.log
logger.Extend("daily", logging.NewDailyChannel(logDir, "golite", cfg.Log.Days)) // LOG_DAILY_DAYS, default 14
logger.Extend("stack", logging.NewStackChannel(logging.NewSingleChannel(cfg.Log.Path)))
c.Bind("log", logger)
```

`storage/` is git-ignored (see `.gitignore`), so `storage/logs/` is
created on first write and never committed — the same pattern
`storage/app/` and `storage/avatars/` already follow elsewhere in the
demo routes.

## Usage

```go
logger := c.App.Make("log").(logging.Logger)
logger.Info("user registered", map[string]any{"email": user.Email})
logger.Warning("rate limit approaching")
logger.Error("payment failed", map[string]any{"order_id": order.ID})

// A specific channel, bypassing the configured default:
logger.(*logging.Manager).Channel("daily").Critical("disk usage above 90%")
```

Output (`"single"`, the default):

```
[2026-07-13 20:50:02] golite.info: user registered {"email":"jane@example.com"}
[2026-07-13 20:50:02] golite.warning: rate limit approaching
[2026-07-13 20:50:02] golite.error: payment failed {"order_id":"1234"}
```

## Demo route

`GET /logs/demo`, handled by
[`LogController.Demo`](../app/Http/Controllers/LogController.go)
(constructor-injected with the container's `logging.Logger` — wired up in
[`routes/web.go`](../routes/web.go)), writes an info, a warning, and an
error entry, then points at `storage/logs/golite.log` in its response.
