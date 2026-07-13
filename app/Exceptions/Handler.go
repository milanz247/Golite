package exceptions

import (
	"fmt"
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/validation"
)

// Render turns a recovered value — anything passed to panic(), most often
// an error — into a JSON error response written directly to c, mirroring
// Laravel's Handler::render. It recognizes two Golite-specific error
// types and falls back to a generic 500 for everything else:
//
//   - *HttpException (from Abort/NotFound/Forbidden/Unauthorized/
//     BadRequest): rendered with its own Status and Message.
//   - *validation.Exception (from Context.Validate): rendered as 422 with
//     a Laravel-shaped {"message": ..., "errors": {field: [...]}}} body.
//
// debug controls whether the underlying error detail is included in the
// response body (config.App.Debug — true unless APP_ENV is
// "production"/APP_DEBUG=false) — production deployments must never leak
// internals (stack state, driver errors, file paths, ...) to the client.
func Render(c *apphttp.Context, recovered any, debug bool) {
	switch e := recovered.(type) {
	case *HttpException:
		body := map[string]any{"error": e.Error()}
		if debug && e.Err != nil {
			body["debug"] = e.Err.Error()
		}
		c.JSON(e.Status, body)

	case *validation.Exception:
		c.JSON(http.StatusUnprocessableEntity, map[string]any{
			"message": e.Error(),
			"errors":  e.Errors,
		})

	case error:
		body := map[string]any{"error": "Server Error"}
		if debug {
			body["debug"] = e.Error()
		}
		c.JSON(http.StatusInternalServerError, body)

	default:
		body := map[string]any{"error": "Server Error"}
		if debug {
			body["debug"] = fmt.Sprintf("%v", recovered)
		}
		c.JSON(http.StatusInternalServerError, body)
	}
}

// ShouldReport reports whether a recovered value represents a genuine
// failure worth writing to the log, mirroring Laravel's Handler::$dontReport
// (and the underlying reportable()/Exception::report() convention):
// expected, client-driven conditions — a *validation.Exception, or an
// *HttpException below 500 (a 404, a 403, an intentional abort(418) demo,
// ...) — are not application errors, so logging every one of them at
// "error" level would just bury genuine 500s in noise. Anything else (an
// *HttpException with a 5xx status, a plain error, or a non-error panic
// value) is reportable.
func ShouldReport(recovered any) bool {
	switch e := recovered.(type) {
	case *HttpException:
		return e.Status >= http.StatusInternalServerError
	case *validation.Exception:
		return false
	default:
		return true
	}
}
