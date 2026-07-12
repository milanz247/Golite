package http

import (
	"net/http"
	"reflect"
	"strings"
)

// ---------------------------------------------------------------------------
// Invokable (single-action) controllers
// ---------------------------------------------------------------------------

// Invokable is a single-action controller, analogous to a Laravel
// controller using PHP's __invoke magic method.
type Invokable interface {
	Invoke(c *Context)
}

// InvokableHandler adapts an Invokable controller into a HandlerFunc, so it
// can be registered directly on a route without naming a method — Golite's
// equivalent of Laravel's single-action controller tuple syntax
// (Route::post('/server', ProvisionServerController::class)):
//
//	kernel.POST("/server", apphttp.InvokableHandler(NewProvisionServerController()))
func InvokableHandler(controller Invokable) HandlerFunc {
	return controller.Invoke
}

// InvokeAction is the action name to pass to ApplyControllerMiddleware for
// a single-action controller — PHP's own magic method name, for
// continuity with the Laravel concept this mirrors. A middleware rule
// with no .Only(...)/.Except(...) restriction applies regardless of which
// action name is passed, which covers the common case for invokable
// controllers (there's only ever one action) without callers needing to
// invent their own label.
const InvokeAction = "__invoke"

// ---------------------------------------------------------------------------
// Controller-level middleware
// ---------------------------------------------------------------------------

// ControllerMiddleware is implemented by any controller that declares its
// own per-action middleware — typically by embedding
// controllers.Controller, whose MiddlewareForAction method satisfies this
// automatically. Route::resource/apiResource/singleton check for it when
// registering each action's route; ApplyControllerMiddleware does the same
// for a manually registered (e.g. single-action) route.
type ControllerMiddleware interface {
	MiddlewareForAction(action string) []string
}

// ApplyControllerMiddleware attaches whatever middleware controller
// declared for action (if it implements ControllerMiddleware) onto route,
// and returns route for further chaining. Route::resource/apiResource/
// singleton call this automatically for each generated route; call it
// directly for a controller registered on a plain verb route — see
// InvokableHandler's example.
func ApplyControllerMiddleware(route *RouteDefinition, controller any, action string) *RouteDefinition {
	cm, ok := controller.(ControllerMiddleware)
	if !ok {
		return route
	}
	names := cm.MiddlewareForAction(action)
	if len(names) == 0 {
		return route
	}
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	return route.Middleware(args...)
}

// ---------------------------------------------------------------------------
// Resource action table shared by Resource/ApiResource
// ---------------------------------------------------------------------------

// resourceAction describes one of the 7 standard RESTful actions Laravel's
// Route::resource registers: its action name, HTTP method(s), URI suffix
// relative to the resource's base URI ("{param}" is replaced with the
// resource's singularized parameter name), which Go method
// Route::resource looks for via reflection, and whether apiResource keeps
// it (false for the two HTML-presentation-only actions, create and edit).
type resourceAction struct {
	name             string
	methods          []string
	uriSuffix        string
	controllerMethod string
	includeInAPI     bool
}

var resourceActionTable = []resourceAction{
	{"index", []string{http.MethodGet}, "", "Index", true},
	{"create", []string{http.MethodGet}, "/create", "Create", false},
	{"store", []string{http.MethodPost}, "", "Store", true},
	{"show", []string{http.MethodGet}, "/{param}", "Show", true},
	{"edit", []string{http.MethodGet}, "/{param}/edit", "Edit", false},
	{"update", []string{http.MethodPut, http.MethodPatch}, "/{param}", "Update", true},
	{"destroy", []string{http.MethodDelete}, "/{param}", "Destroy", true},
}

func apiResourceActionTable() []resourceAction {
	out := make([]resourceAction, 0, len(resourceActionTable))
	for _, a := range resourceActionTable {
		if a.includeInAPI {
			out = append(out, a)
		}
	}
	return out
}

