package controllers

import (
	"net/http"

	apphttp "Golite/app/Http"
)

// CommentController demonstrates nested resource routing:
// Route::resource("photos.comments", ...) nests every route under
// /photos/{photo}/comments/..., or — combined with .Shallow() — nests only
// the collection actions (Index/Store) and promotes the member actions
// (Show/Update/Destroy) to /comments/{comment} directly, since a
// comment's own ID is already globally unique.
type CommentController struct {
	Controller
}

// NewCommentController constructs a CommentController with no declared
// middleware, to keep the nesting/shallow-routing demo focused.
func NewCommentController() *CommentController {
	return &CommentController{}
}

func (cc *CommentController) Index(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "comments.index", "photo": c.Param("photo")})
}

func (cc *CommentController) Store(c *apphttp.Context) {
	c.JSON(http.StatusCreated, map[string]any{"action": "comments.store", "photo": c.Param("photo")})
}

func (cc *CommentController) Show(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "comments.show", "comment": c.Param("comment")})
}

func (cc *CommentController) Update(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]any{"action": "comments.update", "comment": c.Param("comment")})
}

func (cc *CommentController) Destroy(c *apphttp.Context) {
	c.Writer.WriteHeader(http.StatusNoContent)
}
