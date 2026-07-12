package bootstrap

import (
	"golite/app/http"
	"golite/container"
	"golite/app/providers"
)

type Application struct {
	Container *container.Container
	Providers []providers.ServiceProvider
}

func NewApplication() *Application {
	app := &Application{
		Container: container.New(),
		Providers: []providers.ServiceProvider{},
	}

	// Core Kernel එක Container එකට Bind කිරීම
	kernel := http.NewKernel(app.Container)
	app.Container.Bind("kernel", kernel)

	return app
}

// Service Providers ලියාපදිංචි කිරීම
func (app *Application) RegisterProvider(p providers.ServiceProvider) {
	app.Providers = append(app.Providers, p)
	p.Register(app.Container)
}

// සර්වර් එක Run වීමට ප්‍රථම Providers සක්‍රීය කිරීම
func (app *Application) BootProviders() {
	for _, p := range app.Providers {
		p.Boot(app.Container)
	}
}