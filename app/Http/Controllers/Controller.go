package controllers

import "sync"

// MiddlewareRule pairs a middleware name (or "name:params" spec, matching
// the same strings routes use) with an optional restriction to specific
// controller actions, returned by Controller.Middleware so it can be
// scoped fluently — mirroring Laravel's
// $this->middleware('auth')->only(['index'])->except(...).
type MiddlewareRule struct {
	name          string
	onlyActions   []string
	exceptActions []string
}

// Only restricts this middleware rule to the given action names (e.g.
// "index", "store", or "__invoke" for a single-action controller).
func (r *MiddlewareRule) Only(actions ...string) *MiddlewareRule {
	r.onlyActions = actions
	return r
}

// Except restricts this middleware rule to every action *except* the
// given names.
func (r *MiddlewareRule) Except(actions ...string) *MiddlewareRule {
	r.exceptActions = actions
	return r
}

func (r *MiddlewareRule) appliesTo(action string) bool {
	if len(r.onlyActions) > 0 {
		return containsAction(r.onlyActions, action)
	}
	if len(r.exceptActions) > 0 {
		return !containsAction(r.exceptActions, action)
	}
	return true
}

func containsAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == action {
			return true
		}
	}
	return false
}

// Controller is the base type every Golite controller should embed,
// analogous to Laravel's Illuminate\Routing\Controller. On its own it
// contributes no HTTP behavior — its only job is letting a controller
// declare middleware, typically from its constructor:
//
//	func NewPostController(hasher Hasher) *PostController {
//		c := &PostController{hasher: hasher}
//		c.Middleware("auth").Except("index", "show")
//		return c
//	}
//
// Route::resource/apiResource/singleton (see app/Http/Resource.go) detect
// this automatically for any controller that embeds Controller — they
// don't import this package at all, only a small structural interface
// (apphttp.ControllerMiddleware) that Controller's MiddlewareForAction
// method happens to satisfy, which is what keeps
// app/Http -> app/Http/Controllers from ever becoming an import cycle
// (app/Http/Controllers already imports app/Http for Context).
type Controller struct {
	mu    sync.Mutex
	rules []*MiddlewareRule
}

// Middleware declares a middleware for this controller, returning a
// *MiddlewareRule so it can immediately be scoped with
// .Only(...)/.Except(...). With neither, the middleware applies to every
// action. Safe to call more than once to declare several middleware.
func (c *Controller) Middleware(name string) *MiddlewareRule {
	rule := &MiddlewareRule{name: name}
	c.mu.Lock()
	c.rules = append(c.rules, rule)
	c.mu.Unlock()
	return rule
}

// MiddlewareForAction returns the middleware names that apply to the given
// action, honoring each declared rule's Only/Except scoping. This is what
// satisfies apphttp.ControllerMiddleware — see that interface's doc
// comment for who calls it and when.
func (c *Controller) MiddlewareForAction(action string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var names []string
	for _, rule := range c.rules {
		if rule.appliesTo(action) {
			names = append(names, rule.name)
		}
	}
	return names
}
