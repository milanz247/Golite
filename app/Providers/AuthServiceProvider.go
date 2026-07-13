package providers

import (
	"log"

	"gorm.io/gorm"

	"Golite/auth"
	"Golite/container"
	"Golite/hashing"
)

// AuthServiceProvider binds Golite's authentication engine — an
// *auth.Guard, wrapping session-based register/login/logout plus
// remember-me and password-reset support — into the container under
// "auth". See docs/authentication.md.
type AuthServiceProvider struct{}

// Register builds the Guard from the already-bound "db" and "hash"
// services (register this provider after both AppServiceProvider and
// DatabaseServiceProvider — see public/main.go). Like
// DatabaseServiceProvider, a missing dependency here is non-fatal: if
// MySQL wasn't reachable at boot, "db" is simply never bound, and there
// is nothing a Guard could meaningfully do without one — so Register
// logs a warning and leaves "auth" unbound too, rather than panicking.
// routes/web.go checks for exactly this before registering any auth
// route (see registerAuthRoutes).
func (p *AuthServiceProvider) Register(c *container.Container) {
	db, ok := c.Make("db").(*gorm.DB)
	if !ok {
		log.Println(`[AuthServiceProvider] "db" is unavailable — "auth" service not bound (see DatabaseServiceProvider)`)
		return
	}
	hasher, ok := c.Make("hash").(hashing.Hasher)
	if !ok {
		log.Println(`[AuthServiceProvider] "hash" is unavailable — "auth" service not bound`)
		return
	}
	c.Bind("auth", auth.NewGuard(db, hasher))
}

// Boot does nothing — the guard is already usable once Register returns.
func (p *AuthServiceProvider) Boot(c *container.Container) {}
