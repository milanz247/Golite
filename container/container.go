package container

import "sync"

type Container struct {
	mu       sync.RWMutex
	services map[string]interface{}
}

func New() *Container {
	return &Container{
		services: make(map[string]interface{}),
	}
}

func (c *Container) Bind(name string, service interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.services[name] = service
}

func (c *Container) Make(name string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.services[name]
}