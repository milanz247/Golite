package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"Golite/container"
)

// HandlerFunc is Golite's request handler signature, analogous to Laravel's
// Closure-based route actions and middleware.
type HandlerFunc func(*Context)

// Context wraps the request/response pair together with the application's
// service container (Laravel's Application itself extends Container, so
// resolving services through App mirrors Laravel's `app()->make(...)`),
// the resolved route parameters, and a middleware/handler pipeline.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	App     *container.Container

	handlers []HandlerFunc
	index    int
	params   map[string]string
}

func newContext(w http.ResponseWriter, r *http.Request, app *container.Container, handlers []HandlerFunc) *Context {
	return &Context{
		Writer:   w,
		Request:  r,
		App:      app,
		handlers: handlers,
		index:    -1,
	}
}

// Next invokes the next handler in the chain, if any, and returns once it
// (and everything it in turn calls Next on) has finished — the classic
// recursive "onion" middleware pattern. A middleware that wants to run code
// after the rest of the chain finishes should call Next and then continue
// below it, just like Laravel's pipeline; a middleware that wants to
// short-circuit the request (e.g. failed auth) simply returns *without*
// calling Next, and nothing further down the chain ever runs. Handlers
// appended to the chain *while* Next is running (see Kernel.dispatch, which
// splices in route-specific middleware only once routing has been
// resolved) are picked up correctly, since each call re-checks
// len(c.handlers) against the freshly incremented index.
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}

// Param returns the value of a resolved route parameter (e.g. "id" for a
// route defined as "/user/{id}"), or "" if it wasn't present and had no
// default.
func (c *Context) Param(name string) string {
	if c.params == nil {
		return ""
	}
	return c.params[name]
}

// Params returns a copy of every resolved route parameter for the current
// request, analogous to Laravel's $request->route()->parameters().
func (c *Context) Params() map[string]string {
	out := make(map[string]string, len(c.params))
	for k, v := range c.params {
		out[k] = v
	}
	return out
}

// JSON writes a JSON response with the given status code.
func (c *Context) JSON(status int, payload any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(status)
	_ = json.NewEncoder(c.Writer).Encode(payload)
}

// Redirect writes an HTTP redirect response, the primitive behind
// Kernel.Redirect (Route::redirect).
func (c *Context) Redirect(status int, url string) {
	http.Redirect(c.Writer, c.Request, url, status)
}

// ---------------------------------------------------------------------------
// RouteDefinition: a single registered route, with Laravel-style fluent
// configuration (parameters, regex constraints, naming, and per-route
// middleware). Named "RouteDefinition" rather than "Route" so the package
// can also expose the global Route(name, params) URL-generation helper
// required below without a naming collision.
// ---------------------------------------------------------------------------

type routeSegment struct {
	static   string // literal path segment, used when isParam is false
	param    string // parameter name, used when isParam is true
	optional bool   // true for "{param?}" segments
	isParam  bool
}

// RouteDefinition represents one registered route. Every fluent method
// (Where/WhereNumber/.../Name/Middleware/Defaults) mutates the route in
// place and returns it, mirroring the Illuminate\Routing\Route builder.
type RouteDefinition struct {
	kernel  *Kernel
	methods []string // immutable after creation
	uri     string   // normalized URI, e.g. "/posts/{post}/comments/{comment?}"

	segments []routeSegment // immutable after creation
	handler  HandlerFunc

	namePrefix string // captured from the enclosing group(s) at creation time

	mu              sync.RWMutex
	name            string
	wheres          map[string]string // param name -> regex constraint
	defaults        map[string]string // param name -> fallback value
	middlewareNames []string          // group middleware (at creation) + per-route middleware
	regex           *regexp.Regexp
}

var paramNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseSegments splits a URI into static and {param}/{param?} segments.
func parseSegments(uri string) []routeSegment {
	trimmed := strings.Trim(uri, "/")
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, "/")
	segments := make([]routeSegment, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && len(part) >= 2 {
			name := part[1 : len(part)-1]
			optional := strings.HasSuffix(name, "?")
			if optional {
				name = strings.TrimSuffix(name, "?")
			}
			if !paramNameRe.MatchString(name) {
				panic(fmt.Sprintf("golite: invalid route parameter name %q in %q", name, uri))
			}
			segments = append(segments, routeSegment{param: name, optional: optional, isParam: true})
			continue
		}
		segments = append(segments, routeSegment{static: part})
	}
	return segments
}

