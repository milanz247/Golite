package bootstrap

import (
	apphttp "Golite/app/Http"
	"Golite/app/Providers"
	"Golite/config"
	"Golite/container"
)

// Application is Golite's core, analogous to Laravel's Illuminate\Foundation\Application:
// it wires together the service container, configuration, and service
// providers, then drives the Register -> Boot lifecycle before the server
// starts.
type Application struct {
	Container *container.Container
	Config    *config.Config
	Kernel    *apphttp.Kernel

	providers []providers.ServiceProvider
}

// NewApplication boots the container, loads configuration from .env, and
// prepares the HTTP kernel — the Go equivalent of Laravel's
// bootstrap/app.php.
func NewApplication() *Application {
	c := container.New()
	cfg := config.LoadConfig()
	kernel := apphttp.NewKernel(c)

	c.Bind("config", cfg)
	c.Bind("kernel", kernel)

	return &Application{
		Container: c,
		Config:    cfg,
		Kernel:    kernel,
	}
}

// Register adds a service provider and immediately invokes its Register
// method, letting it bind services into the container right away.
func (app *Application) Register(p providers.ServiceProvider) {
	app.providers = append(app.providers, p)
	p.Register(app.Container)
}

// Boot runs every registered provider's Boot method. This must happen right
// before the HTTP server starts serving requests, mirroring Laravel's
// provider boot phase.
func (app *Application) Boot() {
	for _, p := range app.providers {
		p.Boot(app.Container)
	}
}
