package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	apphttp "Golite/app/Http"
)

// StatusPageExpired is Laravel's conventional CSRF-mismatch status code.
// It isn't a registered IANA status, so the standard library has no
// constant for it. Golite defines its own for the same reason Laravel
// does: a 4xx that specifically communicates "the page you had open is
// stale, reload and try again" rather than a generic 403 Forbidden.
const StatusPageExpired = 419

// csrfSafeMethods never require a CSRF token, mirroring Laravel's
// VerifyCsrfToken::isReading().
var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// VerifyCsrfToken is Golite's equivalent of Laravel's
// Illuminate\Foundation\Http\Middleware\VerifyCsrfToken: it rejects
// state-changing requests that don't carry a valid, session-bound CSRF
// token, and — on every request that passes (or was exempt) — refreshes
// the XSRF-TOKEN cookie so JS clients (Axios, Angular) can read it and echo
// it back as the X-XSRF-TOKEN header on subsequent requests.
type VerifyCsrfToken struct {
	// Except lists URI patterns exempt from verification, matched with
	// Laravel's $except wildcard semantics: a trailing "*" matches any
	// suffix (e.g. "/stripe/*" matches "/stripe/webhook" and anything
	// deeper), and an entry with no "*" must match the path exactly.
	// Typical uses are third-party webhooks (Stripe, GitHub, ...) that
	// can't supply a session-bound token.
	Except []string
}

// NewVerifyCsrfToken constructs a VerifyCsrfToken middleware exempting the
// given URI patterns.
func NewVerifyCsrfToken(except ...string) *VerifyCsrfToken {
	return &VerifyCsrfToken{Except: except}
}

// Handle verifies the request's CSRF token (unless the method is safe or
// the path is exempt) and, only for requests allowed to proceed, syncs the
// XSRF-TOKEN cookie to the active session's current token.
//
// The cookie is queued *before* calling next, not after. Laravel sets it
// after the response comes back, via tap($next($request), ...), which
// works there because PHP's Response is a mutable object that isn't
// actually sent to the client until the framework explicitly flushes it —
// headers can still be added right up until then. Go's http.ResponseWriter
// has no such buffering: the moment a downstream handler calls
// WriteHeader (which Context.JSON does), every header set after that point
// is silently dropped. Setting the cookie first sidesteps that entirely,
// and is safe to do unconditionally here because the token value only
// depends on the session, never on what the downstream handler does.
func (m *VerifyCsrfToken) Handle(c *apphttp.Context, next func(), _ ...string) {
	if csrfSafeMethods[c.Request.Method] || m.isExcluded(c.Request.URL.Path) || m.tokensMatch(c) {
		m.syncCookie(c)
		next()
		return
	}

	c.JSON(StatusPageExpired, map[string]string{"error": "CSRF token mismatch"})
}

// isExcluded reports whether requestPath matches any of the middleware's
// Except patterns.
func (m *VerifyCsrfToken) isExcluded(requestPath string) bool {
	for _, pattern := range m.Except {
		if matchesExceptPattern(pattern, requestPath) {
			return true
		}
	}
	return false
}

func matchesExceptPattern(pattern, requestPath string) bool {
	if pattern == requestPath {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(requestPath, prefix)
	}
	return false
}

// tokensMatch compares the request's supplied token against the active
// session's token in constant time (crypto/subtle), so response timing
// can't leak how much of the token an attacker guessed correctly.
func (m *VerifyCsrfToken) tokensMatch(c *apphttp.Context) bool {
	expected := c.Session().Token()
	token := tokenFromRequest(c.Request)
	if expected == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

// tokenFromRequest reads the submitted CSRF token from, in order: the
// "_token" form field, the X-CSRF-TOKEN header, then the X-XSRF-TOKEN
// header — what Axios/Angular send automatically, read client-side from
// the XSRF-TOKEN cookie this same middleware sets (the "double submit
// cookie" pattern: a value the browser echoes back is only meaningful
// proof of same-origin JS if an attacker's cross-site form can't read the
// cookie to forge it).
func tokenFromRequest(r *http.Request) string {
	if token := r.PostFormValue("_token"); token != "" {
		return token
	}
	if token := r.Header.Get("X-CSRF-TOKEN"); token != "" {
		return token
	}
	return r.Header.Get("X-XSRF-TOKEN")
}

// syncCookie refreshes the XSRF-TOKEN cookie to the active session's
// current token. It's deliberately *not* HttpOnly — client-side JS must be
// able to read it to echo it back as X-XSRF-TOKEN. SameSite=Lax keeps it
// off cross-site requests without breaking normal top-level navigation,
// and Secure is set whenever the request is (or is reported by a trusted
// proxy to be) HTTPS. Unlike the session cookie itself (HttpOnly, set by
// Context.Session), this cookie carries no session identity on its own —
// it's only useful paired with the actual session cookie, so a leaked
// XSRF-TOKEN alone doesn't let an attacker impersonate the session.
func (m *VerifyCsrfToken) syncCookie(c *apphttp.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "XSRF-TOKEN",
		Value:    c.Session().Token(),
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   apphttp.IsSecureRequest(c.Request),
	})
}