// compilePattern turns a route's segments into an anchored regular
// expression, embedding each parameter's constraint (from wheres, defaulting
// to "any non-slash characters") directly into the pattern — this is how
// Route::where ends up making a request 404 rather than needing a separate
// validation step: a constraint violation simply means the compiled regex
// doesn't match, so the router falls through exactly as if the route didn't
// exist.
func compilePattern(segments []routeSegment, wheres map[string]string) (*regexp.Regexp, error) {
	if len(segments) == 0 {
		return regexp.Compile(`^/$`)
	}

	var b strings.Builder
	b.WriteString("^")
	for _, seg := range segments {
		if !seg.isParam {
			b.WriteString("/")
			b.WriteString(regexp.QuoteMeta(seg.static))
			continue
		}

		constraint := wheres[seg.param]
		if constraint == "" {
			constraint = "[^/]+"
		}
		group := fmt.Sprintf("(?P<%s>%s)", seg.param, constraint)

		if seg.optional {
			b.WriteString("(?:/")
			b.WriteString(group)
			b.WriteString(")?")
		} else {
			b.WriteString("/")
			b.WriteString(group)
		}
	}
	b.WriteString("$")

	return regexp.Compile(b.String())
}

// recompile rebuilds the route's matching regex from its current segments
// and constraints. Called once at registration and again every time Where /
// WhereMap adds a new constraint.
func (r *RouteDefinition) recompile() {
	r.mu.Lock()
	wheres := make(map[string]string, len(r.wheres))
	for k, v := range r.wheres {
		wheres[k] = v
	}
	r.mu.Unlock()

	re, err := compilePattern(r.segments, wheres)
	if err != nil {
		panic(fmt.Sprintf("golite: invalid constraint pattern for route %q: %v", r.uri, err))
	}

	r.mu.Lock()
	r.regex = re
	r.mu.Unlock()
}

// Where constrains a single parameter to match a regular expression,
// equivalent to Laravel's ->where("id", "[0-9]+").
func (r *RouteDefinition) Where(param, pattern string) *RouteDefinition {
	r.mu.Lock()
	r.wheres[param] = pattern
	r.mu.Unlock()
	r.recompile()
	return r
}

// WhereMap constrains multiple parameters at once, equivalent to Laravel's
// ->where(["id" => "[0-9]+", "slug" => "[a-z-]+"]).
func (r *RouteDefinition) WhereMap(constraints map[string]string) *RouteDefinition {
	r.mu.Lock()
	for param, pattern := range constraints {
		r.wheres[param] = pattern
	}
	r.mu.Unlock()
	r.recompile()
	return r
}

// WhereNumber constrains a parameter to digits only.
func (r *RouteDefinition) WhereNumber(param string) *RouteDefinition {
	return r.Where(param, `[0-9]+`)
}

// WhereAlpha constrains a parameter to letters only.
func (r *RouteDefinition) WhereAlpha(param string) *RouteDefinition {
	return r.Where(param, `[A-Za-z]+`)
}

// WhereAlphaNumeric constrains a parameter to letters and digits only.
func (r *RouteDefinition) WhereAlphaNumeric(param string) *RouteDefinition {
	return r.Where(param, `[A-Za-z0-9]+`)
}

// WhereIn constrains a parameter to one of a fixed set of values.
func (r *RouteDefinition) WhereIn(param string, values []string) *RouteDefinition {
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = regexp.QuoteMeta(v)
	}
	return r.Where(param, "(?:"+strings.Join(escaped, "|")+")")
}

// Defaults sets fallback values used when an optional parameter is absent
// from the matched URL, equivalent to Laravel's ->defaults().
func (r *RouteDefinition) Defaults(defaults map[string]string) *RouteDefinition {
	r.mu.Lock()
	for k, v := range defaults {
		r.defaults[k] = v
	}
	r.mu.Unlock()
	return r
}

