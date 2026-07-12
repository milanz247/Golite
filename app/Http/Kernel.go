package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"Golite/container"
)

// HandlerFunc is Golite's terminal request-handler signature — what a
// controller action looks like. Unlike Middleware (below), a HandlerFunc
// has no "next" to call; it's the end of the pipeline.
type HandlerFunc func(*Context)

// ---------------------------------------------------------------------------
// Middleware: Golite's middleware contract, mirroring Laravel's
// $middleware->handle($request, $next, ...$params).
// ---------------------------------------------------------------------------

// Middleware is Golite's middleware contract. Handle receives the request
// Context, a next callback that continues the pipeline, and any parameters
// parsed from a "name:param1,param2" middleware string (see
// ParseMiddlewareSpec). A middleware that wants to short-circuit the
// request simply never calls next.
//
// Implementing Middleware as a struct (rather than a bare closure) is what
// lets a middleware take constructor-injected dependencies resolved from
// the service container — see Kernel.Container and
// docs/middleware.md#dependency-injection.
type Middleware interface {
	Handle(c *Context, next func(), params ...string)
}

// MiddlewareFunc adapts a plain function into a Middleware, for stateless
// middleware that needs neither parameters nor termination logic —
// Golite's equivalent of the standard library's http.HandlerFunc adapter.
type MiddlewareFunc func(c *Context, next func())

// Handle implements Middleware by calling f, ignoring any parameters.
func (f MiddlewareFunc) Handle(c *Context, next func(), _ ...string) {
	f(c, next)
}

// TerminableMiddleware is implemented by middleware that needs to run logic
// after the response has been fully written to the client — post-response
// cleanup, background/audit logging, and the like — mirroring Laravel's
// TerminableMiddleware::terminate(). The Kernel calls Terminate, in
// execution order, on every middleware from the current request that
// implements this interface, once the whole handler chain has returned.
type TerminableMiddleware interface {
	Terminate(c *Context)
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

// Context wraps the request/response pair together with the application's
// service container (Laravel's Application itself extends Container, so
// resolving services through App mirrors Laravel's `app()->make(...)`),
// the resolved route parameters, a middleware/handler pipeline, and a
// small per-request key/value store.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	App     *container.Container

	handlers []HandlerFunc
	index    int
	params   map[string]string

	store    map[string]any
	executed []Middleware
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

// Set stores an arbitrary value on the request, keyed by name. Its main
// purpose is letting a middleware pass data from Handle to its own
// Terminate — a middleware instance is shared across every concurrent
// request, so it must never store per-request state on itself; Context is
// the per-request, goroutine-safe place for that instead.
func (c *Context) Set(key string, value any) {
	if c.store == nil {
		c.store = make(map[string]any)
	}
	c.store[key] = value
}

// Get retrieves a value previously stored with Set.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.store[key]
	return v, ok
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
// Middleware spec parsing and name normalization shared by RouteDefinition,
// RouteGroup, and Kernel.
// ---------------------------------------------------------------------------

// ParseMiddlewareSpec splits a middleware string of the form
// "name:param1,param2" into its base name and parameter list, mirroring
// Laravel's parsing of "role:editor,admin" into ["editor", "admin"]. A spec
// with no ":" has no parameters.
func ParseMiddlewareSpec(spec string) (name string, params []string) {
	name, rest, hasParams := strings.Cut(spec, ":")
	if !hasParams || rest == "" {
		return name, nil
	}
	return name, strings.Split(rest, ",")
}

// flattenMiddlewareNames normalizes the two calling conventions every
// Middleware(...)/WithoutMiddleware(...) method accepts — a bare string
// ("auth"), several strings ("auth", "role:editor"), or a single []string
// ([]string{"web", "auth"}) — into one flat list, so callers can use
// whichever reads best at the call site.
func flattenMiddlewareNames(args []any) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch v := a.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		default:
			panic(fmt.Sprintf("golite: middleware name must be a string or []string, got %T", a))
		}
	}
	return out
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
// (Where/WhereNumber/.../Name/Middleware/WithoutMiddleware/Defaults) mutates
// the route in place and returns it, mirroring the
// Illuminate\Routing\Route builder.
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
	middlewareNames []string          // group middleware (at creation) + per-route middleware, as raw specs
	withoutNames    map[string]bool   // base middleware names excluded for this route specifically
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

