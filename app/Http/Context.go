package http

import (
	"encoding/json"
	"net/http"

	"Golite/container"
)

// Context wraps the request/response pair together with the application's
// service container (Laravel's Application itself extends Container, so
// resolving services through App mirrors Laravel's `app()->make(...)`),
// the resolved route parameters, a middleware/handler pipeline, the active
// session, and a small per-request key/value store.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	App     *container.Container

	handlers []HandlerFunc
	index    int
	params   map[string]string

	store    map[string]any
	executed []Middleware

	sessions *SessionStore
	session  *Session
}

func newContext(w http.ResponseWriter, r *http.Request, app *container.Container, sessions *SessionStore, handlers []HandlerFunc) *Context {
	return &Context{
		Writer:   w,
		Request:  r,
		App:      app,
		sessions: sessions,
		handlers: handlers,
		index:    -1,
	}
}

// Next invokes the next handler in the chain, if any, and returns once it
// (and everything it in turn calls Next on) has finished — the classic
// recursive "onion" middleware pattern. A middleware that wants to run code
// after the rest of the chain finishes should call Next and then continue
// below it, just like Laravel's pipeline; a middleware that wants to
// short-circuit the request (e.g. failed auth) simply returns *without*
// calling Next, and nothing further down the chain ever runs. Handlers
// appended to the chain *while* Next is running (see Kernel.dispatch, which
// splices in route-specific middleware only once routing has been
// resolved) are picked up correctly, since each call re-checks
// len(c.handlers) against the freshly incremented index.
func (c *Context) Next() {
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}

// Param returns the value of a resolved route parameter (e.g. "id" for a
// route defined as "/user/{id}"), or "" if it wasn't present and had no
// default.
func (c *Context) Param(name string) string {
	if c.params == nil {
		return ""
	}
	return c.params[name]
}

// Params returns a copy of every resolved route parameter for the current
// request, analogous to Laravel's $request->route()->parameters().
func (c *Context) Params() map[string]string {
	out := make(map[string]string, len(c.params))
	for k, v := range c.params {
		out[k] = v
	}
	return out
}

// Set stores an arbitrary value on the request, keyed by name. Its main
// purpose is letting a middleware pass data from Handle to its own
// Terminate — a middleware instance is shared across every concurrent
// request, so it must never store per-request state on itself; Context is
// the per-request, goroutine-safe place for that instead.
func (c *Context) Set(key string, value any) {
	if c.store == nil {
		c.store = make(map[string]any)
	}
	c.store[key] = value
}

// Get retrieves a value previously stored with Set.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.store[key]
	return v, ok
}

// JSON writes a JSON response with the given status code.
func (c *Context) JSON(status int, payload any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(status)
	_ = json.NewEncoder(c.Writer).Encode(payload)
}

// Redirect writes an HTTP redirect response, the primitive behind
// Kernel.Redirect (Route::redirect).
func (c *Context) Redirect(status int, url string) {
	http.Redirect(c.Writer, c.Request, url, status)
}

// Session resolves the current request's session, identified by the
// SessionCookieName cookie, creating one if the request doesn't carry a
// valid session cookie yet. The result is cached on the Context, so
// repeated calls within one request always return the same *Session. The
// first call for a request with no existing session queues a Set-Cookie
// header for the new session ID — like any other header, this must happen
// before the response is written, which is naturally the case for
// middleware (e.g. VerifyCsrfToken) and any handler code that calls
// Session/CsrfToken before writing a body.
func (c *Context) Session() *Session {
	if c.session != nil {
		return c.session
	}

	if cookie, err := c.Request.Cookie(SessionCookieName); err == nil {
		if sess, ok := c.sessions.find(cookie.Value); ok {
			c.session = sess
			return sess
		}
	}

	sess := c.sessions.create()
	c.session = sess
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   IsSecureRequest(c.Request),
	})
	return sess
}

// CsrfToken returns the active session's CSRF token, generating one on
// first use. Use it to render a hidden
// <input type="hidden" name="_token" value="{{ c.CsrfToken() }}"> field or
// a <meta name="csrf-token" content="..."> tag, mirroring Laravel's
// csrf_token() helper / @csrf Blade directive.
func (c *Context) CsrfToken() string {
	return c.Session().Token()
}
