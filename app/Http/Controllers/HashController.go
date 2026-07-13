package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// HashController demonstrates the hashing package (see docs/hashing.md):
// Hash::make/Hash::check-equivalent endpoints via a method-injected
// Hasher, resolved automatically by apphttp.Inject (see routes/web.go).
type HashController struct {
	Controller
}

// NewHashController creates a new HashController.
func NewHashController() *HashController {
	return &HashController{}
}

// Make handles POST /hash/make, hashing the given password. hasher is
// resolved automatically from the container — Golite's equivalent of a
// Laravel action type-hinting Hasher $hash.
func (hc *HashController) Make(c *apphttp.Context, hasher Hasher) {
	password, _ := c.Input("password", "").(string)
	if password == "" {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "password is required"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"hash": hasher.Make(password)})
}

// Check handles POST /hash/check, verifying a candidate password against a
// previously-made hash.
func (hc *HashController) Check(c *apphttp.Context, hasher Hasher) {
	password, _ := c.Input("password", "").(string)
	hash, _ := c.Input("hash", "").(string)
	c.JSON(http.StatusOK, map[string]any{"matches": hasher.Check(password, hash)})
}
