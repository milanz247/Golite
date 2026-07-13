package http

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"

	gosession "Golite/app/Http/Session"
	"Golite/container"
	"Golite/validation"
)

// Context wraps the request/response pair together with the application's
// service container (Laravel's Application itself extends Container, so
// resolving services through App mirrors Laravel's `app()->make(...)`),
// the resolved route parameters, a middleware/handler pipeline, the active
// session, the unified input payload, and a small per-request key/value
// store.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	App     *container.Container

	handlers []HandlerFunc
	index    int
	params   map[string]string

	store    map[string]any
	executed []Middleware

	// session is attached by StartSessionMiddleware, not resolved lazily
	// — see SetSession and Session's doc comments.
	session *gosession.Session

	// sessionManager backs RegenerateSession/InvalidateSession, which need
	// to write the session cookie immediately (not just mutate the Session
	// object) — see those methods' doc comments for why.
	sessionManager *gosession.Manager

	appKey []byte

	inputResolved bool
	input         map[string]any

	tempFiles []string
}

func newContext(w http.ResponseWriter, r *http.Request, app *container.Container, appKey []byte, sessionManager *gosession.Manager, handlers []HandlerFunc) *Context {
	return &Context{
		Writer:         w,
		Request:        r,
		App:            app,
		appKey:         appKey,
		sessionManager: sessionManager,
		handlers:       handlers,
		index:          -1,
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

// Validate validates the request's unified input (c.All()) against rules
// ("field": "required|email|...", see the validation package), mirroring
// Laravel's $request->validate($rules). On success it returns the
// validated subset of input; on failure it panics with a
// *validation.Exception, which RecoverMiddleware (see
// app/Http/Middleware/RecoverMiddleware.go, and docs/error-handling.md)
// turns into a 422 JSON response carrying the field errors — the same
// automatic behavior Laravel's own exception handler gives
// ValidationException, without every handler needing its own
// `if v.Fails() { ... }` boilerplate. RecoverMiddleware must be registered
// (see public/main.go) for a failure here to render cleanly rather than
// crash the connection.
func (c *Context) Validate(rules map[string]string) map[string]any {
	validated, err := validation.Make(c.All(), rules).Validated()
	if err != nil {
		panic(err)
	}
	return validated
}

// Response starts a fluent response (see the *Response type and its
// chaining methods in Response.go): content is auto-converted the same
// way a handler's raw return value is (see Responder/writeAutoResponse)
// unless a specialized method (Json/View/Download/File/StreamDownload) is
// chained afterward. status defaults to 200.
func (c *Context) Response(content any, status ...int) *Response {
	return NewResponse(content, status...)
}

// View renders resources/views/<name>.html and sends it immediately — a
// shorthand for c.Response(nil).View(name, data).Send(c) for the common
// case where a handler wants nothing else from the fluent Response API.
// data is optional and variadic:
//
//	c.View("index", H{"Message": "hi"})   // an H (map[string]any) literal
//	c.View("index", someStruct)            // any struct, for {{.Field}} access
//	c.View("index")                        // no data: whatever was
//	    previously stored via Context.Set is used instead — lets a handler
//	    (or an earlier middleware) build up view data incrementally rather
//	    than assembling one map/struct inline.
func (c *Context) View(name string, data ...any) {
	var viewData any
	if len(data) > 0 {
		viewData = data[0]
	} else {
		viewData = c.store
	}
	c.Response(nil).View(name, viewData).Send(c)
}

// Redirect builds a redirect response to a (typically local, but not
// required to be) URL, defaulting to 302 Found — Laravel's redirect()->to().
func (c *Context) Redirect(to string, status ...int) *Response {
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	return &Response{kind: kindRedirect, redirectTo: to, status: code}
}

// Back redirects to the page the browser says it came from (the Referer
// header), or "/" if there isn't one — Laravel's redirect()->back().
func (c *Context) Back() *Response {
	referer := c.Header("Referer")
	if referer == "" {
		referer = "/"
	}
	return c.Redirect(referer)
}

// Away redirects to an external URL — Laravel's redirect()->away(), which
// exists there to bypass Laravel's URL generator (which would otherwise
// try to resolve a relative-looking path against the app's own domain).
// Golite's Redirect never does that kind of local resolution in the first
// place — it just sets the Location header to whatever string it's
// given, exactly like Go's own http.Redirect — so Away is provided for
// API parity and to make the caller's intent explicit, but behaves
// identically to Redirect.
func (c *Context) Away(url string) *Response {
	return c.Redirect(url)
}

// Macro invokes a response macro registered on ResponseFactory (see
// Response.go) by name, returning the *Response it builds. Panics if the
// macro doesn't exist or was called with the wrong arguments — see
// macroRegistry.Call's doc comment for why that's the appropriate failure
// mode here.
func (c *Context) Macro(name string, args ...any) *Response {
	resp, err := ResponseFactory.Call(name, args...)
	if err != nil {
		panic(err)
	}
	return resp
}

// oldInputKey is the single session key Flash/Old store the entire
// unified input payload under, as one nested map — matching Laravel's own
// internal convention (Request::flash() -> Session::flashInput(), which
// flashes the whole input array under "_old_input" rather than one
// session key per field). This is what keeps a flashed field named, say,
// "email" from colliding with an unrelated Response.With("email", ...)
// flash message: With flashes directly under the literal key given, while
// Flash/Old always go through this one level of nesting.
const oldInputKey = "_old_input"

// Session returns the request's active session, attached by
// StartSessionMiddleware before any route handler runs (see
// app/Http/Middleware/StartSessionMiddleware.go) — this does not create
// one lazily. Panics if no session was attached, meaning
// StartSessionMiddleware isn't active on the matched route; register it
// (NewKernel already seeds the "web" middleware group with "session" —
// see Kernel.go) on any route that calls Session, CsrfToken, Flash, or
// Old, directly or indirectly (redirect .With/.WithInput, for instance).
func (c *Context) Session() *gosession.Session {
	if c.session == nil {
		panic("golite: no active session on this request — is StartSessionMiddleware registered on this route? (see docs/sessions.md)")
	}
	return c.session
}

// SetSession attaches sess to the request. Called by StartSessionMiddleware
// once per request; application code should not need to call this
// directly.
func (c *Context) SetSession(sess *gosession.Session) {
	c.session = sess
}

// RegenerateSession regenerates the active session's ID (see
// gosession.Session.Regenerate) and immediately refreshes the session
// cookie to carry the new one.
//
// This exists because of a Go http.ResponseWriter constraint:
// StartSessionMiddleware queues the session cookie *before* the rest of the
// chain runs (so it isn't silently dropped once a handler's response body
// finalizes the headers — see that middleware's doc comment), using
// whatever the session's ID was at that point. If a handler regenerates the
// ID mid-request and then writes its own response, that "before" cookie is
// already wrong and there is no later opportunity to fix it — WriteHeader
// will already have run by the time StartSessionMiddleware's own code
// resumes. Calling RegenerateSession from the handler, before it writes
// anything, updates the already-queued cookie in place (headers can be
// freely rewritten right up until WriteHeader is actually called), so the
// client receives the new ID in the very same response — critical for
// preventing session fixation around a login, where the response
// confirming success and the cookie carrying the new ID need to arrive
// together. Prefer this over calling c.Session().Regenerate() directly
// whenever the client needs to see the new ID in this response.
func (c *Context) RegenerateSession() string {
	newID := c.Session().Regenerate()
	c.refreshSessionCookie(newID)
	return newID
}

// InvalidateSession is RegenerateSession's counterpart for
// gosession.Session.Invalidate — see RegenerateSession's doc comment for why
// this, rather than c.Session().Invalidate() directly, is needed to make
// the new session ID reach the client in the same response (typically used
// right before a logout redirect).
func (c *Context) InvalidateSession() {
	c.Session().Invalidate()
	c.refreshSessionCookie(c.Session().ID())
}

// refreshSessionCookie queues (or, if StartSessionMiddleware already queued
// one earlier in this same request, replaces) the session cookie with
// value. Cookies share the "Set-Cookie" response header across possibly
// many distinct cookies, so this searches the header's existing values for
// one already carrying this session's cookie name and overwrites just that
// entry, rather than blindly appending a duplicate (which would leave two
// conflicting Set-Cookie lines for the same cookie name — surprisingly not
// well-defined across HTTP clients).
func (c *Context) refreshSessionCookie(value string) {
	if c.sessionManager == nil {
		return
	}
	cookie := &http.Cookie{
		Name:     c.sessionManager.CookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   c.sessionManager.Lifetime(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   IsSecureRequest(c.Request),
	}
	encoded := cookie.String()

	prefix := c.sessionManager.CookieName() + "="
	existing := c.Writer.Header()["Set-Cookie"]
	for i, h := range existing {
		if strings.HasPrefix(h, prefix) {
			existing[i] = encoded
			return
		}
	}
	c.Writer.Header().Add("Set-Cookie", encoded)
}

// CsrfToken returns the active session's CSRF token, generating one on
// first use. Use it to render a hidden
// <input type="hidden" name="_token" value="{{ c.CsrfToken() }}"> field or
// a <meta name="csrf-token" content="..."> tag, mirroring Laravel's
// csrf_token() helper / @csrf Blade directive.
func (c *Context) CsrfToken() string {
	return c.Session().Token()
}

// Flash copies the current unified input payload into the session, ready
// to be read back via Old on the *next* request (typically the one a
// validation-failure redirect sends the browser to) — Laravel's
// Request::flash().
func (c *Context) Flash() {
	c.resolveInput()
	c.Session().Flash(oldInputKey, c.input)
}

// Old retrieves a value flashed on the *previous* request via Flash, for
// repopulating a form field after a redirect — Laravel's old() helper /
// Request::old(). Returns "" if nothing was flashed under that key.
func (c *Context) Old(key string) string {
	fields, ok := c.Session().Get(oldInputKey).(map[string]any)
	if !ok {
		return ""
	}
	v, ok := fields[key]
	if !ok {
		return ""
	}
	return stringifyInputValue(v)
}

// Cookie retrieves and decrypts a cookie previously set with SetCookie,
// verifying its AES-GCM authentication tag in the process — Golite's
// equivalent of Laravel's automatically decrypted, "signed" cookies.
// Returns ErrInvalidCookie if the cookie is missing, malformed, or fails
// authentication (including simply having been encrypted by a previous
// process, since Golite's app key is regenerated on every restart).
func (c *Context) Cookie(name string) (string, error) {
	raw, err := c.Request.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return decryptCookieValue(c.appKey, raw.Value)
}

// SetCookie sets an AES-256-GCM encrypted, authenticated cookie — the
// counterpart to Cookie — HttpOnly, SameSite=Lax, and Secure whenever the
// request is (or is reported by a trusted proxy to be) HTTPS. maxAge is in
// seconds; 0 means a session cookie (expires when the browser closes).
func (c *Context) SetCookie(name, value string, maxAge int) error {
	encrypted, err := encryptCookieValue(c.appKey, value)
	if err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    encrypted,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   IsSecureRequest(c.Request),
	})
	return nil
}

// HasFile reports whether a file was uploaded under the given multipart
// form field name.
func (c *Context) HasFile(key string) bool {
	_, err := c.File(key)
	return err == nil
}

// File returns the uploaded file submitted under the given multipart form
// field name. The upload is copied to a temporary file on disk (so Path()
// is always valid; see UploadedFile's doc comment) that's cleaned up
// automatically at the end of the request unless Store/StoreAs already
// moved it — see Kernel.ServeHTTP.
func (c *Context) File(key string) (*UploadedFile, error) {
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}
	if c.Request.MultipartForm == nil || len(c.Request.MultipartForm.File[key]) == 0 {
		return nil, http.ErrMissingFile
	}
	header := c.Request.MultipartForm.File[key][0]

	return c.copyUploadToTemp(header)
}