// memberActions are the 4 resource actions that operate on one specific
// resource instance (as opposed to index/create/store, which operate on
// the collection) — the ones Shallow promotes to the resource's own
// top-level URI on a nested resource.
var memberActions = map[string]bool{
	"show":    true,
	"edit":    true,
	"update":  true,
	"destroy": true,
}

// ---------------------------------------------------------------------------
// Reflection-based controller method lookup
// ---------------------------------------------------------------------------

var contextPointerType = reflect.TypeOf((*Context)(nil))

// methodHandler returns a HandlerFunc that reflectively calls the named
// method on controller, if — and only if — that method exists with
// exactly the signature func(*Context). This is the mechanism behind
// Route::resource's "automatically inspect the controller" behavior: a
// controller need not implement all 7 actions (or all 5 API actions);
// whichever it's missing are simply skipped rather than registered as a
// route that would panic at request time.
func methodHandler(controller any, methodName string) (HandlerFunc, bool) {
	value := reflect.ValueOf(controller)
	method := value.MethodByName(methodName)
	if !method.IsValid() {
		return nil, false
	}

	methodType := method.Type()
	if methodType.NumIn() != 1 || methodType.NumOut() != 0 || methodType.In(0) != contextPointerType {
		return nil, false
	}

	return func(c *Context) {
		method.Call([]reflect.Value{reflect.ValueOf(c)})
	}, true
}

// ---------------------------------------------------------------------------
// singularize: a deliberately simple English singularizer
// ---------------------------------------------------------------------------

// singularize turns a resource name segment into the parameter name used
// for its {param} route segment (e.g. "photos" -> "photo", "categories"
// -> "category"), mirroring what Laravel derives via Str::singular(). This
// is a small heuristic, not a full English inflector: "ies" -> "y",
// "sses"/"xes"/"ches"/"shes" -> drop "es", a trailing "s" (not "ss") ->
// drop it, otherwise unchanged. Good enough for the overwhelming majority
// of resource names; an irregular plural (e.g. "children") is left as-is
// — see docs/controllers.md for the tradeoff.
func singularize(word string) string {
	switch {
	case strings.HasSuffix(word, "ies") && len(word) > 3:
		return strings.TrimSuffix(word, "ies") + "y"
	case strings.HasSuffix(word, "sses"), strings.HasSuffix(word, "xes"),
		strings.HasSuffix(word, "ches"), strings.HasSuffix(word, "shes"):
		return strings.TrimSuffix(word, "es")
	case strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss"):
		return strings.TrimSuffix(word, "s")
	default:
		return word
	}
}

// ---------------------------------------------------------------------------
// removeRoutes: how Only/Except/Shallow/Creatable re-register without
// leaking stale routes, since Go has no destructor to defer registration
// to the way Laravel's PendingResourceRegistration does.
// ---------------------------------------------------------------------------

