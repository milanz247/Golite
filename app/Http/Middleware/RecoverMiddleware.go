package middleware

import (
	"fmt"

	exceptions "Golite/app/Exceptions"
	apphttp "Golite/app/Http"
	"Golite/logging"
)

// Recover is Golite's global panic-recovery middleware — the equivalent of
// every request in Laravel implicitly running inside the exception
// handler. Its deferred recover() sits outermost in Context.Next's
// recursive call chain (see docs/middleware.md#how-the-chain-runs--
// contextnext and app/Exceptions/exceptions.go's package doc), so it
// catches a panic from literally anywhere downstream: any other
// middleware, a controller, or a route closure — including
// *exceptions.HttpException (from exceptions.Abort/NotFound/...) and
// *validation.Exception (from Context.Validate), as well as a genuine
// programmer error.
//
// Recover must be registered first in the global middleware stack (see
// public/main.go) — anything registered before it runs outside its
// deferred recover and would still crash the connection on panic, the
// same way it did before this middleware existed.
//
// Only genuinely reportable panics are logged (see
// exceptions.ShouldReport) — an expected client-driven condition like a
// failed validation or an intentional 404 is rendered but not logged,
// mirroring Laravel's Handler::$dontReport, so the log isn't flooded with
// entries for things that aren't actually application errors.
func Recover(logger logging.Logger, debug bool) apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil && exceptions.ShouldReport(r) {
					logger.Error(fmt.Sprintf("unhandled panic: %v", r), map[string]any{
						"method": c.Request.Method,
						"path":   c.Request.URL.Path,
					})
				}
				exceptions.Render(c, r, debug)
			}
		}()
		next()
	})
}
