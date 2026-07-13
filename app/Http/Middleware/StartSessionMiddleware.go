package middleware

import (
	"net/http"

	apphttp "Golite/app/Http"
	gosession "Golite/app/Http/Session"
)

// StartSession loads the request's session — via the manager's active
// driver, keyed by the session cookie — before the rest of the chain
// runs, attaches it to the Context, and, once the chain completes,
// persists it back and refreshes the session cookie's expiration.
// Golite's equivalent of Laravel's Illuminate\Session\Middleware\StartSession.
//
// Register it as the "session" alias inside the "web" middleware group —
// NewKernel already seeds that group with the name "session", ahead of
// "csrf" (which depends on a session being present):
//
//	kernel.AliasMiddleware("session", middleware.NewStartSession(app.Kernel.Sessions()))
type StartSession struct {
	manager *gosession.Manager
}

// NewStartSession constructs a StartSession middleware backed by manager
// (typically kernel.Sessions()).
func NewStartSession(manager *gosession.Manager) *StartSession {
	return &StartSession{manager: manager}
}

// Handle loads the session, runs the rest of the chain, then saves it
// (unless a RouteDefinition.Block-created middleware, nested inside this
// one, already did so while holding its lock — see that middleware's doc
// comment) and refreshes the session cookie.
//
// For every driver except the stateless "cookie" one, the cookie is queued
// *before* next() runs, using the session's ID at load time — not after.
// This is a deliberate, Go-specific ordering: http.ResponseWriter finalizes
// headers the instant anything downstream calls WriteHeader, which is
// exactly what any handler serving an actual response does (writing JSON,
// rendering a view, redirecting, ...) — so a Set-Cookie added only after
// next() returns is silently dropped for the overwhelming majority of
// routes. Queuing it early works because an ID-based driver's cookie value
// doesn't depend on anything the handler does — it's stable barring a
// Regenerate/Invalidate call, which Context.RegenerateSession/
// InvalidateSession handle by updating the already-queued cookie in place,
// from within the handler, before it writes its own response (the only
// point left where that's still possible).
//
// The stateless "cookie" driver can't use this trick: its cookie value
// *is* the encoded session payload, which isn't known until the handler
// has finished mutating the session, so it's computed after next() as
// before. That means it's only reliably delivered on requests where the
// handler doesn't itself write a response before this code runs — true for
// very few real routes — so prefer the "memory" or "file" driver for any
// route that both mutates the session and responds in the same request;
// see docs/sessions.md.
func (m *StartSession) Handle(c *apphttp.Context, next func(), _ ...string) {
	rawCookieValue := ""
	if cookie, err := c.Request.Cookie(m.manager.CookieName()); err == nil {
		rawCookieValue = cookie.Value
	}

	sess := m.manager.Load(rawCookieValue)
	c.SetSession(sess)
	c.Set(apphttp.SessionOriginalIDKey, sess.ID())

	stateless := m.manager.IsStateless()
	if !stateless {
		m.setCookie(c, sess.ID())
	}

	next()

	if saved, _ := c.Get(apphttp.SessionSavedKey); saved == true {
		return // a .Block() middleware, nested inside this one, already saved
	}

	originalID, _ := c.Get(apphttp.SessionOriginalIDKey)
	id, _ := originalID.(string)
	value, err := m.manager.Save(sess, id)
	if err != nil {
		return
	}
	if stateless {
		m.setCookie(c, value)
	}
}

func (m *StartSession) setCookie(c *apphttp.Context, value string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     m.manager.CookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   m.manager.Lifetime(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   apphttp.IsSecureRequest(c.Request),
	})
}
