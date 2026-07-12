package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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

// Register binds the dummy "hash" service into the container.
func (p *AppServiceProvider) Register(c *container.Container) {
	c.Bind("hash", NewHasher())
}

// Boot runs once every provider has had a chance to register its services.
func (p *AppServiceProvider) Boot(c *container.Container) {
	fmt.Println("[AppServiceProvider] booted")
}
