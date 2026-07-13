package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// Hasher is the dependency any controller in this package takes via
// constructor injection for the container's "hash" service — resolved as
// kernel.Container().Make("hash").(controllers.Hasher) in routes/web.go.
// *hashing.Manager (see docs/hashing.md) satisfies it structurally, so
// this package never needs to import "Golite/hashing" itself. PostController
// only calls Make; HashController (HashController.go) uses the full
// interface to demonstrate Hash::check/needsRehash as well.
type Hasher interface {
	Make(value string) string
	Check(value, hashedValue string) bool
	NeedsRehash(hashedValue string) bool
}

// PostController is a full resource controller. It deliberately does not
// implement Create or Edit — the two HTML-presentation-only actions — to
// demonstrate Route::resource's reflection-based "only register what the
// controller actually implements" behavior: registering this controller
// with Route::resource (not just ApiResource) still only wires up Index,
// Store, Show, Update, and Destroy.
type PostController struct {
	Controller
	hasher Hasher
}

// NewPostController constructs a PostController with an injected Hasher,
// and declares that every action except Index and Show requires the
// "auth" middleware — Golite's equivalent of Laravel's
// $this->middleware('auth')->except(['index', 'show']).
func NewPostController(hasher Hasher) *PostController {
	c := &PostController{hasher: hasher}
	c.Middleware("auth").Except("index", "show")
	return c
}

func (p *PostController) Index(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "posts.index"})
}

func (p *PostController) Show(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "posts.show", "post": c.Param("post")})
}

func (p *PostController) Store(c *apphttp.Context) {
	title, _ := c.Input("title", "").(string)
	c.JSON(http.StatusCreated, map[string]any{
		"action": "posts.store",
		"title":  title,
		"token":  p.hasher.Make(title),
	})
}

func (p *PostController) Update(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "posts.update", "post": c.Param("post")})
}

func (p *PostController) Destroy(c *apphttp.Context) {
	c.Writer.WriteHeader(http.StatusNoContent)
}