func (c *Context) copyUploadToTemp(header *multipart.FileHeader) (*UploadedFile, error) {
	src, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "golite-upload-*")
	if err != nil {
		return nil, err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, src); err != nil {
		return nil, err
	}

	c.tempFiles = append(c.tempFiles, tmp.Name())

	return &UploadedFile{
		Filename: header.Filename,
		Size:     header.Size,
		tempPath: tmp.Name(),
	}, nil
}

// cleanupTempFiles removes every temporary file File() created for this
// request that Store/StoreAs didn't already move elsewhere (which updates
// UploadedFile.tempPath, but this slice holds the *original* temp paths,
// so a moved file's original path is simply already gone — os.Remove
// returning an error here is expected and ignored). Called once per
// request from Kernel.ServeHTTP, after the response has been sent.
func (c *Context) cleanupTempFiles() {
	for _, path := range c.tempFiles {
		_ = os.Remove(path)
	}
}

// ---------------------------------------------------------------------------
// Request inspection helpers
// ---------------------------------------------------------------------------

// Path returns the request's URI path, without the query string.
func (c *Context) Path() string {
	return c.Request.URL.Path
}

// Is reports whether the request path matches pattern, which may contain
// "*" wildcards anywhere (e.g. "admin/*" matches "admin/users" and
// "admin/users/5"), mirroring Laravel's Request::is(). Leading slashes on
// both pattern and the path are ignored.
func (c *Context) Is(pattern string) bool {
	return wildcardMatch(strings.TrimPrefix(pattern, "/"), strings.TrimPrefix(c.Path(), "/"))
}

