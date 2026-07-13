package controllers

import (
	"fmt"
	"net/http"
	"strconv"

	exceptions "Golite/app/Exceptions"
	apphttp "Golite/app/Http"
)

// ErrorDemoController demonstrates Golite's error handling (see
// docs/error-handling.md): panicking with an *exceptions.HttpException or
// a plain error, both caught by RecoverMiddleware and rendered as JSON
// from anywhere downstream of it.
type ErrorDemoController struct {
	Controller
}

// NewErrorDemoController constructs an ErrorDemoController. It takes no
// dependencies.
func NewErrorDemoController() *ErrorDemoController {
	return &ErrorDemoController{}
}

// Abort handles GET /errors/abort/{code}, panicking with an
// exceptions.HttpException built from the route's {code} parameter.
func (ec *ErrorDemoController) Abort(c *apphttp.Context) {
	code, err := strconv.Atoi(c.Param("code"))
	if err != nil {
		code = http.StatusTeapot
	}
	panic(exceptions.Abort(code, fmt.Sprintf("demo abort with status %d", code)))
}

// NotFound handles GET /errors/not-found, panicking with the NotFound
// helper (a 404 HttpException).
func (ec *ErrorDemoController) NotFound(c *apphttp.Context) {
	panic(exceptions.NotFound(""))
}

// Boom handles GET /errors/boom, panicking with a plain Go error rather
// than an HttpException — renders as a generic 500, with the underlying
// message included only when APP_DEBUG is on (see exceptions.Render).
func (ec *ErrorDemoController) Boom(c *apphttp.Context) {
	panic(fmt.Errorf("golite: something went wrong deep in a handler"))
}
