package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/config"
)

// hashService is a minimal local contract describing whatever hasher the
// container has bound under the "hash" key. Go's structural typing lets us
// consume it without importing the providers package, which keeps
// controllers -> providers from ever becoming an import cycle.
type hashService interface {
	Make(value string) string
}

// UserController groups user-facing endpoints, analogous to Laravel's
// App\Http\Controllers\UserController. It embeds the base Controller (see
// Controller.go) purely for consistency with the rest of the framework's
// controllers — it doesn't declare any middleware of its own.
type UserController struct {
	Controller
}

// NewUserController creates a new UserController.
func NewUserController() *UserController {
	return &UserController{}
}

// Show resolves the "hash" service from the container and returns a JSON
// payload containing the app configuration and a sample user, the same way
// a Laravel controller would pull dependencies out of the service
// container.
func (u *UserController) Show(c *apphttp.Context) {
	hasher := c.App.Make("hash").(hashService)
	cfg := c.App.Make("config").(*config.Config)

	c.JSON(http.StatusOK, map[string]any{
		"app": cfg.App,
		"user": map[string]string{
			"id":    "1",
			"name":  "Jane Doe",
			"email": "jane@example.com",
			"token": hasher.Make("jane@example.com"),
		},
	})
}
