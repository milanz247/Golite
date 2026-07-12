package main

import (
	"fmt"
	"log"
	"net/http"

	appMiddleware "Golite/app/Http/Middleware"
	"Golite/app/Providers"
	"Golite/bootstrap"
)

// main is Golite's front controller / entry point, the equivalent of
// Laravel's public/index.php: it boots the application, registers
// providers and global middleware, then starts serving HTTP requests.
func main() {
	app := bootstrap.NewApplication()

	app.Register(&providers.AppServiceProvider{})
	app.Register(&providers.RouteServiceProvider{})

	app.Kernel.UseMiddleware(appMiddleware.MethodSpoofing(), appMiddleware.Logger())

	app.Boot()

	fmt.Printf("[%s] Golite is running on %s (%s environment)\n", app.Config.App.Name, app.Config.App.Port, app.Config.App.Env)

	if err := http.ListenAndServe(app.Config.App.Port, app.Kernel); err != nil {
		log.Fatal(err)
	}
}
