package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Dynamic return-type serialization (requirement 1): handlers can
// optionally return a value instead of writing the response themselves.
// ---------------------------------------------------------------------------

// ResponderFunc is a request handler that returns a value instead of
// writing the response itself — Golite's opt-in version of Laravel's "a
// route can return anything" behavior: a string is sent as
// text/html, a struct/map/slice/array is serialized to JSON, and a
// *Response handles its own writing (see Response.Send).
//
// This is deliberately a *separate* type from HandlerFunc rather than a
// change to it: HandlerFunc (func(*Context), no return) is used
// extensively throughout the framework and every existing controller —
// changing its signature would force every one of them to add a bare
// "return nil". Wrapping a ResponderFunc with Responder to register it is
// the opt-in Requirement 1 asks for; a plain HandlerFunc needs no changes
// and keeps working exactly as before.
type ResponderFunc func(c *Context) any

// Responder adapts a ResponderFunc into a HandlerFunc, so it can be
// registered on a route exactly like any other handler:
//
//	kernel.GET("/greeting", apphttp.Responder(func(c *apphttp.Context) any {
//		return "Hello!" // -> text/html, 200
//	}))
func Responder(fn ResponderFunc) HandlerFunc {
	return func(c *Context) {
		writeAutoResponse(c, fn(c), http.StatusOK)
	}
}

// writeAutoResponse implements the auto-conversion rules: nil means the
// handler already wrote its own response (or intentionally wrote
// nothing); a *Response sends itself; a string is written as-is with a
// text/html content type; anything else (struct, map, slice, array,
// primitive, ...) is JSON-encoded. defaultStatus is used for the string
// and JSON cases; a *Response carries (and applies) its own status.
func writeAutoResponse(c *Context, result any, defaultStatus int) {
	switch v := result.(type) {
	case nil:
		return
	case *Response:
		v.Send(c)
	case string:
		c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		c.Writer.WriteHeader(defaultStatus)
		_, _ = io.WriteString(c.Writer, v)
	default:
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(defaultStatus)
		_ = json.NewEncoder(c.Writer).Encode(v)
	}
}

// ---------------------------------------------------------------------------
// Response: the fluent response factory (requirement 2)
// ---------------------------------------------------------------------------

// responseKind selects how Response.Send writes the response; set by
// whichever specialized-format method (Json/View/Download/File/
// StreamDownload) was last called, or kindRedirect for
// Context.Redirect/Back/Away. The zero value, kindContent, is the plain
// auto-converted (string -> html, else -> JSON) case Context.Response
// starts every Response at.
type responseKind int

const (
	kindContent responseKind = iota
	kindJSON
	kindView
	kindRedirect
	kindDownload
	kindFile
	kindStream
)

type responseCookie struct {
	name   string
	value  string
	maxAge int // seconds; negative deletes the cookie immediately
}

// Response is Golite's fluent response builder, analogous to Laravel's
// Illuminate\Http\Response / RedirectResponse, built via Context.Response
// (or Context.Redirect/Back/Away for the redirect case). Every chaining
// method returns the same *Response, ending with either an explicit
// Send(c) or — the more common path — being returned from a handler
// wrapped in Responder, which sends it automatically.
//
// A Response is created fresh per request and never shared, so — unlike
// Session or the middleware registries — it needs no locking.
type Response struct {
	kind    responseKind
	content any
	status  int

	headers map[string]string
	cookies []responseCookie

	redirectTo string
	flashData  map[string]any
	flashInput bool

	viewName string
	viewData map[string]any

	filePath     string
	downloadName string

	streamFunc func(w io.Writer)
	streamName string
}

// NewResponse constructs a Response directly, without a Context — useful
// from code that doesn't have one at hand, such as a response macro (see
// ResponseFactory below) registered from a service provider's Register/
// Boot method. Context.Response is the equivalent entry point when a
// Context is available. Named NewResponse rather than Response to avoid
// colliding with the Response type itself — Go doesn't allow a
// package-level function and type to share an identifier (the same reason
// RouteDefinition isn't named Route; see docs/architecture.md).
func NewResponse(content any, status ...int) *Response {
	code := http.StatusOK
	if len(status) > 0 {
		code = status[0]
	}
	return &Response{kind: kindContent, content: content, status: code}
}

