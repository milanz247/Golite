package http

import "time"

// Shared Context keys StartSessionMiddleware and the blocking middleware
// created by RouteDefinition.Block use to coordinate exactly one session
// save per request. Exported (despite the "internal coordination" nature)
// because StartSessionMiddleware lives in the separate
// app/Http/Middleware package, which needs to reference them.
const (
	// SessionOriginalIDKey holds the session ID the request started with
	// (before any Regenerate/Invalidate call), set by
	// StartSessionMiddleware right after loading the session — Session.Save
	// needs this to know whether to destroy a now-stale record.
	SessionOriginalIDKey = "__golite_session_original_id"

	// SessionSavedKey, when true, tells StartSessionMiddleware's own
	// after-phase to skip saving the session — because a .Block()
	// middleware, nested inside it, already did so while still holding
	// its lock. See RouteDefinition.Block's doc comment for why the save
	// has to happen there instead of in StartSessionMiddleware. The
	// session *cookie* itself needs no equivalent handoff: for every
	// driver but the stateless "cookie" one, StartSessionMiddleware
	// already queued it before .Block()'s middleware (nested inside it)
	// ever runs — see StartSession.Handle's doc comment.
	SessionSavedKey = "__golite_session_saved"
)

// defaultBlockTimeout is used when .Block() is called with no explicit
// lockSeconds argument — Laravel's own default is 10 seconds.
const defaultBlockTimeout = 10 * time.Second

// sessionBlockMiddleware serializes concurrent requests sharing the same
// session ID — see RouteDefinition.Block.
type sessionBlockMiddleware struct {
	kernel  *Kernel
	timeout time.Duration
}

// Handle acquires a per-session lock (Kernel.Sessions().Lock) before
// running the rest of the chain, re-loads the session under that lock, and
// — critically — saves it *itself*, while still holding the lock, rather
// than leaving that to StartSessionMiddleware's own after-phase.
//
// Both the reload and the save-location matter, for two separate reasons:
//
//  1. StartSessionMiddleware already loaded (and attached to Context) a
//     session before this middleware ever runs — necessarily so, since it
//     wraps this one and has to have a session ready before route-specific
//     middleware executes at all. That load happened with no lock held, so
//     under concurrent requests it may have raced another request's still-
//     in-flight save and picked up a stale snapshot. Re-loading here, now
//     that the lock is actually held, is what makes this request start
//     from the freshest available state — skip it and the lock only
//     serializes *saves* of independently-stale copies, which still loses
//     updates (each save just overwrites the previous one's cart item, for
//     instance), defeating the entire point of blocking.
//  2. Golite nests route-level middleware (this one) *inside* whatever
//     middleware group wraps it (StartSessionMiddleware is registered into
//     the "web" group, resolved and spliced in ahead of route-specific
//     middleware, but still runs its own "after" phase — where it would
//     normally save the session — only once everything nested inside it,
//     including this middleware's "after" phase, has already returned). If
//     the lock were released here and the save happened there, the actual
//     write would sit outside the critical section. Setting SessionSavedKey
//     tells StartSessionMiddleware to skip its own save when this already
//     ran.
//
// A brand-new session (no incoming cookie, so nothing yet persisted under
// any ID for another request to race against) skips both the lock and the
// reload — there's nothing to serialize against yet.
func (m *sessionBlockMiddleware) Handle(c *Context, next func(), _ ...string) {
	manager := m.kernel.Sessions()
	originalID, _ := c.Get(SessionOriginalIDKey)
	id, _ := originalID.(string)

	if id != "" {
		if unlock, ok := manager.Lock(id, m.timeout); ok {
			defer unlock()
		}

		fresh := manager.Load(id)
		c.SetSession(fresh)
		if fresh.ID() != id {
			// The record backing id vanished from under us (e.g. it
			// expired or was destroyed by a concurrent Regenerate
			// elsewhere) — Load minted a new one. StartSessionMiddleware
			// already queued a cookie for the *old* id before this
			// middleware ran; fix it up now, same as
			// Context.RegenerateSession does for the mid-handler case.
			c.refreshSessionCookie(fresh.ID())
		}
	}

	sess := c.Session()
	next()

	_, _ = manager.Save(sess, id)
	c.Set(SessionSavedKey, true)
}

// Block makes this route acquire an exclusive, per-session-ID lock before
// running, and reload the session under that lock — serializing concurrent
// requests that share the same session (two AJAX calls fired close
// together from the same browser tab, for instance) so one can't silently
// overwrite the other's session changes (the "lost update" problem: both
// load a copy, both mutate independently, whichever saves last wins).
// lockSeconds is how long to wait for the lock before giving up and running
// anyway, unlocked (default 10s, matching Laravel's own default);
// equivalent to Laravel's ->block($lockSeconds).
//
// Requires StartSessionMiddleware to be active on this route (so
// Context.Session resolves) — see docs/sessions.md, which also covers the
// precise scope of what this protects given Golite's middleware nesting
// order.
func (r *RouteDefinition) Block(lockSeconds ...int) *RouteDefinition {
	timeout := defaultBlockTimeout
	if len(lockSeconds) > 0 {
		timeout = time.Duration(lockSeconds[0]) * time.Second
	}
	return r.WithMiddleware("block", &sessionBlockMiddleware{kernel: r.kernel, timeout: timeout})
}
