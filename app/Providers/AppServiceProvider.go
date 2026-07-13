package providers

import (
	"fmt"
	"strings"

	apphttp "Golite/app/Http"
	"Golite/config"
	"Golite/container"
	"Golite/encryption"
	"Golite/hashing"
	"Golite/logging"
)

// AppServiceProvider is the Go counterpart to Laravel's AppServiceProvider:
// the default place to bind core, application-wide services into the
// container.
type AppServiceProvider struct{}

// Register binds Golite's core application services into the container —
// "hash" (hashing.Manager, bcrypt by default; see docs/hashing.md),
// "encrypter" (encryption.Encrypter; see docs/encryption.md), and "log"
// (logging.Manager; see docs/logging.md) — reading their configuration
// from whatever AppServiceProvider was registered after (bootstrap.
// Application.Register already bound "config" before any provider runs;
// see bootstrap/app.go). It also registers a sample response macro —
// Golite's equivalent of Laravel's own convention of registering
// Response::macro(...) calls from AppServiceProvider::boot().
func (p *AppServiceProvider) Register(c *container.Container) {
	cfg := c.Make("config").(*config.Config)

	hasher := hashing.NewManager(cfg.Hash.Driver)
	hasher.Extend("bcrypt", hashing.NewBcryptHasher(cfg.Hash.BcryptCost))
	c.Bind("hash", hasher)

	c.Bind("encrypter", encryption.NewEncrypter(cfg.App.Key))

	logger := logging.NewManager(cfg.Log.Channel)
	logger.Extend("single", logging.NewSingleChannel(cfg.Log.Path))
	logger.Extend("daily", logging.NewDailyChannel(logDirectory(cfg.Log.Path), "golite", cfg.Log.Days))
	logger.Extend("stack", logging.NewStackChannel(logging.NewSingleChannel(cfg.Log.Path)))
	c.Bind("log", logger)

	apphttp.ResponseFactory.Macro("caps", func(val string) *apphttp.Response {
		return apphttp.NewResponse(strings.ToUpper(val))
	})
}

// logDirectory returns path's parent directory, used to root the "daily"
// channel's date-suffixed files alongside the "single" channel's file.
func logDirectory(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return "."
}

// Boot runs once every provider has had a chance to register its services.
func (p *AppServiceProvider) Boot(c *container.Container) {
	fmt.Println("[AppServiceProvider] booted")
}