// Name assigns (or extends, if the route was declared inside a ->name(...)
// group) the route's name and registers it in the kernel's named-route
// table for URL generation via Kernel.Route / the global Route() helper.
func (r *RouteDefinition) Name(name string) *RouteDefinition {
	r.mu.Lock()
	full := r.namePrefix + name
	r.name = full
	r.mu.Unlock()

	if r.kernel != nil {
		r.kernel.registerNamedRoute(full, r)
	}
	return r
}

// Middleware appends middleware (referenced by alias name, see
// Kernel.AliasMiddleware) to this specific route, in addition to whatever
// middleware its enclosing group(s) already contributed.
func (r *RouteDefinition) Middleware(names ...string) *RouteDefinition {
	r.mu.Lock()
	r.middlewareNames = append(r.middlewareNames, names...)
	r.mu.Unlock()
	return r
}

func (r *RouteDefinition) middlewareNamesCopy() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.middlewareNames))
	copy(out, r.middlewareNames)
	return out
}

func (r *RouteDefinition) hasMethod(method string) bool {
	for _, m := range r.methods {
		if m == method {
			return true
		}
	}
	return false
}

// matchPath tests the route's compiled regex against a request path and, on
// success, returns the resolved parameters (falling back to any configured
// defaults for empty optional segments).
func (r *RouteDefinition) matchPath(path string) (map[string]string, bool) {
	r.mu.RLock()
	re := r.regex
	defaults := r.defaults
	r.mu.RUnlock()

	if re == nil {
		return nil, false
	}
	matches := re.FindStringSubmatch(path)
	if matches == nil {
		return nil, false
	}

	params := make(map[string]string, len(matches))
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		value := matches[i]
		if value == "" {
			if def, ok := defaults[name]; ok {
				value = def
			}
		}
		params[name] = value
	}
	return params, true
}

// buildURL renders a concrete URL for this route from a set of parameter
// values, used for named-route URL generation. Missing optional parameters
// (and their segment) are omitted entirely; a missing required parameter is
// rendered as a visible "{name}" placeholder rather than silently producing
// a broken URL.
func (r *RouteDefinition) buildURL(params map[string]any) string {
	r.mu.RLock()
	defaults := make(map[string]string, len(r.defaults))
	for k, v := range r.defaults {
		defaults[k] = v
	}
	r.mu.RUnlock()

	var b strings.Builder
	for _, seg := range r.segments {
		if !seg.isParam {
			b.WriteString("/")
			b.WriteString(seg.static)
			continue
		}

		value, ok := stringifyParam(params, seg.param)
		if !ok {
			if def, hasDefault := defaults[seg.param]; hasDefault {
				value, ok = def, true
			}
		}
		if !ok {
			if seg.optional {
				continue
			}
			value = "{" + seg.param + "}"
		}
		b.WriteString("/")
		b.WriteString(value)
	}

	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}

func stringifyParam(params map[string]any, name string) (string, bool) {
	if params == nil {
		return "", false
	}
	v, ok := params[name]
	if !ok {
		return "", false
	}
	return fmt.Sprint(v), true
}

// ---------------------------------------------------------------------------
// RouteGroup: shared prefix / middleware / name-prefix attributes for a set
// of routes, equivalent to Laravel's Route::prefix()/middleware()/name()
// fluent group builder.
// ---------------------------------------------------------------------------

// RouteGroup accumulates shared attributes (URI prefix, middleware aliases,
// and route-name prefix) for a set of routes. Every fluent method returns a
// new RouteGroup rather than mutating the receiver, so a group can be reused
// as the base for several independent sub-groups without cross-contaminating
// their attributes.
type RouteGroup struct {
	kernel     *Kernel
	prefix     string
	namePrefix string
	middleware []string
}

func (g *RouteGroup) clone() *RouteGroup {
	middleware := make([]string, len(g.middleware))
	copy(middleware, g.middleware)
	return &RouteGroup{
		kernel:     g.kernel,
		prefix:     g.prefix,
		namePrefix: g.namePrefix,
		middleware: middleware,
	}
}

// Prefix extends the group's URI prefix, equivalent to Route::prefix().
func (g *RouteGroup) Prefix(prefix string) *RouteGroup {
	c := g.clone()
	c.prefix = joinURI(g.prefix, prefix)
	return c
}

