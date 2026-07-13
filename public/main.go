package main

import (
	"fmt"
	stdlog "log"
	"net/http"

	appMiddleware "Golite/app/Http/Middleware"
	"Golite/app/Providers"
	"Golite/bootstrap"
	"Golite/logging"
)

// main is Golite's front controller / entry point, the equivalent of
// Laravel's public/index.php: it boots the application, registers
// providers and global middleware, then starts serving HTTP requests.
func main() {
	app := bootstrap.NewApplication()

	app.Register(&providers.AppServiceProvider{})
	app.Register(&providers.RouteServiceProvider{})

	// Order matters: Recover must be outermost — its deferred recover()
	// only catches panics from middleware/handlers that run *after* it in
	// the chain, so anything registered before it would still crash the
	// connection on panic instead of getting a clean error response (see
	// docs/error-handling.md). Host/proxy trust must be resolved before
	// anything reads the client's address or builds a URL from the Host
	// header; method spoofing must run before routing; input
	// normalization (Trim, then ConvertEmptyStringsToNull) should happen
	// before any handler reads input; Logger runs last so it captures the
	// whole request. TrustHosts is left with no patterns (i.e. disabled)
	// here, since this demo has no fixed production domain — pass real
	// domains in a production deployment.
	app.Kernel.UseMiddleware(
		appMiddleware.Recover(app.Container.Make("log").(logging.Logger), app.Config.App.Debug),
		appMiddleware.NewTrustHosts(),
		appMiddleware.NewTrustProxies("127.0.0.1", "::1"),
		appMiddleware.MethodSpoofing(),
		appMiddleware.TrimStrings(),
		appMiddleware.ConvertEmptyStringsToNull(),
		appMiddleware.Logger(),
	)

	app.Boot()

	fmt.Printf("[%s] Golite is running on %s (%s environment)\n", app.Config.App.Name, app.Config.App.Port, app.Config.App.Env)

	if err := http.ListenAndServe(app.Config.App.Port, app.Kernel); err != nil {
		stdlog.Fatal(err)
	}
}