// Status overrides the response's HTTP status code.
func (r *Response) Status(code int) *Response {
	r.status = code
	return r
}

func (r *Response) statusOrDefault(def int) int {
	if r.status == 0 {
		return def
	}
	return r.status
}

// Header sets a single response header.
func (r *Response) Header(key, value string) *Response {
	if r.headers == nil {
		r.headers = make(map[string]string)
	}
	r.headers[key] = value
	return r
}

// WithHeaders sets several response headers at once.
func (r *Response) WithHeaders(headers map[string]string) *Response {
	if r.headers == nil {
		r.headers = make(map[string]string, len(headers))
	}
	for k, v := range headers {
		r.headers[k] = v
	}
	return r
}

// Cookie queues an encrypted, authenticated cookie on the response —
// consistent with Context.SetCookie, which this shares its encryption
// with (see Send). minutes is the cookie's lifetime; 0 makes it a session
// cookie (expires when the browser closes).
func (r *Response) Cookie(name, value string, minutes int) *Response {
	r.cookies = append(r.cookies, responseCookie{name: name, value: value, maxAge: minutes * 60})
	return r
}

// WithoutCookie queues a cookie deletion: a same-named cookie with an
// already-expired MaxAge, which browsers treat as "delete immediately."
func (r *Response) WithoutCookie(name string) *Response {
	r.cookies = append(r.cookies, responseCookie{name: name, value: "", maxAge: -1})
	return r
}

// Json forces the response to be JSON-encoded, regardless of what type
// data is — including a string, which the default auto-conversion would
// otherwise send as text/html.
func (r *Response) Json(data any) *Response {
	r.kind = kindJSON
	r.content = data
	return r
}

// View renders an html/template file from ViewsDirectory (default
// "resources/views"), named without its extension — View("welcome", ...)
// loads "resources/views/welcome.html" — mirroring Laravel's view()
// helper. Templates are parsed once and cached; see parseView.
func (r *Response) View(templateName string, data map[string]any) *Response {
	r.kind = kindView
	r.viewName = templateName
	r.viewData = data
	return r
}

// Download forces the client to save filePath as an attachment rather
// than display it, via Content-Disposition. filename overrides the
// downloaded name shown to the user (defaulting to filePath's base name)
// — it does not need to match the file's real name or path on disk.
func (r *Response) Download(filePath string, filename ...string) *Response {
	r.kind = kindDownload
	r.filePath = filePath
	if len(filename) > 0 {
		r.downloadName = filename[0]
	} else {
		r.downloadName = filepath.Base(filePath)
	}
	return r
}

// File serves filePath for inline display (an image or PDF opening
// directly in the browser, for example) rather than as a forced download
// — the only difference from Download is the absence of a
// Content-Disposition: attachment header.
func (r *Response) File(filePath string) *Response {
	r.kind = kindFile
	r.filePath = filePath
	return r
}

// StreamDownload streams callback's output straight to the client as a
// downloadable file named filename, with no temporary file ever written
// to disk — useful for a dynamically generated report, export, or
// archive. callback receives the live response writer; whatever it writes
// is what the client receives.
func (r *Response) StreamDownload(callback func(w io.Writer), filename string) *Response {
	r.kind = kindStream
	r.streamFunc = callback
	r.streamName = filename
	return r
}

// With flashes a single key/value pair into the session, readable on the
// *next* request via Context.Old(key) — Laravel's ->with(), typically
// used for one-off flash messages ("Post created!") rather than form
// input specifically (that's WithInput). Only meaningful on a redirect
// response; see Send.
func (r *Response) With(key string, value any) *Response {
	if r.flashData == nil {
		r.flashData = make(map[string]any)
	}
	r.flashData[key] = value
	return r
}