// Middleware attaches middleware to this specific route, in addition to
// whatever middleware its enclosing group(s) already contributed. Each
// argument is either a single name ("auth"), a parameterized spec
// ("role:editor,admin"), or a []string of several names — e.g.
// .Middleware("auth") or .Middleware([]string{"web", "auth"}). Names are
// resolved (against the kernel's RouteMiddleware registry, its
// MiddlewareGroups, and finally its service container), filtered by
// WithoutMiddleware, and sorted by MiddlewarePriority at dispatch time —
// see Kernel.resolveRouteMiddleware.
func (r *RouteDefinition) Middleware(names ...any) *RouteDefinition {
	flat := flattenMiddlewareNames(names)
	r.mu.Lock()
	r.middlewareNames = append(r.middlewareNames, flat...)
	r.mu.Unlock()
	return r
}

// WithoutMiddleware excludes middleware — by base name, ignoring any
// parameters — from this route, even if an enclosing group would otherwise
// contribute it. Equivalent to Laravel's ->withoutMiddleware().
func (r *RouteDefinition) WithoutMiddleware(names ...any) *RouteDefinition {
	flat := flattenMiddlewareNames(names)
	r.mu.Lock()
	if r.withoutNames == nil {
		r.withoutNames = make(map[string]bool, len(flat))
	}
	for _, n := range flat {
		r.withoutNames[n] = true
	}
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

func (r *RouteDefinition) withoutMiddlewareCopy() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.withoutNames))
	for k := range r.withoutNames {
		out[k] = true
	}
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

// RouteGroup accumulates shared attributes (URI prefix, middleware specs,
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

