package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// ProfileController demonstrates Route::singleton: a resource with
// exactly one instance per (implicit) subject — here, "the current
// user's profile" — so its routes carry no {id} segment.
type ProfileController struct {
	Controller
}

// NewProfileController constructs a ProfileController requiring "auth" on
// every action.
func NewProfileController() *ProfileController {
	c := &ProfileController{}
	c.Middleware("auth")
	return c
}

func (p *ProfileController) Show(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "profile.show"})
}

func (p *ProfileController) Edit(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "profile.edit"})
}

func (p *ProfileController) Update(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "profile.update"})
}

// Create and Store only get registered if Singleton(...).Creatable() is
// used — see routes/web.go.
func (p *ProfileController) Create(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "profile.create"})
}

func (p *ProfileController) Store(c *apphttp.Context) {
	c.JSON(http.StatusCreated, map[string]any{"action": "profile.store"})
}
