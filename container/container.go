package container

import (
	"reflect"
	"sync"
)

// Container is Golite's thread-safe service registry, the Go equivalent of
// Laravel's IoC container. Anything bound via Bind can later be resolved by
// name via Make, from any goroutine.
type Container struct {
	mu       sync.RWMutex
	services map[string]any
}

// New creates an empty Container.
func New() *Container {
	return &Container{
		services: make(map[string]any),
	}
}

// Bind registers a service under the given name, overwriting any existing
// binding of the same name.
func (c *Container) Bind(name string, service any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services[name] = service
}

// Make resolves a previously bound service by name. It returns nil if no
// service has been bound under that name.
func (c *Container) Make(name string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.services[name]
}

// ResolveType returns the first bound service whose concrete type is
// assignable to t, and true — or nil, false if none qualifies. This is
// what makes Laravel-style automatic method injection possible in Go
// (see apphttp.Inject in app/Http/Injection.go): a controller action can
// type-hint an interface or concrete type as a parameter and have it
// resolved by *shape*, exactly like Laravel resolves a type-hinted
// $hash: Hasher from whatever concrete class was bound to that contract,
// without either side needing to agree on a string key.
//
// If more than one bound service is assignable to t, which one is
// returned is unspecified (map iteration order) — bind only one
// implementation per distinct interface/type shape, or fall back to
// explicit constructor injection (as PostController does) when a
// controller genuinely needs to disambiguate between several.
func (c *Container) ResolveType(t reflect.Type) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, service := range c.services {
		if reflect.TypeOf(service).AssignableTo(t) {
			return service, true
		}
	}
	return nil, false
}