// Middleware appends middleware specs to the group, equivalent to
// Route::middleware(). Accepts the same "auth" / "role:editor,admin" /
// []string{"web", "auth"} forms as RouteDefinition.Middleware.
func (g *RouteGroup) Middleware(names ...any) *RouteGroup {
	flat := flattenMiddlewareNames(names)
	c := g.clone()
	c.middleware = append(c.middleware, flat...)
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
// Kernel: Golite's HTTP kernel — global middleware, route (aliased) and
// grouped middleware registries, middleware priority, the route table,
// named routes, and the fallback route.
// ---------------------------------------------------------------------------

// resolvedMiddleware pairs a middleware instance with the parameters (if
// any) it was invoked with, and the base name it was resolved from — the
// name is kept around purely so MiddlewarePriority can sort by it.
type resolvedMiddleware struct {
	name   string
	mw     Middleware
	params []string
}

// Kernel is Golite's HTTP kernel: it owns the global middleware stack, the
// named/grouped route-middleware registries and their priority order, the
// route table, named routes, and the fallback route, and dispatches every
// incoming request through them. It implements http.Handler so it can be
// passed straight to http.ListenAndServe, mirroring Laravel's
// App\Http\Kernel + the Router that sits behind the Route facade.
type Kernel struct {
	container *container.Container

	mu sync.RWMutex

	// GlobalMiddleware runs on every request, in registration order, before
	// routing is resolved — Laravel's $middleware.
	GlobalMiddleware []Middleware

	// RouteMiddleware maps a short alias (e.g. "auth") to the middleware
	// that implements it — Laravel's $routeMiddleware / middleware aliases.
	// Route/group middleware not found here falls back to a lookup on the
	// service container by the same name (see Kernel.Container), so a
	// middleware struct can also just be Bind'd into the container.
	RouteMiddleware map[string]Middleware

	// MiddlewareGroups maps a group name (e.g. "web") to an ordered list of
	// other middleware names — Laravel's $middlewareGroups. Referencing a
	// group name in .Middleware(...) expands to its members (recursively,
	// if a member is itself a group name).
	MiddlewareGroups map[string][]string

	// MiddlewarePriority defines the order non-global middleware run in,
	// regardless of the order they were assigned on a route or pulled in
	// via a group — Laravel's $middlewarePriority. Middleware not listed
	// here run after every listed one, in their original relative order.
	// Intended to be configured once at boot, before the server starts
	// serving requests.
	MiddlewarePriority []string

	routes      []*RouteDefinition
	namedRoutes map[string]*RouteDefinition
	fallback    *RouteDefinition
}

// NewKernel creates a Kernel bound to the given service container.
func NewKernel(c *container.Container) *Kernel {
	k := &Kernel{
		container:        c,
		RouteMiddleware:  make(map[string]Middleware),
		MiddlewareGroups: make(map[string][]string),
		namedRoutes:      make(map[string]*RouteDefinition),
	}
	setDefaultKernel(k)
	return k
}

// Container returns the kernel's service container, so middleware can be
// registered by binding an instance into it (Kernel.RouteMiddleware lookups
// fall back to the container by name) rather than only via AliasMiddleware
// — see the "role"/"audit" middleware in routes/web.go for an example.
func (k *Kernel) Container() *container.Container {
	return k.container
}

// UseMiddleware registers one or more global middleware, executed on every
// request — including ones that end up in the fallback or 404 handler — in
// the order they were added, and always before routing is resolved (so
// middleware like method-spoofing can influence which route matches).
func (k *Kernel) UseMiddleware(middleware ...Middleware) {
	k.mu.Lock()
	k.GlobalMiddleware = append(k.GlobalMiddleware, middleware...)
	k.mu.Unlock()
}

// AliasMiddleware registers a named middleware in the RouteMiddleware
// registry, so routes and groups can reference it by string
// (Route::middleware("auth")) instead of needing a direct Middleware value.
func (k *Kernel) AliasMiddleware(name string, mw Middleware) {
	k.mu.Lock()
	k.RouteMiddleware[name] = mw
	k.mu.Unlock()
}

// MiddlewareGroup defines (or extends, if called again with the same name)
// a named middleware group — Laravel's $middlewareGroups — accepting the
// same "name" / "name:params" / []string forms as Route/RouteGroup
// Middleware.
func (k *Kernel) MiddlewareGroup(name string, members ...any) {
	flat := flattenMiddlewareNames(members)
	k.mu.Lock()
	k.MiddlewareGroups[name] = append(k.MiddlewareGroups[name], flat...)
	k.mu.Unlock()
}

// lookupMiddleware resolves a base middleware name against the
// RouteMiddleware registry first, then falls back to the service
// container — the mechanism behind Kernel.Container's doc comment.
func (k *Kernel) lookupMiddleware(name string) (Middleware, bool) {
	k.mu.RLock()
	mw, ok := k.RouteMiddleware[name]
	k.mu.RUnlock()
	if ok {
		return mw, true
	}

	if k.container == nil {
		return nil, false
	}
	if svc := k.container.Make(name); svc != nil {
		if mw, ok := svc.(Middleware); ok {
			return mw, true
		}
	}
	return nil, false
}

// expandMiddlewareNames replaces any spec whose base name matches a
// MiddlewareGroups entry with that group's members, recursively (so a
// group can itself reference another group), guarding against cycles.
func (k *Kernel) expandMiddlewareNames(specs []string, visiting map[string]bool) []string {
	k.mu.RLock()
	groups := k.MiddlewareGroups
	k.mu.RUnlock()

	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		base, _ := ParseMiddlewareSpec(spec)
		members, isGroup := groups[base]
		if !isGroup || visiting[base] {
			out = append(out, spec)
			continue
		}
		visiting[base] = true
		out = append(out, k.expandMiddlewareNames(members, visiting)...)
		delete(visiting, base)
	}
	return out
}

