package controllers

import apphttp "Golite/app/Http"

// Index renders the welcome page: c.View sends resources/views/welcome.html
// as the response. apphttp.H is a map[string]any shorthand for view data;
// a struct, or c.Set + c.View("welcome") with no data argument, work just
// as well — see Context.View.
//
// Unlike every other controller in this package, Index is a plain
// package-level function rather than a method on a struct that embeds
// Controller — for a handler this simple (no dependencies, no per-action
// middleware), there's nothing a struct would add. It's still a
// perfectly ordinary apphttp.HandlerFunc, so it registers exactly like
// any other: kernel.GET("/welcome", controllers.Index). Reach for the
// struct-based style (see PostController, UserController, ...) once a
// handler actually needs constructor/method-injected dependencies or its
// own middleware rules.
func Index(c *apphttp.Context) {
	c.View("welcome", apphttp.H{
		"Name": "World",
	})
}