func wildcardMatch(pattern, subject string) bool {
	if pattern == subject || pattern == "*" {
		return true
	}

	segments := strings.Split(pattern, "*")
	var b strings.Builder
	b.WriteString("^")
	for i, seg := range segments {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	b.WriteString("$")

	matched, err := regexp.MatchString(b.String(), subject)
	return err == nil && matched
}

// Url returns the request's URL without its query string, e.g.
// "https://example.com/posts/5" — Laravel's Request::url().
func (c *Context) Url() string {
	scheme := "http"
	if IsSecureRequest(c.Request) {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + c.Request.URL.Path
}

// FullUrl returns the request's URL including its query string — Laravel's
// Request::fullUrl().
func (c *Context) FullUrl() string {
	if c.Request.URL.RawQuery == "" {
		return c.Url()
	}
	return c.Url() + "?" + c.Request.URL.RawQuery
}

// Method returns the request's HTTP method (reflecting any
// MethodSpoofingMiddleware override, since that runs before any route
// handler).
func (c *Context) Method() string {
	return c.Request.Method
}

// IsMethod reports whether the request's method matches, case-insensitively.
func (c *Context) IsMethod(method string) bool {
	return strings.EqualFold(c.Request.Method, method)
}

// Ip returns the client's IP address. It deliberately reads only
// Request.RemoteAddr — never an X-Forwarded-For/X-Real-IP header directly
// — because those headers are trivially spoofable by the client itself
// unless a specific upstream proxy is known to overwrite them; see
// TrustProxiesMiddleware, which is the only thing that should ever
// promote a forwarded address into RemoteAddr (after validating the
// immediate peer is actually a trusted proxy), making it safe for Ip to
// stay this simple.
func (c *Context) Ip() string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// Header returns a request header's value, or defaultValue[0] if absent.
func (c *Context) Header(key string, defaultValue ...string) string {
	if v := c.Request.Header.Get(key); v != "" {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// HasHeader reports whether the request carries the given header at all
// (including an empty value), unlike Header, which can't distinguish
// "absent" from "present but empty".
func (c *Context) HasHeader(key string) bool {
	return len(c.Request.Header.Values(key)) > 0
}

// BearerToken extracts the token from an "Authorization: Bearer <token>"
// header, or "" if the header is absent or doesn't use the Bearer scheme.
func (c *Context) BearerToken() string {
	const prefix = "Bearer "
	auth := c.Header("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// ExpectsJson reports whether the client wants a JSON response: either its
// Accept header names a JSON media type (application/json, or any
// "+json" structured suffix like application/vnd.api+json), or it sent
// the conventional X-Requested-With: XMLHttpRequest header AJAX clients
// use — mirroring Laravel's Request::expectsJson().
func (c *Context) ExpectsJson() bool {
	accept := c.Header("Accept")
	if accept != "" {
		first := strings.TrimSpace(strings.Split(accept, ",")[0])
		if strings.Contains(first, "application/json") || strings.HasSuffix(first, "+json") {
			return true
		}
	}
	return c.Header("X-Requested-With") == "XMLHttpRequest"
}
