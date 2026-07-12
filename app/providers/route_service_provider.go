package providers

import (
	"fmt"
	"golite/container"
	"golite/routes"
)

type RouteServiceProvider struct {
	KernelInterface interface{} // Router එකට access ලබා ගැනීමට
}

func (p *RouteServiceProvider) Register(c *container.Container) {
	// Routes ලියාපදිංචි කිරීමේ මූලික පියවර
}

func (p *RouteServiceProvider) Boot(c *container.Container) {
	// HTTP Kernel එක හරහා Routes load කිරීම
	kernel := c.Make("kernel").(interface {
		GET(path string, handler func(*http.Context))
	})
	
	// routes/web.go හි ඇති routes load කර දීම
	routes.MapWebRoutes(kernel)
	fmt.Println("[Service Provider] Web Routes Successfully Mapped")
}