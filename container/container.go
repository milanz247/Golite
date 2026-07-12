package container

import "sync"

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
