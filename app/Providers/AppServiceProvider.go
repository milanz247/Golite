package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	apphttp "Golite/app/Http"
	"Golite/container"
)

// Hasher is a minimal stand-in for Laravel's Hash facade, just enough to
// demonstrate a service resolved out of the container by name.
type Hasher struct{}

// NewHasher creates a new Hasher instance.
func NewHasher() *Hasher {
	return &Hasher{}
}

// Make hashes a value using SHA-256.
func (h *Hasher) Make(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// AppServiceProvider is the Go counterpart to Laravel's AppServiceProvider:
// the default place to bind core, application-wide services into the
// container.
type AppServiceProvider struct{}

// Register binds the dummy "hash" service into the container, and
// registers a sample response macro — Golite's equivalent of Laravel's
// own convention of registering Response::macro(...) calls from
// AppServiceProvider::boot(). "caps" wraps its argument, uppercased, in a
// plain (auto-converted-to-text/html) Response; see
// apphttp.ResponseFactory's doc comment and routes/web.go's "/shout"
// route for how a handler invokes it via c.Macro("caps", ...).
func (p *AppServiceProvider) Register(c *container.Container) {
	c.Bind("hash", NewHasher())

	apphttp.ResponseFactory.Macro("caps", func(val string) *apphttp.Response {
		return apphttp.NewResponse(strings.ToUpper(val))
	})
}

// Boot runs once every provider has had a chance to register its services.
func (p *AppServiceProvider) Boot(c *container.Container) {
	fmt.Println("[AppServiceProvider] booted")
}