// sortByPriority stable-sorts resolved middleware according to
// MiddlewarePriority: entries whose base name appears there are ordered by
// that list's index; everything else keeps its original relative order,
// after every prioritized entry.
func (k *Kernel) sortByPriority(list []resolvedMiddleware) {
	k.mu.RLock()
	priority := k.MiddlewarePriority
	k.mu.RUnlock()
	if len(priority) == 0 {
		return
	}

	rank := make(map[string]int, len(priority))
	for i, name := range priority {
		rank[name] = i
	}
	unranked := len(priority)

	sort.SliceStable(list, func(i, j int) bool {
		ri, oki := rank[list[i].name]
		rj, okj := rank[list[j].name]
		if !oki {
			ri = unranked
		}
		if !okj {
			rj = unranked
		}
		return ri < rj
	})
}

// resolveRouteMiddleware expands the route's middleware specs (including
// whatever its enclosing group(s) contributed) through MiddlewareGroups,
// removes anything the route excluded via WithoutMiddleware, resolves each
// remaining spec to an actual Middleware + its parameters, and sorts the
// result by MiddlewarePriority.
func (k *Kernel) resolveRouteMiddleware(route *RouteDefinition) []resolvedMiddleware {
	specs := k.expandMiddlewareNames(route.middlewareNamesCopy(), make(map[string]bool))
	excluded := route.withoutMiddlewareCopy()

	resolved := make([]resolvedMiddleware, 0, len(specs))
	for _, spec := range specs {
		name, params := ParseMiddlewareSpec(spec)
		if excluded[name] {
			continue
		}
		mw, ok := k.lookupMiddleware(name)
		if !ok {
			continue // unresolved middleware name: silently skipped
		}
		resolved = append(resolved, resolvedMiddleware{name: name, mw: mw, params: params})
	}

	k.sortByPriority(resolved)
	return resolved
}

// toHandler wraps a resolved middleware into a HandlerFunc suitable for
// splicing into a Context's handler chain: it records the middleware as
// "executed" (so Kernel.terminate can find it after the response is sent)
// and calls its Handle method, passing Context.Next as the "next" callback.
func toHandler(mw Middleware, params []string) HandlerFunc {
	return func(c *Context) {
		c.executed = append(c.executed, mw)
		mw.Handle(c, c.Next, params...)
	}
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
		withoutNames:    make(map[string]bool),
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
// Route::middleware(...)->group(...). Accepts the same "auth" /
// "role:editor,admin" / []string{"web", "auth"} forms as
// RouteDefinition.Middleware.
func (k *Kernel) Middleware(names ...any) *RouteGroup {
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
// On a match, the route's middleware (expanded, filtered, resolved, and
// priority-sorted by resolveRouteMiddleware) plus the route handler are
// spliced into the same Context's handler chain and executed via a nested
// Next(), keeping the whole request in a single onion-style pipeline.
func (k *Kernel) dispatch(c *Context) {
	route, params, pathMatched, allowed := k.match(c.Request.Method, c.Request.URL.Path)

	if route != nil {
		c.params = params
		for _, rm := range k.resolveRouteMiddleware(route) {
			c.handlers = append(c.handlers, toHandler(rm.mw, rm.params))
		}
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

// terminate runs Terminate on every middleware that actually executed
// during this request and implements TerminableMiddleware, in the order
// they ran — Golite's equivalent of Laravel's Kernel::terminate(), called
// once the response has been fully written.
func (k *Kernel) terminate(c *Context) {
	for _, mw := range c.executed {
		if t, ok := mw.(TerminableMiddleware); ok {
			t.Terminate(c)
		}
	}
}

// ServeHTTP builds the request's middleware chain — every global middleware
// followed by the kernel's own routing dispatch — runs it, and then runs
// Kernel.terminate. This is Golite's front controller, the equivalent of
// Laravel's public/index.php -> Kernel::handle() -> $response->send() ->
// Kernel::terminate().
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	global := make([]Middleware, len(k.GlobalMiddleware))
	copy(global, k.GlobalMiddleware)
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(global)+1)
	for _, mw := range global {
		chain = append(chain, toHandler(mw, nil))
	}
	chain = append(chain, k.dispatch)

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()

	k.terminate(ctx)
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