// removeRoutes deletes the given routes (by pointer identity) from the
// route table and, if named, from the named-route registry — used by
// ResourceRegistrar/SingletonRegistrar's fluent methods, each of which
// re-registers its full route set from scratch on every call (Only,
// Except, Shallow, Creatable), first removing whatever it registered last
// time.
func (k *Kernel) removeRoutes(routes []*RouteDefinition) {
	if len(routes) == 0 {
		return
	}
	remove := make(map[*RouteDefinition]bool, len(routes))
	for _, r := range routes {
		remove[r] = true
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	filtered := make([]*RouteDefinition, 0, len(k.routes))
	for _, r := range k.routes {
		if !remove[r] {
			filtered = append(filtered, r)
		}
	}
	k.routes = filtered

	for name, r := range k.namedRoutes {
		if remove[r] {
			delete(k.namedRoutes, name)
		}
	}
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ResourceRegistrar: Route::resource / Route::apiResource
// ---------------------------------------------------------------------------

// ResourceRegistrar builds and holds the routes for a Route::resource or
// Route::apiResource call, letting Only/Except/Shallow be chained
// afterward — Golite's equivalent of Laravel's PendingResourceRegistration.
// Since Go has no destructor to defer registration to, each fluent method
// here re-registers the full route set immediately (first removing
// whatever the previous call registered), rather than deferring to some
// later "commit" step.
type ResourceRegistrar struct {
	kernel     *Kernel
	name       string // dotted resource name, e.g. "photos.comments"
	controller any

	prefix     string // inherited from an enclosing RouteGroup, if any
	namePrefix string
	middleware []string

	table   []resourceAction // the full candidate set: 7 for Resource, 5 for ApiResource
	only    []string
	except  []string
	shallow bool

	routes []*RouteDefinition
}

func newResourceRegistrar(k *Kernel, name string, controller any, table []resourceAction, prefix, namePrefix string, middleware []string) *ResourceRegistrar {
	r := &ResourceRegistrar{
		kernel:     k,
		name:       name,
		controller: controller,
		prefix:     prefix,
		namePrefix: namePrefix,
		middleware: middleware,
		table:      table,
	}
	r.register()
	return r
}

// Only restricts the registered routes to the given action names (e.g.
// []string{"index", "show"}).
func (r *ResourceRegistrar) Only(actions []string) *ResourceRegistrar {
	r.only = actions
	r.except = nil
	r.register()
	return r
}

// Except restricts the registered routes to every action *except* the
// given names (e.g. []string{"destroy"}).
func (r *ResourceRegistrar) Except(actions []string) *ResourceRegistrar {
	r.except = actions
	r.only = nil
	r.register()
	return r
}

// Shallow promotes this resource's "member" actions (show, edit, update,
// destroy — the ones addressing one specific child by its own ID) to the
// resource's own top-level URI instead of nesting them under the parent,
// since a child's ID is already globally unique and doesn't need the
// parent's ID to disambiguate it. Only meaningful for a nested resource
// (a dotted name like "photos.comments"); a harmless no-op otherwise.
func (r *ResourceRegistrar) Shallow() *ResourceRegistrar {
	r.shallow = true
	r.register()
	return r
}

func (r *ResourceRegistrar) allowedActions() []resourceAction {
	var allowed []resourceAction
	for _, action := range r.table {
		if len(r.only) > 0 && !containsString(r.only, action.name) {
			continue
		}
		if len(r.except) > 0 && containsString(r.except, action.name) {
			continue
		}
		allowed = append(allowed, action)
	}
	return allowed
}

func (r *ResourceRegistrar) register() {
	r.kernel.removeRoutes(r.routes)

	segments := strings.Split(r.name, ".")
	last := segments[len(segments)-1]
	paramName := singularize(last)

	var parentPrefixBuilder strings.Builder
	for _, seg := range segments[:len(segments)-1] {
		parentPrefixBuilder.WriteString("/")
		parentPrefixBuilder.WriteString(seg)
		parentPrefixBuilder.WriteString("/{")
		parentPrefixBuilder.WriteString(singularize(seg))
		parentPrefixBuilder.WriteString("}")
	}
	nestedBase := parentPrefixBuilder.String() + "/" + last
	shallowBase := "/" + last

	routes := make([]*RouteDefinition, 0, len(r.table))
	for _, action := range r.allowedActions() {
		handler, ok := methodHandler(r.controller, action.controllerMethod)
		if !ok {
			continue
		}

		base := nestedBase
		if r.shallow && memberActions[action.name] {
			base = shallowBase
		}
		uri := base + strings.ReplaceAll(action.uriSuffix, "{param}", "{"+paramName+"}")

		route := r.kernel.addRoute(action.methods, uri, handler, r.prefix, r.namePrefix, r.middleware)
		route.Name(r.name + "." + action.name)
		ApplyControllerMiddleware(route, r.controller, action.name)

		routes = append(routes, route)
	}
	r.routes = routes
}

// Resource registers the 7 standard RESTful routes for a controller —
// Index, Create, Store, Show, Edit, Update, Destroy — equivalent to
// Laravel's Route::resource($uri, $controller). name may use dot notation
// for a nested resource ("photos.comments" -> routes under
// /photos/{photo}/comments/...); only controller methods that actually
// exist (checked via reflection) are registered.
func (k *Kernel) Resource(name string, controller any) *ResourceRegistrar {
	return newResourceRegistrar(k, name, controller, resourceActionTable, "", "", nil)
}

// ApiResource registers the 5 API-appropriate RESTful routes for a
// controller — Index, Store, Show, Update, Destroy — omitting Create and
// Edit, which only exist to serve HTML forms. Equivalent to Laravel's
// Route::apiResource($uri, $controller).
func (k *Kernel) ApiResource(name string, controller any) *ResourceRegistrar {
	return newResourceRegistrar(k, name, controller, apiResourceActionTable(), "", "", nil)
}

// Resource registers a resource's routes with this group's prefix, name
// prefix, and middleware applied — equivalent to nesting
// Route::resource(...) inside a Route::prefix(...)->group(...) closure.
func (g *RouteGroup) Resource(name string, controller any) *ResourceRegistrar {
	return newResourceRegistrar(g.kernel, name, controller, resourceActionTable, g.prefix, g.namePrefix, g.middleware)
}

// ApiResource is ApiResource scoped to this group's prefix, name prefix,
// and middleware — see RouteGroup.Resource.
func (g *RouteGroup) ApiResource(name string, controller any) *ResourceRegistrar {
	return newResourceRegistrar(g.kernel, name, controller, apiResourceActionTable(), g.prefix, g.namePrefix, g.middleware)
}

// ---------------------------------------------------------------------------
// SingletonRegistrar: Route::singleton
// ---------------------------------------------------------------------------

var singletonActionTable = []resourceAction{
	{"show", []string{http.MethodGet}, "", "Show", true},
	{"edit", []string{http.MethodGet}, "/edit", "Edit", false},
	{"update", []string{http.MethodPut, http.MethodPatch}, "", "Update", true},
}

var singletonCreatableTable = []resourceAction{
	{"create", []string{http.MethodGet}, "/create", "Create", false},
	{"store", []string{http.MethodPost}, "", "Store", true},
}

// SingletonRegistrar builds and holds the routes for a Route::singleton
// call, mirroring ResourceRegistrar but for a resource with exactly one
// instance (no {id} segment) — e.g. the current user's profile.
type SingletonRegistrar struct {
	kernel     *Kernel
	name       string
	controller any
	creatable  bool
	routes     []*RouteDefinition
}

// Creatable additionally registers Create and Store routes on this
// singleton resource, equivalent to Laravel's ->creatable().
func (r *SingletonRegistrar) Creatable() *SingletonRegistrar {
	r.creatable = true
	r.register()
	return r
}

func (r *SingletonRegistrar) register() {
	r.kernel.removeRoutes(r.routes)

	actions := singletonActionTable
	if r.creatable {
		actions = append(append([]resourceAction{}, singletonCreatableTable...), singletonActionTable...)
	}

	routes := make([]*RouteDefinition, 0, len(actions))
	for _, action := range actions {
		handler, ok := methodHandler(r.controller, action.controllerMethod)
		if !ok {
			continue
		}
		route := r.kernel.addRoute(action.methods, "/"+r.name+action.uriSuffix, handler, "", "", nil)
		route.Name(r.name + "." + action.name)
		ApplyControllerMiddleware(route, r.controller, action.name)
		routes = append(routes, route)
	}
	r.routes = routes
}

// Singleton registers routes for a resource with exactly one instance —
// Show, Edit, Update, with no {id} segment — equivalent to Laravel's
// Route::singleton($uri, $controller). Chain .Creatable() to also
// register Create and Store.
func (k *Kernel) Singleton(name string, controller any) *SingletonRegistrar {
	r := &SingletonRegistrar{kernel: k, name: name, controller: controller}
	r.register()
	return r
}
