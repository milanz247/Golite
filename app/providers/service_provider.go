package providers

import "golite/container"

type ServiceProvider interface {
	Register(c *container.Container)
	Boot(c *container.Container)
}