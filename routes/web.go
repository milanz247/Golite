package routes

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/app/Http/Controllers"
	"Golite/app/Http/Middleware"
	"Golite/auth"
	"Golite/encryption"
)

// MapWebRoutes registers Golite's routes onto the kernel. This build's
// route table is intentionally minimal: session + CSRF plumbing, and the
// full authentication flow — register/login/logout, "remember me", and
// password reset (see docs/authentication.md). Earlier turns of this
// project explored the full breadth of the framework's other features
// (routing, middleware, resource controllers, responses, encryption/
// hashing/validation/logging demos, ...) via a much larger example route
// table; that history is preserved in git even though this file no
// longer carries it, and each feature's own doc under docs/ still shows
// worked examples independent of routes/web.go.
func MapWebRoutes(kernel *apphttp.Kernel) {
	kernel.MiddlewarePriority = []string{"session", "csrf", "auth"}

	kernel.AliasMiddleware("session", middleware.NewStartSession(kernel.Sessions()))
	kernel.AliasMiddleware("csrf", middleware.NewVerifyCsrfToken())

	// A CSRF-protected POST (register/login/...) needs a session-bound
	// token to send back first — a client hits this to get one, the same
	// pattern the framework's earlier CSRF demo routes used.
	kernel.GET("/csrf-token", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
	}).Middleware("session").Name("csrf.token")

	registerAuthRoutes(kernel)

	kernel.Fallback(func(c *apphttp.Context) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	})
}

// registerAuthRoutes wires up AuthController and the "auth" middleware —
// but only if the database actually connected. DatabaseServiceProvider
// and AuthServiceProvider both degrade gracefully (a logged warning, no
// panic) rather than failing boot when MySQL isn't configured — see
// docs/database.md and docs/authentication.md. Registering these routes
// unconditionally would mean a fresh clone with no MySQL running
// couldn't even start the server; skipping them instead means every
// other route still works, and hitting an auth route just 404s (via the
// fallback route above) until a database is configured.
func registerAuthRoutes(kernel *apphttp.Kernel) {
	guard, ok := kernel.Container().Make("auth").(*auth.Guard)
	if !ok {
		return
	}
	encrypter, ok := kernel.Container().Make("encrypter").(*encryption.Encrypter)
	if !ok {
		return
	}

	kernel.AliasMiddleware("auth", middleware.NewAuth(guard, encrypter))

	authController := controllers.NewAuthController()
	inject := func(handler any) apphttp.HandlerFunc {
		return apphttp.Inject(kernel.Container(), handler)
	}

	kernel.Middleware("session", "csrf").Group(func(g *apphttp.RouteGroup) {
		g.POST("/register", inject(authController.Register)).Name("auth.register")
		g.POST("/login", inject(authController.Login)).Name("auth.login")
		g.POST("/logout", inject(authController.Logout)).Middleware("auth").Name("auth.logout")
		g.POST("/forgot-password", inject(authController.ForgotPassword)).Name("auth.forgot-password")
		g.POST("/reset-password", inject(authController.ResetPassword)).Name("auth.reset-password")
	})

	kernel.GET("/me", inject(authController.Me)).Middleware("session", "auth").Name("auth.me")
}
