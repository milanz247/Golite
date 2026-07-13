package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
	"Golite/config"
)

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

// Show returns a JSON payload containing the app configuration and a
// sample user. hash and cfg are resolved automatically from the service
// container by apphttp.Inject (see routes/web.go) — Golite's equivalent
// of Laravel's automatic method injection:
//
//	public function show(Hasher $hash, Repository $config) { ... }
func (u *UserController) Show(c *apphttp.Context, hash Hasher, cfg *config.Config) {
	c.JSON(http.StatusOK, map[string]any{
		"app": cfg.App,
		"user": map[string]string{
			"id":    "1",
			"name":  "Jane Doe",
			"email": "jane@example.com",
			"token": hash.Make("jane@example.com"),
		},
	})
}
