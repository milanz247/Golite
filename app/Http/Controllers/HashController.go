package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// HashController demonstrates the hashing package (see docs/hashing.md):
// Hash::make/Hash::check-equivalent endpoints via the same constructor-
// injected Hasher interface PostController already uses.
type HashController struct {
	Controller
	hasher Hasher
}

// NewHashController constructs a HashController with an injected Hasher,
// resolved from the container's "hash" binding in routes/web.go.
func NewHashController(hasher Hasher) *HashController {
	return &HashController{hasher: hasher}
}

// Make handles POST /hash/make, hashing the given password.
func (hc *HashController) Make(c *apphttp.Context) {
	password, _ := c.Input("password", "").(string)
	if password == "" {
		c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "password is required"})
		return
	}
	c.JSON(http.StatusOK, map[string]string{"hash": hc.hasher.Make(password)})
}

// Check handles POST /hash/check, verifying a candidate password against a
// previously-made hash.
func (hc *HashController) Check(c *apphttp.Context) {
	password, _ := c.Input("password", "").(string)
	hash, _ := c.Input("hash", "").(string)
	c.JSON(http.StatusOK, map[string]any{"matches": hc.hasher.Check(password, hash)})
}
