package http

import (
	"fmt"
	"reflect"

	"Golite/container"
)

// contextType is the reflect.Type of *Context, used to validate that every
// Inject-wrapped handler takes one as its first parameter.
var contextType = reflect.TypeOf((*Context)(nil))

// Inject adapts handler — a function whose first parameter is *Context and
// whose remaining parameters are anything the container can resolve by
// type (see Container.ResolveType) — into an ordinary HandlerFunc.
//
// This is Golite's equivalent of Laravel's automatic controller method
// injection: instead of a handler pulling its dependencies out of the
// container by name and type-asserting them itself, it just declares what
// it needs as parameters, the same way a Laravel action type-hints
// Hasher $hash:
//
//	// Laravel
//	public function show(Hasher $hash, Repository $config) { ... }
//
//	// Golite
//	func (u *UserController) Show(c *apphttp.Context, hash Hasher, cfg *config.Config) { ... }
//
// handler is typically a bound controller method value:
//
//	kernel.GET("/user", apphttp.Inject(kernel.Container(), userController.Show))
//
// Each parameter after *Context is resolved fresh on every call via
// Container.ResolveType — the same "resolved per request" behavior
// Laravel's container has — so Inject panics immediately (at route
// registration, via reflect on handler's signature) if the first
// parameter isn't *Context, and panics per-request if some later
// parameter type has nothing assignable to it bound in the container; both
// are configuration errors worth failing loudly on rather than silently
// passing a zero value. See docs/controllers.md#method-injection.
func Inject(c *container.Container, handler any) HandlerFunc {
	v := reflect.ValueOf(handler)
	t := v.Type()

	if t.Kind() != reflect.Func {
		panic("golite: Inject requires a function value, got " + t.Kind().String())
	}
	if t.NumIn() == 0 || t.In(0) != contextType {
		panic("golite: Inject: handler's first parameter must be *http.Context")
	}

	numIn := t.NumIn()
	paramTypes := make([]reflect.Type, numIn)
	for i := 0; i < numIn; i++ {
		paramTypes[i] = t.In(i)
	}

	return func(ctx *Context) {
		args := make([]reflect.Value, numIn)
		args[0] = reflect.ValueOf(ctx)
		for i := 1; i < numIn; i++ {
			service, ok := c.ResolveType(paramTypes[i])
			if !ok {
				panic(fmt.Sprintf("golite: Inject: no service bound in the container is assignable to parameter %d (%s) of %s", i, paramTypes[i], t))
			}
			args[i] = reflect.ValueOf(service)
		}
		v.Call(args)
	}
}
