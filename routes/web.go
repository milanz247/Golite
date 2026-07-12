package routes

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/app/Http/Controllers"
	"Golite/app/Http/Middleware"
)

// MapWebRoutes registers the application's web routes onto the kernel,
// mirroring routes/web.php. It demonstrates every routing and middleware
// feature the kernel supports: HTTP verb helpers, Match/Any, required and
// optional parameters with default values, regex constraints, named routes
// with URL generation, nested groups (prefix + middleware + name prefix), a
// redirect, a fallback route, middleware priority, middleware groups,
// parameterized middleware ("role:editor,admin"), excluding a group's
// middleware on one route (WithoutMiddleware), a middleware resolved
// straight from the service container, and CSRF protection.
func MapWebRoutes(kernel *apphttp.Kernel) {
	userController := controllers.NewUserController()

	// --- Middleware priority: regardless of the order middleware is
	// assigned on a route or pulled in via a group, it always executes in
	// this order (anything not listed here runs last, in registration
	// order) — equivalent to Laravel's $middlewarePriority. CSRF runs
	// first, mirroring Laravel's own ordering (session/CSRF concerns ahead
	// of anything that depends on them). ---
	kernel.MiddlewarePriority = []string{"csrf", "auth", "role", "audit"}

	// --- CSRF protection: NewKernel already seeds the "web" middleware
	// group with the name "csrf" (see Kernel.go), but that name only does
	// something once it's aliased to a real implementation here. "/stripe/*"
	// and "/api/v1/webhooks" are exempt — third-party services can't supply
	// a session-bound token, so they're excluded by path instead. ---
	kernel.AliasMiddleware("csrf", middleware.NewVerifyCsrfToken("/stripe/*", "/api/v1/webhooks"))

	// --- Route middleware: name -> Middleware, equivalent to Laravel's
	// $routeMiddleware / Route::aliasMiddleware(). "auth" is a plain
	// closure adapted via MiddlewareFunc; "role" is a parameterized
	// middleware struct (see app/Http/Middleware/RoleMiddleware.go). ---
	kernel.AliasMiddleware("auth", apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		if c.Request.Header.Get("Authorization") == "" {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next()
	}))
	kernel.AliasMiddleware("role", middleware.NewRole())

	// A middleware struct built independently of the Kernel (so it could
	// take injected dependencies via its constructor) and bound straight
	// into the service container instead of the RouteMiddleware map —
	// Kernel.resolveRouteMiddleware falls back to the container by name, so
	// "audit" below is resolved directly through Golite's IoC container.
	kernel.Container().Bind("audit", middleware.NewAudit())

	// --- Middleware groups: name -> ordered list of other middleware
	// names, equivalent to Laravel's $middlewareGroups. Referencing "web"
	// on a route expands to ["auth", "audit"]. ---
	kernel.MiddlewareGroup("web", "auth", "audit")

	// --- HTTP verb helpers: Route::get/post/put/patch/delete/options ---
	kernel.GET("/user", userController.Show).Name("user.show")
	kernel.POST("/posts", resourceHandler("posts.store")).Name("posts.store")
	kernel.PUT("/posts/{post}", resourceHandler("posts.update")).WhereNumber("post").Name("posts.update")
	kernel.PATCH("/posts/{post}", resourceHandler("posts.patch")).WhereNumber("post").Name("posts.patch")
	kernel.DELETE("/posts/{post}", resourceHandler("posts.destroy")).WhereNumber("post").Name("posts.destroy")
	kernel.OPTIONS("/posts", resourceHandler("posts.options"))

	// --- Route::match and Route::any ---
	kernel.Match([]string{http.MethodGet, http.MethodPost}, "/posts/search", resourceHandler("posts.search")).
		Name("posts.search")
	kernel.Any("/ping", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}).Name("ping")

	// --- Required + optional parameters, with a default fallback value for
	// the optional segment (Route::get('greet/{name?}')->defaults(...)) ---
	kernel.GET("/greet/{name?}", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{"greeting": "Hello, " + c.Param("name")})
	}).Defaults(map[string]string{"name": "Guest"}).Name("greet")

	// --- Regex constraints: ->where and the fluent helpers. A request
	// whose parameters fail these constraints simply doesn't match the
	// route (falling through to a 404), exactly like Laravel. ---
	kernel.GET("/articles/{id}", resourceHandler("articles.show")).
		Where("id", `[0-9]+`).
		Name("articles.show")

	kernel.GET("/categories/{category}", resourceHandler("categories.index")).
		WhereIn("category", []string{"news", "sports", "tech"}).
		Name("categories.index")

	// --- Route groups + middleware groups: .Middleware("web") expands
	// (via MiddlewareGroups) to ["auth", "audit"]. Equivalent to:
	//   Route::middleware(["web"])->group(function () {
	//       Route::get("/dashboard", ...)->name("dashboard");
	//   });
	kernel.Middleware("web").Group(func(g *apphttp.RouteGroup) {
		g.GET("/dashboard", resourceHandler("dashboard")).Name("dashboard")
	})

	// --- Parameterized middleware: "role:editor,admin" is parsed into base
	// name "role" and params ["editor", "admin"], which reach Role.Handle. ---
	kernel.GET("/posts/{post}/edit", resourceHandler("posts.edit")).
		WhereNumber("post").
		Middleware("auth", "role:editor,admin").
		Name("posts.edit")

	// --- Nested groups, a slice-form Middleware() call, and
	// WithoutMiddleware excluding a group-contributed middleware on one
	// specific route. Equivalent to:
	//   Route::prefix("admin")->middleware(["web", "role:admin"])->name("admin.")->group(function () {
	//       Route::get("/users", ...)->name("users");
	//       Route::get("/health", ...)->withoutMiddleware("audit")->name("health");
	//   });
	kernel.Prefix("admin").Middleware([]string{"web", "role:admin"}).Name("admin.").Group(func(admin *apphttp.RouteGroup) {
		admin.GET("/users", userController.Show).Name("users") // GET /admin/users, named "admin.users"

		// This route sits inside "admin" (which pulls in "web" ->
		// auth+audit, plus "role:admin"), but opts out of auditing
		// specifically for this one endpoint — e.g. a lightweight health
		// check that shouldn't be written to the audit trail.
		admin.GET("/health", func(c *apphttp.Context) {
			c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		}).WithoutMiddleware("audit").Name("health")

		// Nested groups inherit and extend the parent's prefix, name
		// prefix, and middleware.
		admin.Prefix("posts").Name("posts.").Group(func(posts *apphttp.RouteGroup) {
			posts.GET("", resourceHandler("admin.posts.index")).Name("index")                          // GET /admin/posts
			posts.GET("/{post}", resourceHandler("admin.posts.show")).WhereNumber("post").Name("show") // GET /admin/posts/{post}
		})
	})

	// --- CSRF-protected form flow: a GET establishes the session and
	// returns its token (in a real app, rendered into a hidden
	// <input type="hidden" name="_token"> field or a <meta
	// name="csrf-token"> tag via c.CsrfToken()); the matching POST must
	// echo that token back via the "_token" field, X-CSRF-TOKEN, or
	// X-XSRF-TOKEN, or it's rejected with 419. ---
	kernel.GET("/comments", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
	}).Middleware("csrf").Name("comments.form")

	kernel.POST("/comments", resourceHandler("comments.store")).Middleware("csrf").Name("comments.store")

	// A third-party webhook: state-changing (POST), but it can't carry our
	// session's CSRF token, so it's exempted via VerifyCsrfToken's Except
	// list ("/stripe/*") rather than by omitting the "csrf" middleware —
	// the same middleware runs, it just lets this path through.
	kernel.POST("/stripe/webhook", resourceHandler("stripe.webhook")).Middleware("csrf").Name("stripe.webhook")

	// --- Redirect shortcut: Route::redirect($from, $to, $status) ---
	kernel.Redirect("/home", "/user", http.StatusFound)

	// --- Fallback route: runs when nothing else matches, after global
	// middleware (logging, method spoofing, ...) has already executed. ---
	kernel.Fallback(func(c *apphttp.Context) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
	})
}

// resourceHandler returns a small demo handler that echoes back the route's
// name, method, and resolved parameters, so every example route above
// returns something meaningful without needing a dedicated controller per
// resource.
func resourceHandler(name string) apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]any{
			"route":  name,
			"method": c.Request.Method,
			"params": c.Params(),
		})
	}
}
