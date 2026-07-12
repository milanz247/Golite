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

	"Golite/container"
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

	sessions *SessionStore
	session  *Session

	appKey []byte

	inputResolved bool
	input         map[string]any

	tempFiles []string
}

func newContext(w http.ResponseWriter, r *http.Request, app *container.Container, sessions *SessionStore, appKey []byte, handlers []HandlerFunc) *Context {
	return &Context{
		Writer:   w,
		Request:  r,
		App:      app,
		sessions: sessions,
		appKey:   appKey,
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

// Response starts a fluent response (see the *Response type and its
// chaining methods in Response.go): content is auto-converted the same
// way a handler's raw return value is (see Responder/writeAutoResponse)
// unless a specialized method (Json/View/Download/File/StreamDownload) is
// chained afterward. status defaults to 200.
func (c *Context) Response(content any, status ...int) *Response {
	return NewResponse(content, status...)
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

// Session resolves the current request's session, identified by the
// SessionCookieName cookie, creating one if the request doesn't carry a
// valid session cookie yet. The result is cached on the Context, so
// repeated calls within one request always return the same *Session. The
// first call for a request with no existing session queues a Set-Cookie
// header for the new session ID — like any other header, this must happen
// before the response is written, which is naturally the case for
// middleware (e.g. VerifyCsrfToken) and any handler code that calls
// Session/CsrfToken before writing a body.
//
// This is also the single hook point where flash data ages by exactly one
// request (see Session.ageFlashData): it's guarded by the same
// c.session != nil check that makes Session idempotent within a request,
// so aging happens exactly once per request, before that request's own
// Flash (if any) runs.
func (c *Context) Session() *Session {
	if c.session != nil {
		return c.session
	}

	sess, found := c.sessionFromCookie()
	if !found {
		sess = c.sessions.create()
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     SessionCookieName,
			Value:    sess.ID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   IsSecureRequest(c.Request),
		})
	}

	c.session = sess
	sess.ageFlashData()
	return sess
}

func (c *Context) sessionFromCookie() (*Session, bool) {
	cookie, err := c.Request.Cookie(SessionCookieName)
	if err != nil {
		return nil, false
	}
	return c.sessions.find(cookie.Value)
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
	sess := c.Session()
	for key, value := range c.input {
		sess.flashPut(key, stringifyInputValue(value))
	}
}

// Old retrieves a value flashed on the *previous* request via Flash, for
// repopulating a form field after a redirect — Laravel's old() helper /
// Request::old(). Returns "" if nothing was flashed under that key.
func (c *Context) Old(key string) string {
	return c.Session().flashGet(key)
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
