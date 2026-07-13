// Package exceptions is Golite's equivalent of App\Exceptions: HTTP-aware
// error values plus the rendering logic that turns a recovered panic into
// a proper JSON response instead of a dropped connection.
//
// Go has no throw/catch, so Golite leans on the language's actual
// equivalent for "abort the current request from deep in a call stack":
// panic/recover. Context.Next is already recursive (see
// docs/middleware.md#how-the-chain-runs--contextnext), which means a
// regular Go panic unwinds through exactly the same call chain Next()
// builds — so recovering once, in the outermost middleware
// (middleware.Recover, registered first in public/main.go), catches a
// panic from literally anywhere downstream: any other middleware, a
// controller, or a route closure.
package exceptions

import "net/http"

// HttpException is an error that carries the HTTP status it should be
// rendered as — Golite's equivalent of Laravel's HttpException, what
// abort() throws under the hood.
type HttpException struct {
	Status  int
	Message string
	Err     error // optional wrapped cause, included in the response only when debug mode is on
}

// Error implements the error interface, falling back to the status's
// standard text (e.g. "Not Found") when Message is empty.
func (e *HttpException) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Status)
}

// Unwrap exposes Err to errors.Is/errors.As.
func (e *HttpException) Unwrap() error {
	return e.Err
}

// Abort builds an HttpException for the given status and message —
// Golite's equivalent of Laravel's abort($code, $message). Pair with
// panic to actually short-circuit the request:
//
//	panic(exceptions.Abort(http.StatusTeapot, "no coffee today"))
func Abort(status int, message string) *HttpException {
	return &HttpException{Status: status, Message: message}
}

// NotFound builds a 404 HttpException, defaulting Message to "Not Found".
func NotFound(message string) *HttpException {
	return abortWithDefault(http.StatusNotFound, message)
}

// Forbidden builds a 403 HttpException, defaulting Message to "Forbidden".
func Forbidden(message string) *HttpException {
	return abortWithDefault(http.StatusForbidden, message)
}

// Unauthorized builds a 401 HttpException, defaulting Message to
// "Unauthorized".
func Unauthorized(message string) *HttpException {
	return abortWithDefault(http.StatusUnauthorized, message)
}

// BadRequest builds a 400 HttpException, defaulting Message to "Bad
// Request".
func BadRequest(message string) *HttpException {
	return abortWithDefault(http.StatusBadRequest, message)
}

func abortWithDefault(status int, message string) *HttpException {
	if message == "" {
		message = http.StatusText(status)
	}
	return Abort(status, message)
}
