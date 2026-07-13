package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// ValidationController demonstrates the validation package (see
// docs/validation.md): a realistic "register" endpoint using
// Context.Validate, which panics with a *validation.Exception on failure
// — caught by RecoverMiddleware and rendered as an automatic 422 response,
// mirroring Laravel's $request->validate($rules).
type ValidationController struct {
	Controller
}

// NewValidationController constructs a ValidationController. It takes no
// dependencies — validation itself needs nothing beyond the request's own
// input, resolved through Context.Validate.
func NewValidationController() *ValidationController {
	return &ValidationController{}
}

// Register handles POST /register.
func (vc *ValidationController) Register(c *apphttp.Context) {
	validated := c.Validate(map[string]string{
		"name":     "required|string|min:2",
		"email":    "required|email",
		"password": "required|min:6|confirmed",
	})
	c.JSON(http.StatusOK, map[string]any{"status": "registered", "user": validated})
}
