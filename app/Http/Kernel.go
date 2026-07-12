package http

import (
	"encoding/json"
	"net/http"
	"sync"

	"Golite/container"
)

// HandlerFunc is Golite's request handler signature, analogous to Laravel's
// Closure-based route actions and middleware.
type HandlerFunc func(*Context)

// Context wraps the request/response pair together with the application's
// service container (Laravel's Application itself extends Container, so
// resolving services through App mirrors Laravel's `app()->make(...)`),
// giving handlers access to bound services and a middleware pipeline.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	App     *container.Container

	handlers []HandlerFunc
	index    int
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

// Next advances the middleware/handler chain. A middleware that wants to run
// code after the rest of the chain finishes should call Next and then
// continue below it, just like Laravel's pipeline.
func (c *Context) Next() {
	c.index++
	for c.index < len(c.handlers) {
		c.handlers[c.index](c)
		c.index++
	}
}

// JSON writes a JSON response with the given status code.
func (c *Context) JSON(status int, payload any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(status)
	_ = json.NewEncoder(c.Writer).Encode(payload)
}

// Kernel is Golite's HTTP kernel: it owns the global middleware stack and
// the route table, and dispatches every incoming request through them. It
// implements http.Handler so it can be passed straight to
// http.ListenAndServe, mirroring Laravel's App\Http\Kernel.
type Kernel struct {
	container  *container.Container
	middleware []HandlerFunc

	mu     sync.RWMutex
	routes map[string]HandlerFunc
}

// NewKernel creates a Kernel bound to the given service container.
func NewKernel(c *container.Container) *Kernel {
	return &Kernel{
		container: c,
		routes:    make(map[string]HandlerFunc),
	}
}

// UseMiddleware registers one or more global middleware, executed on every
// request in the order they were added.
func (k *Kernel) UseMiddleware(middleware ...HandlerFunc) {
	k.middleware = append(k.middleware, middleware...)
}

// GET registers a handler for GET requests on the given path.
func (k *Kernel) GET(path string, handler HandlerFunc) {
	k.Handle(http.MethodGet, path, handler)
}

// POST registers a handler for POST requests on the given path.
func (k *Kernel) POST(path string, handler HandlerFunc) {
	k.Handle(http.MethodPost, path, handler)
}

// Handle registers a handler for an arbitrary HTTP method and path.
func (k *Kernel) Handle(method, path string, handler HandlerFunc) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.routes[routeKey(method, path)] = handler
}

func routeKey(method, path string) string {
	return method + " " + path
}

// ServeHTTP resolves the matching route (falling back to a 404 JSON
// response), builds the middleware + handler chain, and dispatches the
// request — Golite's equivalent of Laravel's front controller.
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k.mu.RLock()
	handler, ok := k.routes[routeKey(r.Method, r.URL.Path)]
	k.mu.RUnlock()

	chain := make([]HandlerFunc, 0, len(k.middleware)+1)
	chain = append(chain, k.middleware...)

	if ok {
		chain = append(chain, handler)
	} else {
		chain = append(chain, func(c *Context) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "404 not found"})
		})
	}

	ctx := newContext(w, r, k.container, chain)
	ctx.Next()
}
