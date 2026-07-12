package middleware

import (
	"net/http"
	"strings"

	apphttp "Golite/app/Http"
)

// Role is a parameterized middleware struct — invoked on a route as e.g.
// .Middleware("role:editor,admin"), which the kernel parses into the base
// name "role" plus params ["editor", "admin"] and passes straight through
// to Handle. Being a struct (rather than a bare closure) is what lets it
// take constructor-injected dependencies — e.g. a real auth guard resolved
// from the service container in a service provider — while remaining
// resolvable directly through Golite's Service Container: see
// kernel.Container().Bind("audit", ...) in routes/web.go for the same
// pattern applied to AuditMiddleware.
type Role struct{}

// NewRole constructs a Role middleware. In a real application this would
// accept whatever dependency tells it the current user's role (a session
// store, an auth guard, ...); it takes none here to keep the example
// self-contained.
func NewRole() *Role {
	return &Role{}
}

// Handle allows the request through only if the "X-User-Role" header
// matches one of the middleware's parameters, returning 403 otherwise —
// demonstrating how middleware parameters (split from a "role:a,b" spec by
// Kernel.resolveRouteMiddleware) reach the handler.
func (m *Role) Handle(c *apphttp.Context, next func(), params ...string) {
	userRole := c.Request.Header.Get("X-User-Role")
	for _, allowed := range params {
		if strings.EqualFold(strings.TrimSpace(allowed), userRole) {
			next()
			return
		}
	}
	c.JSON(http.StatusForbidden, map[string]string{
		"error": "forbidden: requires role " + strings.Join(params, " or "),
	})
}
