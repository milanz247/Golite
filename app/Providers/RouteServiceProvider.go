package providers

import (
	"fmt"

	apphttp "Golite/app/Http"
	"Golite/container"
	"Golite/routes"
)

// RouteServiceProvider loads the routing engine, mirroring Laravel's
// RouteServiceProvider, which maps route files (routes/web.php) onto the
// router.
type RouteServiceProvider struct{}

// Register has nothing of its own to bind; routes are mapped during Boot so
// every other provider has already had a chance to register its services.
func (p *RouteServiceProvider) Register(c *container.Container) {}

// Boot resolves the Kernel from the container and maps the web routes onto
// it.
func (p *RouteServiceProvider) Boot(c *container.Container) {
	kernel := c.Make("kernel").(*apphttp.Kernel)
	routes.MapWebRoutes(kernel)
	fmt.Println("[RouteServiceProvider] web routes mapped")
}