// WithInput flashes the current request's unified input payload into the
// session (via Context.Flash), so the page being redirected to can
// repopulate a form from Context.Old — Laravel's ->withInput(). Only
// meaningful on a redirect response; see Send.
func (r *Response) WithInput() *Response {
	r.flashInput = true
	return r
}

// Send writes the response to the client according to whatever this
// Response was configured to do. It's called automatically for any
// *Response returned from a Responder-wrapped handler; call it directly
// from a plain HandlerFunc that builds one itself:
//
//	func(c *apphttp.Context) {
//		c.Response(data).Json(data).Send(c)
//	}
func (r *Response) Send(c *Context) {
	for key, value := range r.headers {
		c.Writer.Header().Set(key, value)
	}
	r.applyCookies(c)

	switch r.kind {
	case kindRedirect:
		r.applyFlash(c)
		http.Redirect(c.Writer, c.Request, r.redirectTo, r.statusOrDefault(http.StatusFound))
	case kindJSON:
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(r.statusOrDefault(http.StatusOK))
		_ = json.NewEncoder(c.Writer).Encode(r.content)
	case kindView:
		r.sendView(c)
	case kindDownload:
		c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(r.downloadName)+`"`)
		http.ServeFile(c.Writer, c.Request, r.filePath)
	case kindFile:
		http.ServeFile(c.Writer, c.Request, r.filePath)
	case kindStream:
		c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(r.streamName)+`"`)
		if c.Writer.Header().Get("Content-Type") == "" {
			c.Writer.Header().Set("Content-Type", "application/octet-stream")
		}
		c.Writer.WriteHeader(r.statusOrDefault(http.StatusOK))
		r.streamFunc(c.Writer)
	default: // kindContent
		writeAutoResponse(c, r.content, r.statusOrDefault(http.StatusOK))
	}
}

// applyCookies queues every cookie this Response accumulated via
// Cookie/WithoutCookie, encrypting non-deleted values with the same
// AES-256-GCM primitive Context.SetCookie uses, so a cookie set via
// either path is readable via Context.Cookie either way.
func (r *Response) applyCookies(c *Context) {
	for _, rc := range r.cookies {
		value := rc.value
		if rc.maxAge >= 0 && value != "" {
			if encrypted, err := encryptCookieValue(c.appKey, value); err == nil {
				value = encrypted
			}
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     rc.name,
			Value:    value,
			Path:     "/",
			MaxAge:   rc.maxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   IsSecureRequest(c.Request),
		})
	}
}

// applyFlash writes this Response's With(...)/WithInput() data into the
// session, before the redirect itself is written — like the session
// cookie Context.Session may set for a brand-new session, this has to
// happen before Send calls http.Redirect, which finalizes the response
// headers via WriteHeader (see VerifyCsrfToken's near-identical ordering
// note in docs/security-csrf.md for why header order matters in Go).
func (r *Response) applyFlash(c *Context) {
	if len(r.flashData) > 0 {
		sess := c.Session()
		for key, value := range r.flashData {
			sess.flashPut(key, stringifyInputValue(value))
		}
	}
	if r.flashInput {
		c.Flash()
	}
}

// sanitizeFilename strips characters that would let a filename break out
// of its quoted Content-Disposition value or inject additional header
// content — relevant if a filename is ever derived from anything
// user-supplied (it shouldn't be passed completely unvalidated either way;
// see docs/responses.md).
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(`"`, "", "\r", "", "\n", "")
	return replacer.Replace(name)
}

// ---------------------------------------------------------------------------
// View rendering
// ---------------------------------------------------------------------------

// ViewsDirectory is where View looks for templates, relative to the
// process's working directory — Golite's equivalent of Laravel's
// resources/views. Intended to be configured once at boot (e.g. in
// public/main.go), before the server starts serving requests; changing it
// concurrently with requests being served is not safe.
var ViewsDirectory = "resources/views"

var (
	viewCacheMu sync.RWMutex
	viewCache   = make(map[string]*template.Template)
)