// Middleware appends middleware aliases to the group, equivalent to
// Route::middleware().
func (g *RouteGroup) Middleware(names ...string) *RouteGroup {
	c := g.clone()
	c.middleware = append(c.middleware, names...)
	return c
}

// Name extends the group's route-name prefix, equivalent to Route::name().
func (g *RouteGroup) Name(prefix string) *RouteGroup {
	c := g.clone()
	c.namePrefix = g.namePrefix + prefix
	return c
}

// Group invokes fn with this RouteGroup so routes — and further nested
// groups, which inherit and extend its prefix/name/middleware — can be
// registered against it, equivalent to Laravel's Route::group(Closure).
func (g *RouteGroup) Group(fn func(*RouteGroup)) {
	fn(g)
}

func (g *RouteGroup) GET(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodGet}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

func (g *RouteGroup) POST(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodPost}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

func (g *RouteGroup) PUT(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodPut}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

func (g *RouteGroup) PATCH(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodPatch}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

func (g *RouteGroup) DELETE(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodDelete}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

func (g *RouteGroup) OPTIONS(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute([]string{http.MethodOptions}, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

// Match registers a route that responds to a fixed set of HTTP methods,
// equivalent to Route::match([...], $uri, $action).
func (g *RouteGroup) Match(methods []string, uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute(normalizeMethods(methods), uri, handler, g.prefix, g.namePrefix, g.middleware)
}

// Any registers a route that responds to every common HTTP method,
// equivalent to Route::any($uri, $action).
func (g *RouteGroup) Any(uri string, handler HandlerFunc) *RouteDefinition {
	return g.kernel.addRoute(allMethods, uri, handler, g.prefix, g.namePrefix, g.middleware)
}

// joinURI joins URI fragments (route prefixes, group prefixes, route URIs)
// into a single, slash-normalized path fragment with no leading/trailing
// slash — the canonical form both RouteGroup.prefix and RouteDefinition.uri
// use internally.
func joinURI(parts ...string) string {
	var segments []string
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			segments = append(segments, part)
		}
	}
	return strings.Join(segments, "/")
}

func normalizeMethods(methods []string) []string {
	out := make([]string, len(methods))
	for i, m := range methods {
		out[i] = strings.ToUpper(strings.TrimSpace(m))
	}
	return out
}

var allMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

// ---------------------------------------------------------------------------
// Kernel: Golite's HTTP kernel — global middleware, the route table, named
// routes, middleware aliases, and the fallback route.
// ---------------------------------------------------------------------------

// Kernel is Golite's HTTP kernel: it owns the global middleware stack, the
// route table, named routes, and middleware aliases, and dispatches every
// incoming request through them. It implements http.Handler so it can be
// passed straight to http.ListenAndServe, mirroring Laravel's
// App\Http\Kernel + the Router that sits behind the Route facade.
type Kernel struct {
	container *container.Container

	mu                sync.RWMutex
	globalMiddleware  []HandlerFunc
	middlewareAliases map[string]HandlerFunc
	routes            []*RouteDefinition
	namedRoutes       map[string]*RouteDefinition
	fallback          *RouteDefinition
}

// NewKernel creates a Kernel bound to the given service container.
func NewKernel(c *container.Container) *Kernel {
	k := &Kernel{
		container:         c,
		middlewareAliases: make(map[string]HandlerFunc),
		namedRoutes:       make(map[string]*RouteDefinition),
	}
	setDefaultKernel(k)
	return k
}

// UseMiddleware registers one or more global middleware, executed on every
// request — including ones that end up in the fallback or 404 handler — in
// the order they were added, and always before routing is resolved (so
// middleware like method-spoofing can influence which route matches).
func (k *Kernel) UseMiddleware(middleware ...HandlerFunc) {
	k.mu.Lock()
	k.globalMiddleware = append(k.globalMiddleware, middleware...)
	k.mu.Unlock()
}

// AliasMiddleware registers a named middleware, so routes and groups can
// reference it by string (Route::middleware("auth")) instead of needing a
// direct HandlerFunc reference.
func (k *Kernel) AliasMiddleware(name string, mw HandlerFunc) {
	k.mu.Lock()
	k.middlewareAliases[name] = mw
	k.mu.Unlock()
}

