package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// ProvisionServerController is a single-action (invokable) controller —
// Golite's equivalent of a Laravel controller using __invoke. It
// implements apphttp.Invokable instead of exposing a named action method,
// and is registered via apphttp.InvokableHandler rather than
// Route::resource/apiResource/singleton.
type ProvisionServerController struct {
	Controller
}

// NewProvisionServerController constructs a ProvisionServerController
// requiring "auth".
func NewProvisionServerController() *ProvisionServerController {
	c := &ProvisionServerController{}
	c.Middleware("auth")
	return c
}

// Invoke handles the controller's one and only action.
func (p *ProvisionServerController) Invoke(c *apphttp.Context) {
	c.JSON(http.StatusAccepted, map[string]any{"status": "provisioning started"})
}
