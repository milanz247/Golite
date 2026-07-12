package providers

import "Golite/container"

// ServiceProvider is the contract every Golite provider must satisfy,
// mirroring Laravel's Illuminate\Support\ServiceProvider: bindings belong in
// Register, and anything that depends on other providers' bindings belongs
// in Boot.
type ServiceProvider interface {
	Register(c *container.Container)
	Boot(c *container.Container)
}