func (k *Kernel) resolveMiddleware(names []string) []HandlerFunc {
	if len(names) == 0 {
		return nil
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	resolved := make([]HandlerFunc, 0, len(names))
	for _, name := range names {
		if mw, ok := k.middlewareAliases[name]; ok {
			resolved = append(resolved, mw)
		}
	}
	return resolved
}

func (k *Kernel) registerNamedRoute(name string, r *RouteDefinition) {
	k.mu.Lock()
	k.namedRoutes[name] = r
	k.mu.Unlock()
}

// addRoute is the single place every verb helper (on Kernel or RouteGroup)
// funnels through: it joins the group prefix with the route's own URI,
// parses parameters, compiles the matcher, and appends the route to the
// table.
func (k *Kernel) addRoute(methods []string, uri string, handler HandlerFunc, prefix, namePrefix string, groupMiddleware []string) *RouteDefinition {
	fullURI := joinURI(prefix, uri)
	segments := parseSegments(fullURI)

	displayURI := "/" + fullURI
	if fullURI == "" {
		displayURI = "/"
	}

	middlewareNames := make([]string, len(groupMiddleware))
	copy(middlewareNames, groupMiddleware)

	route := &RouteDefinition{
		kernel:          k,
		methods:         normalizeMethods(methods),
		uri:             displayURI,
		segments:        segments,
		handler:         handler,
		namePrefix:      namePrefix,
		wheres:          make(map[string]string),
		defaults:        make(map[string]string),
		middlewareNames: middlewareNames,
	}
	route.recompile()

	k.mu.Lock()
	k.routes = append(k.routes, route)
	k.mu.Unlock()

	return route
}

func (k *Kernel) GET(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodGet}, uri, handler, "", "", nil)
}

func (k *Kernel) POST(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodPost}, uri, handler, "", "", nil)
}

func (k *Kernel) PUT(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodPut}, uri, handler, "", "", nil)
}

func (k *Kernel) PATCH(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodPatch}, uri, handler, "", "", nil)
}

func (k *Kernel) DELETE(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodDelete}, uri, handler, "", "", nil)
}

func (k *Kernel) OPTIONS(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute([]string{http.MethodOptions}, uri, handler, "", "", nil)
}

// Match registers a route that responds to a fixed set of HTTP methods,
// equivalent to Route::match([...], $uri, $action).
func (k *Kernel) Match(methods []string, uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute(normalizeMethods(methods), uri, handler, "", "", nil)
}

// Any registers a route that responds to every common HTTP method,
// equivalent to Route::any($uri, $action).
func (k *Kernel) Any(uri string, handler HandlerFunc) *RouteDefinition {
	return k.addRoute(allMethods, uri, handler, "", "", nil)
}

// Prefix starts a new route group with a shared URI prefix, equivalent to
// Route::prefix($prefix)->group(...).
func (k *Kernel) Prefix(prefix string) *RouteGroup {
	return (&RouteGroup{kernel: k}).Prefix(prefix)
}

// Middleware starts a new route group with shared middleware, equivalent to
// Route::middleware(...)->group(...).
func (k *Kernel) Middleware(names ...string) *RouteGroup {
	return (&RouteGroup{kernel: k}).Middleware(names...)
}

// Name starts a new route group with a shared route-name prefix, equivalent
// to Route::name($prefix)->group(...).
func (k *Kernel) Name(prefix string) *RouteGroup {
	return (&RouteGroup{kernel: k}).Name(prefix)
}

// Redirect registers a route that redirects every common HTTP method from
// one URI to another, equivalent to Route::redirect($from, $to, $status).
// The default status is 302 Found, matching Laravel.
func (k *Kernel) Redirect(from, to string, status int) *RouteDefinition {
	if status == 0 {
		status = http.StatusFound
	}
	return k.addRoute(allMethods, from, func(c *Context) {
		c.Redirect(status, to)
	}, "", "", nil)
}

// Fallback registers a handler run when no other route matches — after
// global middleware has already executed — equivalent to
// Route::fallback($action).
func (k *Kernel) Fallback(handler HandlerFunc) *RouteDefinition {
	route := &RouteDefinition{kernel: k, handler: handler}
	k.mu.Lock()
	k.fallback = route
	k.mu.Unlock()
	return route
}

