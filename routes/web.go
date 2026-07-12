package routes

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/app/Http/Controllers"
)

// MapWebRoutes registers the application's web routes onto the kernel,
// mirroring routes/web.php. It demonstrates every routing feature the
// kernel supports: HTTP verb helpers, Match/Any, required and optional
// parameters with default values, regex constraints, named routes with URL
// generation, nested groups (prefix + middleware + name prefix), a
// redirect, and a fallback route.
func MapWebRoutes(kernel *apphttp.Kernel) {
	userController := controllers.NewUserController()

	// A minimal "auth" middleware alias, purely to demonstrate string-based
	// middleware references inside groups, mirroring Laravel's named
	// middleware (the "auth" key in app/Http/Kernel.php).
	kernel.AliasMiddleware("auth", requireAuthHeader)

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

	kernel.GET("/posts/{post}/comments/{comment?}", resourceHandler("posts.comments.show")).
		WhereNumber("post").
		WhereNumber("comment").
		Name("posts.comments.show")

	// --- Regex constraints: ->where, ->whereMap, and the fluent helpers.
	// A request whose parameters fail these constraints simply doesn't
	// match the route (falling through to a 404), exactly like Laravel. ---
	kernel.GET("/articles/{id}", resourceHandler("articles.show")).
		Where("id", `[0-9]+`).
		Name("articles.show")

	kernel.GET("/products/{category}/{sku}", resourceHandler("products.show")).
		WhereMap(map[string]string{
			"category": "[a-z-]+",
			"sku":      "[A-Z0-9]+",
		}).
		Name("products.show")

	kernel.GET("/users/{id}", userController.Show).
		WhereNumber("id").
		Name("users.show")

	kernel.GET("/tags/{slug}", resourceHandler("tags.show")).
		WhereAlphaNumeric("slug").
		Name("tags.show")

	kernel.GET("/authors/{name}", resourceHandler("authors.show")).
		WhereAlpha("name").
		Name("authors.show")

	kernel.GET("/categories/{category}", resourceHandler("categories.index")).
		WhereIn("category", []string{"news", "sports", "tech"}).
		Name("categories.index")

	// --- Route groups: shared prefix, middleware, and name prefix.
	// Equivalent to:
	//   Route::prefix("admin")->middleware("auth")->name("admin.")->group(func () {
	//       Route::get("/users", [UserController::class, "show"])->name("users");
	//   });
	kernel.Prefix("admin").Middleware("auth").Name("admin.").Group(func(admin *apphttp.RouteGroup) {
		admin.GET("/users", userController.Show).Name("users") // GET /admin/users, named "admin.users"

		// Nested groups inherit and extend the parent's prefix, name
		// prefix, and middleware.
		admin.Prefix("posts").Name("posts.").Group(func(posts *apphttp.RouteGroup) {
			posts.GET("", resourceHandler("admin.posts.index")).Name("index")                          // GET /admin/posts
			posts.GET("/{post}", resourceHandler("admin.posts.show")).WhereNumber("post").Name("show") // GET /admin/posts/{post}
		})
	})

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

// requireAuthHeader is a minimal demo middleware bound to the "auth" alias.
func requireAuthHeader(c *apphttp.Context) {
	if c.Request.Header.Get("Authorization") == "" {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	c.Next()
}