// parseView loads and parses a template exactly once per name, caching
// the result — templates aren't re-read from disk on every request. There
// is no cache invalidation (matching the "restart to pick up changes"
// tradeoff already made for sessions and the cookie encryption key; see
// docs/security-csrf.md).
func parseView(name string) (*template.Template, error) {
	viewCacheMu.RLock()
	tmpl, ok := viewCache[name]
	viewCacheMu.RUnlock()
	if ok {
		return tmpl, nil
	}

	tmpl, err := template.ParseFiles(filepath.Join(ViewsDirectory, name+".html"))
	if err != nil {
		return nil, err
	}

	viewCacheMu.Lock()
	viewCache[name] = tmpl
	viewCacheMu.Unlock()
	return tmpl, nil
}

func (r *Response) sendView(c *Context) {
	tmpl, err := parseView(r.viewName)
	if err != nil {
		c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Writer.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(c.Writer, "golite: failed to render view %q: %v", r.viewName, err)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(r.statusOrDefault(http.StatusOK))
	_ = tmpl.Execute(c.Writer, r.viewData)
}

// ---------------------------------------------------------------------------
// Response macros (requirement 5)
// ---------------------------------------------------------------------------

// macroRegistry is a thread-safe registry of named response macros —
// arbitrary functions returning *Response, registered once (typically
// from a service provider's Register/Boot method) and invoked by name
// from anywhere with a Context, via Context.Macro. Go has no equivalent
// of PHP's __callStatic, which is what lets Laravel call a macro as if it
// were a native method (Response::caps($val)); ResponseFactory.Call
// invokes a registered macro by name via reflection instead, which is
// also what makes registering an arbitrary function *signature* (not just
// a fixed one) possible — a macro isn't required to take a single string
// argument, whatever parameters and count it declares are what
// Context.Macro must be called with.
type macroRegistry struct {
	mu     sync.RWMutex
	macros map[string]any
}

// ResponseFactory is the global response macro registry — Golite's
// equivalent of Laravel's Response facade's Macroable trait. Register a
// macro once, typically from AppServiceProvider.Register:
//
//	apphttp.ResponseFactory.Macro("caps", func(val string) *apphttp.Response {
//		return apphttp.NewResponse(strings.ToUpper(val))
//	})
//
// then invoke it anywhere a Context is available:
//
//	c.Macro("caps", "hello") // -> *Response wrapping "HELLO"
var ResponseFactory = &macroRegistry{macros: make(map[string]any)}

// Macro registers fn under name. fn may have any signature as long as it
// returns exactly one value, a *Response — Context.Macro/Call verify this
// (and the argument count/types) at call time via reflection.
func (f *macroRegistry) Macro(name string, fn any) {
	f.mu.Lock()
	f.macros[name] = fn
	f.mu.Unlock()
}

// Call dynamically invokes the macro registered under name with args,
// returning an error if no such macro exists, fn isn't a function, the
// argument count doesn't match, or fn doesn't return exactly one
// *Response. These are all caller/registration bugs — see Context.Macro,
// which panics on any of them rather than returning the error, on the
// grounds that a macro name and its arguments are supplied by the
// developer at a fixed call site, not derived from request input.
func (f *macroRegistry) Call(name string, args ...any) (*Response, error) {
	f.mu.RLock()
	fn, ok := f.macros[name]
	f.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("golite: no response macro registered under %q", name)
	}

	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("golite: response macro %q is not a function", name)
	}
	if fnType.NumIn() != len(args) {
		return nil, fmt.Errorf("golite: response macro %q expects %d argument(s), got %d", name, fnType.NumIn(), len(args))
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		in[i] = reflect.ValueOf(arg)
	}

	out := fnValue.Call(in)
	if len(out) != 1 {
		return nil, fmt.Errorf("golite: response macro %q must return exactly one value (*Response)", name)
	}
	resp, ok := out[0].Interface().(*Response)
	if !ok {
		return nil, fmt.Errorf("golite: response macro %q must return *Response", name)
	}
	return resp, nil
}