// Route resolves a named route to a concrete URL, equivalent to Laravel's
// route($name, $parameters) helper. Returns "" if no route was registered
// under that name.
func (k *Kernel) Route(name string, params map[string]any) string {
	k.mu.RLock()
	route, ok := k.namedRoutes[name]
	k.mu.RUnlock()
	if !ok {
		return ""
	}
	return route.buildURL(params)
}

// match finds the first route whose method and compiled pattern match the
// request. If no route matches both, it separately reports whether the path
// matched under a *different* method (so the caller can respond 405 with an
// Allow header, matching Laravel's MethodNotAllowedHttpException behavior)
// versus not matching at all (a plain 404).
func (k *Kernel) match(method, path string) (route *RouteDefinition, params map[string]string, pathMatched bool, allowed []string) {
	k.mu.RLock()
	routes := make([]*RouteDefinition, len(k.routes))
	copy(routes, k.routes)
	k.mu.RUnlock()

	for _, rt := range routes {
		if !rt.hasMethod(method) {
			continue
		}
		if p, ok := rt.matchPath(path); ok {
			return rt, p, true, nil
		}
	}

	seen := make(map[string]bool)
	for _, rt := range routes {
		if _, ok := rt.matchPath(path); !ok {
			continue
		}
		pathMatched = true
		for _, m := range rt.methods {
			if !seen[m] {
				seen[m] = true
				allowed = append(allowed, m)
			}
		}
	}

	return nil, nil, pathMatched, allowed
}

// dispatch resolves the route for the current request and runs it. It is
// always the last handler in Context's chain (see ServeHTTP), so it runs
// after every global middleware — including MethodSpoofingMiddleware, which
// means route matching sees the (possibly overridden) final HTTP method.
// On a match, route-specific middleware and the route handler are spliced
// into the same Context's handler chain and executed via a nested Next(),
// keeping the whole request in a single onion-style pipeline.
func (k *Kernel) dispatch(c *Context) {
	route, params, pathMatched, allowed := k.match(c.Request.Method, c.Request.URL.Path)

	if route != nil {
		c.params = params
		c.handlers = append(c.handlers, k.resolveMiddleware(route.middlewareNamesCopy())...)
		c.handlers = append(c.handlers, route.handler)
		c.Next()
		return
	}

	if pathMatched && len(allowed) > 0 {
		c.Writer.Header().Set("Allow", strings.Join(allowed, ", "))
		c.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "405 method not allowed"})
		return
	}

	k.mu.RLock()
	fallback := k.fallback
	k.mu.RUnlock()
	if fallback != nil {
		fallback.handler(c)
		return
	}

	c.JSON(http.StatusNotFound, map[string]string{"error": "404 not found"})
}

// ServeHTTP builds the request's middleware chain — every global middleware
// followed by the kernel's own routing dispatch — and runs it. This is
// Golite's front controller, the equivalent of Laravel's
// public/index.php -> Kernel::handle().
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	global := make([]HandlerFunc, len(k.globalMiddleware))
	copy(global, k.globalMiddleware)
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(global)+1)
	chain = append(chain, global...)
	chain = append(chain, k.dispatch)

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()
}

// ---------------------------------------------------------------------------
// Global URL helper, mirroring Laravel's global route() function. It
// operates on whichever Kernel was most recently constructed via NewKernel
// — fine for Golite's single-application model, where exactly one Kernel is
// created in bootstrap.NewApplication.
// ---------------------------------------------------------------------------

var (
	defaultKernelMu sync.RWMutex
	defaultKernel   *Kernel
)

func setDefaultKernel(k *Kernel) {
	defaultKernelMu.Lock()
	defaultKernel = k
	defaultKernelMu.Unlock()
}

// Route generates a URL for a named route using the application's default
// kernel, mirroring Laravel's global route($name, $parameters) helper.
// Returns "" if no kernel has been created yet or the name is unknown.
func Route(name string, params map[string]any) string {
	defaultKernelMu.RLock()
	k := defaultKernel
	defaultKernelMu.RUnlock()
	if k == nil {
		return ""
	}
	return k.Route(name, params)
}
