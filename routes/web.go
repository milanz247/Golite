package routes

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

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
// straight from the service container, CSRF protection, request-inspection
// helpers, the unified input payload, encrypted cookies, flash/old input,
// file uploads, controller/resource routing (Resource, ApiResource,
// nested + shallow resources, singleton resources, and a single-action
// controller), response handling (dynamic return-type serialization, the
// fluent Response factory, redirects with flash data, specialized formats
// — Json/View/Download/File/StreamDownload — and macros), and the
// driver-based session engine (the full Session API, and .Block() for
// atomic per-session locking).
func MapWebRoutes(kernel *apphttp.Kernel) {
	userController := controllers.NewUserController()

	// --- Middleware priority: regardless of the order middleware is
	// assigned on a route or pulled in via a group, it always executes in
	// this order (anything not listed here runs last, in registration
	// order) — equivalent to Laravel's $middlewarePriority. "session" runs
	// first (everything else may depend on a session being attached),
	// then "csrf" (which needs the session to check its token against),
	// mirroring Laravel's own ordering. ---
	kernel.MiddlewarePriority = []string{"session", "csrf", "auth", "role", "audit"}

	// --- Sessions: NewKernel already seeds the "web" middleware group with
	// the name "session" (see Kernel.go), but that name only does
	// something once it's aliased to a real implementation here, backed by
	// the kernel's own session manager (kernel.Sessions()) — see
	// docs/sessions.md for switching its driver or registering a custom
	// one via Extend. ---
	kernel.AliasMiddleware("session", middleware.NewStartSession(kernel.Sessions()))

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
	// (individual PUT/PATCH/DELETE/etc. handlers for "/posts/{post}" used
	// to be demonstrated by hand here; they've been superseded by
	// kernel.Resource("posts", postController) further down, which covers
	// the same methods/paths — see the "Controllers & resource routing"
	// section — so registering both would collide.)
	// userController.Show declares Hasher and *config.Config as ordinary
	// parameters (Laravel-style method injection: public function
	// show(Hasher $hash, Repository $config)) instead of pulling them out
	// of the container itself — apphttp.Inject resolves each one by type
	// at request time. See docs/controllers.md#method-injection.
	kernel.GET("/user", apphttp.Inject(kernel.Container(), userController.Show)).Name("user.show")

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
		admin.GET("/users", apphttp.Inject(kernel.Container(), userController.Show)).Name("users") // GET /admin/users, named "admin.users"

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
	// X-XSRF-TOKEN, or it's rejected with 419. "session" is required
	// alongside "csrf" on every route below — VerifyCsrfToken calls
	// c.Session() itself (to check/store the token), which panics unless
	// a session was already attached. ---
	kernel.GET("/comments", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{"csrf_token": c.CsrfToken()})
	}).Middleware("session", "csrf").Name("comments.form")

	kernel.POST("/comments", resourceHandler("comments.store")).Middleware("session", "csrf").Name("comments.store")

	// A third-party webhook: state-changing (POST), but it can't carry our
	// session's CSRF token, so it's exempted via VerifyCsrfToken's Except
	// list ("/stripe/*") rather than by omitting the "csrf" middleware —
	// the same middleware runs, it just lets this path through.
	kernel.POST("/stripe/webhook", resourceHandler("stripe.webhook")).Middleware("session", "csrf").Name("stripe.webhook")

	// --- Request inspection helpers: Path/Is/Url/FullUrl/Method/IsMethod/
	// Ip/BearerToken/ExpectsJson. Ip() only ever reads Request.RemoteAddr
	// (see its doc comment) — it reflects TrustProxies' resolved client
	// address whenever the immediate peer is a trusted proxy. ---
	kernel.GET("/request-info", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]any{
			"path":         c.Path(),
			"url":          c.Url(),
			"full_url":     c.FullUrl(),
			"method":       c.Method(),
			"is_get":       c.IsMethod("GET"),
			"is_admin":     c.Is("admin/*"),
			"ip":           c.Ip(),
			"bearer_token": c.BearerToken(),
			"expects_json": c.ExpectsJson(),
		})
	}).Name("request.info")

	// --- Unified input: All/Input (with a default)/Has/Only/Except/
	// Boolean, merging query parameters, a JSON body, and form fields
	// (whichever the request actually sent) into one payload. Named
	// "account.*", not "profile.*" — that name (and a real POST /profile
	// route) belongs to the Singleton resource demo further down. ---
	kernel.POST("/account", func(c *apphttp.Context) {
		if !c.Has("name", "email") {
			c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "name and email are required"})
			return
		}
		c.JSON(http.StatusOK, map[string]any{
			"all":        c.All(),
			"only":       c.Only("name", "email"),
			"except":     c.Except("email"),
			"nickname":   c.Input("nickname", "anonymous"),
			"newsletter": c.Boolean("newsletter"),
		})
	}).Name("account.update")

	// --- Encrypted, authenticated cookies: SetCookie/Cookie. ---
	kernel.GET("/cookie/set", func(c *apphttp.Context) {
		if err := c.SetCookie("preferred_theme", "dark", 3600); err != nil {
			c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to set cookie"})
			return
		}
		c.JSON(http.StatusOK, map[string]string{"status": "cookie set"})
	}).Name("cookie.set")

	kernel.GET("/cookie/read", func(c *apphttp.Context) {
		value, err := c.Cookie("preferred_theme")
		if err != nil {
			c.JSON(http.StatusOK, map[string]string{"preferred_theme": ""})
			return
		}
		c.JSON(http.StatusOK, map[string]string{"preferred_theme": value})
	}).Name("cookie.read")

	// --- Flash + Old input, paired with CSRF (the realistic Laravel
	// pattern: a failed submission flashes the input and redirects back to
	// the form, which repopulates itself from Old and still carries a
	// valid CSRF token since both live in the same session). ---
	kernel.GET("/contact", func(c *apphttp.Context) {
		c.JSON(http.StatusOK, map[string]string{
			"old_email":  c.Old("email"),
			"csrf_token": c.CsrfToken(),
		})
	}).Middleware("session", "csrf").Name("contact.form")

	kernel.POST("/contact", func(c *apphttp.Context) {
		if !c.Has("email") {
			// .WithInput() replaces the old manual c.Flash() call — same
			// effect (flash the current input for the next request's
			// Old()), now fluent off the redirect itself.
			c.Redirect("/contact", http.StatusFound).WithInput().Send(c)
			return
		}
		c.JSON(http.StatusOK, map[string]string{"status": "submitted"})
	}).Middleware("session", "csrf").Name("contact.submit")

	// --- File uploads: HasFile/File, then Store with an automatically
	// generated, collision-resistant filename. ---
	kernel.POST("/avatar", func(c *apphttp.Context) {
		if !c.HasFile("avatar") {
			c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "avatar file is required"})
			return
		}
		file, err := c.File("avatar")
		if err != nil || !file.IsValid() {
			c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid upload"})
			return
		}
		path, err := file.Store("storage/avatars")
		if err != nil {
			c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
			return
		}
		c.JSON(http.StatusOK, map[string]any{
			"original_name": file.Filename,
			"size":          file.Size,
			"extension":     file.Extension(),
			"stored_at":     path,
		})
	}).Name("avatar.upload")

	// --- Controllers & resource routing ---

	// PostController takes a constructor-injected dependency resolved from
	// the service container — Golite's equivalent of Laravel's automatic
	// controller-constructor dependency injection — and declares its own
	// middleware via the embedded Controller base: "auth" applies to every
	// action except Index and Show.
	postController := controllers.NewPostController(kernel.Container().Make("hash").(controllers.Hasher))

	// Route::resource: registers Index/Store/Show/Update/Destroy. Create
	// and Edit are skipped automatically, since reflection finds no such
	// methods on PostController (see Kernel.Resource's "automatically
	// inspect the controller" behavior in app/Http/Resource.go) — a GET to
	// /posts/create therefore falls through to Show with post="create",
	// exactly like it would under Laravel's apiResource().
	kernel.Resource("posts", postController)

	// Route::apiResource, scoped inside a "api" prefix + name group: the
	// same controller, already missing Create/Edit by definition, and this
	// call also excludes Destroy via .Except(...).
	kernel.Prefix("api").Name("api.").Group(func(g *apphttp.RouteGroup) {
		g.ApiResource("posts", postController).Except([]string{"destroy"})
	})

	// Nested + shallow resource routing: comments nest under their photo
	// for the collection actions (index/store), but Shallow() promotes the
	// member actions (show/update/destroy) to /comments/{comment}
	// directly, since a comment's own ID is already globally unique.
	// Equivalent to:
	//   Route::resource('photos.comments', CommentController::class)->shallow();
	commentController := controllers.NewCommentController()
	kernel.Resource("photos.comments", commentController).Shallow()

	// Singleton resource: a resource with exactly one instance, so its
	// routes carry no {id} segment — Show/Edit/Update by default, plus
	// Create/Store via .Creatable().
	profileController := controllers.NewProfileController()
	kernel.Singleton("profile", profileController).Creatable()

	// Single-action (invokable) controller: registered directly on a route
	// via InvokableHandler rather than a named method, equivalent to
	// Route::post('/server', ProvisionServerController::class). Its own
	// declared middleware isn't picked up automatically the way
	// Resource/ApiResource/Singleton do it — a plain verb route has no
	// other way to know a controller was involved at all — so it's
	// attached explicitly via ApplyControllerMiddleware.
	provisionController := controllers.NewProvisionServerController()
	apphttp.ApplyControllerMiddleware(
		kernel.POST("/server", apphttp.InvokableHandler(provisionController)),
		provisionController,
		apphttp.InvokeAction,
	).Name("server.provision")

	// --- Response handling: dynamic return-type serialization, the
	// fluent Response factory, redirects with flash data, specialized
	// formats, and macros ---

	// A tiny on-disk file for the Download/File demo routes below, created
	// once at boot so they work after a fresh clone — storage/ is
	// git-ignored, the same reason the file-upload demo from earlier
	// generates its own storage/avatars directory rather than committing
	// one.
	demoFilePath := "storage/app/sample.txt"
	if err := os.MkdirAll(filepath.Dir(demoFilePath), 0o755); err == nil {
		_ = os.WriteFile(demoFilePath, []byte("Hello from Golite's file response demo!\n"), 0o644)
	}

	// Requirement 1: a handler wrapped in Responder can return a value
	// instead of writing the response itself. A string is auto-sent as
	// text/html; a struct/map/slice/array is auto-serialized to JSON.
	kernel.GET("/greeting-text", apphttp.Responder(func(c *apphttp.Context) any {
		return "Hello from a plain string return!" // -> text/html, 200
	})).Name("response.string")

	kernel.GET("/greeting-json", apphttp.Responder(func(c *apphttp.Context) any {
		return map[string]any{"message": "Hello from a returned map!", "code": 200} // -> application/json, 200
	})).Name("response.map")

	// Requirement 2: the fluent Response factory — Status/Header/
	// WithHeaders/Cookie/WithoutCookie, all composing regardless of which
	// specialized format (if any) is chained afterward.
	kernel.GET("/response/fluent", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Response(map[string]string{"status": "created"}, http.StatusCreated).
			Header("X-Powered-By", "Golite").
			WithHeaders(map[string]string{"X-Request-Id": "demo-123"}).
			Cookie("last_visit", time.Now().Format(time.RFC3339), 60).
			WithoutCookie("stale_session")
	})).Name("response.fluent")

	// .Json forces JSON regardless of the Go type of the content.
	kernel.GET("/response/json", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Response(nil).Json([]string{"go", "is", "fun"})
	})).Name("response.json")

	// .View renders an html/template file from resources/views/ — see
	// controllers.Index for the plain-function controller style (no
	// struct, no Controller embed) that Context.View makes practical for
	// a handler this simple.
	kernel.GET("/welcome", controllers.Index).Name("welcome")

	// .Download forces a save-as download; .File serves the same kind of
	// file inline (e.g. an image or PDF opening directly in the browser);
	// .StreamDownload streams generated content straight to the client
	// with no temporary file ever written to disk.
	kernel.GET("/files/download", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Response(nil).Download(demoFilePath, "golite-sample.txt")
	})).Name("files.download")

	kernel.GET("/files/view", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Response(nil).File(demoFilePath)
	})).Name("files.view")

	kernel.GET("/files/report", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Response(nil).StreamDownload(func(w io.Writer) {
			fmt.Fprintf(w, "Report generated at %s\n", time.Now().Format(time.RFC3339))
			fmt.Fprintln(w, "No temp file was written to produce this.")
		}, "report.txt")
	})).Name("files.report")

	// Requirement 3: redirects with flash data (.With for a one-off
	// message, .WithInput — demonstrated on /contact above — for form
	// repopulation), Back, and Away.
	kernel.GET("/greet-redirect", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Redirect("/greeting-text").With("flash_message", "Redirected with a flashed message!")
	})).Middleware("session").Name("response.redirect")

	kernel.GET("/go-back", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Back()
	})).Name("response.back")

	kernel.GET("/go-away", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Away("https://go.dev")
	})).Name("response.away")

	// Requirement 5: response macros. "caps" is registered once in
	// AppServiceProvider.Register; c.Macro looks it up by name and invokes
	// it (via reflection, since Go has no __callStatic-style dynamic
	// dispatch) with whatever arguments are given here.
	kernel.GET("/shout", apphttp.Responder(func(c *apphttp.Context) any {
		return c.Macro("caps", "hello from a macro")
	})).Name("response.macro")

	// --- Sessions: the full Get/Put/Push/Pull/All/Has/Exists/Missing/
	// Increment/Decrement/Forget/Flush/Regenerate/Invalidate/Flash/Now/
	// Reflash API on c.Session(), plus .Block() for atomic locking. Every
	// route below needs the "session" middleware attached directly (they
	// aren't in the "web" group, which also pulls in "auth"/"audit" —
	// keeping these routes to just "session" keeps the demo focused). ---

	// Put/Get/Increment: track a visit counter and the last-seen IP.
	kernel.GET("/session/visit", apphttp.Responder(func(c *apphttp.Context) any {
		sess := c.Session()
		visits := sess.Increment("visits")
		sess.Put("last_ip", c.Ip())
		return map[string]any{"visits": visits, "last_ip": sess.Get("last_ip")}
	})).Middleware("session").Name("session.visit")

	// All/Has/Exists/Missing.
	kernel.GET("/session/all", apphttp.Responder(func(c *apphttp.Context) any {
		sess := c.Session()
		return map[string]any{
			"all":           sess.All(),
			"has_visits":    sess.Has("visits"),
			"exists_visits": sess.Exists("visits"),
			"missing_cart":  sess.Missing("cart"),
		}
	})).Middleware("session").Name("session.all")

	// Push, guarded by .Block(): two rapid-fire "add to cart" AJAX
	// requests sharing a session can't race and silently drop one item —
	// see docs/sessions.md for exactly what .Block() does and doesn't
	// guarantee here.
	kernel.POST("/cart/add", apphttp.Responder(func(c *apphttp.Context) any {
		item, _ := c.Input("item", "").(string)
		if item == "" {
			return c.Response(map[string]string{"error": "item is required"}, http.StatusUnprocessableEntity)
		}
		sess := c.Session()
		sess.Push("cart", item)
		return map[string]any{"cart": sess.Get("cart", []any{})}
	})).Middleware("session").Block(5).Name("cart.add")

	// Pull (get + remove in one step) and Forget.
	kernel.DELETE("/cart", apphttp.Responder(func(c *apphttp.Context) any {
		sess := c.Session()
		removed := sess.Pull("cart")
		sess.Forget("last_ip")
		return map[string]any{"removed_cart": removed}
	})).Middleware("session").Name("cart.clear")

	// Flash (readable this request AND the next one) vs Now (readable
	// only this request); Reflash extends every currently-visible flash
	// message for one more cycle, and Keep does the same for specific
	// keys.
	kernel.GET("/session/flash-demo", apphttp.Responder(func(c *apphttp.Context) any {
		sess := c.Session()
		sess.Now("shown_now", "visible only for this request")
		sess.Flash("notice", "visible for this request and the next one")
		return map[string]any{"now": sess.Get("shown_now"), "flash": sess.Get("notice")}
	})).Middleware("session").Name("session.flash-demo")

	kernel.GET("/session/reflash", apphttp.Responder(func(c *apphttp.Context) any {
		c.Session().Reflash()
		return map[string]string{"status": "reflashed"}
	})).Middleware("session").Name("session.reflash")

	// Regenerate (keep the session's data, assign it a fresh ID — call
	// right after a successful login to prevent session fixation) vs
	// Invalidate (fresh ID *and* every value discarded — call on logout).
	// Both go through Context.RegenerateSession/InvalidateSession, not
	// c.Session().Regenerate/Invalidate directly, since this handler also
	// writes its own response — see Context.RegenerateSession's doc
	// comment for why only the Context-level wrapper reliably delivers the
	// new ID to the client in the same response.
	kernel.POST("/session/regenerate", apphttp.Responder(func(c *apphttp.Context) any {
		return map[string]string{"new_session_id": c.RegenerateSession()}
	})).Middleware("session").Name("session.regenerate")

	kernel.POST("/logout", apphttp.Responder(func(c *apphttp.Context) any {
		c.InvalidateSession()
		return map[string]string{"status": "logged out"}
	})).Middleware("session").Name("logout")

	// --- Encryption, Hashing, Validation, Error Handling, Logging: one
	// small controller per feature (CryptoController/HashController/
	// ValidationController/ErrorDemoController/LogController, all in
	// app/Http/Controllers). CryptoController/HashController/LogController
	// take no constructor arguments at all -- each action instead declares
	// the service it needs as an ordinary parameter (Encrypter/Hasher/
	// logging.Logger), resolved automatically by apphttp.Inject, the same
	// Laravel-style method injection userController.Show uses above. See
	// docs/controllers.md#method-injection, docs/encryption.md,
	// docs/hashing.md, docs/validation.md, docs/error-handling.md,
	// docs/logging.md. ---
	cryptoController := controllers.NewCryptoController()
	kernel.GET("/crypto/encrypt", apphttp.Inject(kernel.Container(), cryptoController.Encrypt)).Name("crypto.encrypt")
	kernel.GET("/crypto/decrypt", apphttp.Inject(kernel.Container(), cryptoController.Decrypt)).Name("crypto.decrypt")

	hashController := controllers.NewHashController()
	kernel.POST("/hash/make", apphttp.Inject(kernel.Container(), hashController.Make)).Name("hash.make")
	kernel.POST("/hash/check", apphttp.Inject(kernel.Container(), hashController.Check)).Name("hash.check")

	validationController := controllers.NewValidationController()
	kernel.POST("/register", validationController.Register).Name("register")

	errorDemoController := controllers.NewErrorDemoController()
	kernel.GET("/errors/abort/{code}", errorDemoController.Abort).WhereNumber("code").Name("errors.abort")
	kernel.GET("/errors/not-found", errorDemoController.NotFound).Name("errors.not-found")
	kernel.GET("/errors/boom", errorDemoController.Boom).Name("errors.boom")

	logController := controllers.NewLogController()
	kernel.GET("/logs/demo", apphttp.Inject(kernel.Container(), logController.Demo)).Name("logs.demo")

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
