package middleware

import (
	"net/http"
	"strings"

	apphttp "Golite/app/Http"
)

// spoofableMethods are the verbs HTML forms can't submit natively and are
// therefore allowed to be spoofed, mirroring Laravel's Illuminate\Http\Request::getMethod().
var spoofableMethods = map[string]bool{
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// MethodSpoofing lets HTML forms — which only support GET and POST —
// simulate PUT, PATCH, and DELETE requests. It inspects incoming POST
// requests for either an "X-HTTP-Method-Override" header or a hidden
// "_method" form field and, if it names PUT, PATCH, or DELETE, rewrites
// Request.Method before continuing down the chain.
//
// This must be registered as *global* middleware via Kernel.UseMiddleware,
// and it must run before routing is resolved — which Kernel.ServeHTTP
// guarantees by always appending its routing dispatch as the very last
// handler, after every global middleware. That ordering is what lets a
// spoofed method actually change which route matches, exactly like
// Laravel's method spoofing running in the global middleware stack ahead of
// the router.
func MethodSpoofing() apphttp.HandlerFunc {
	return func(c *apphttp.Context) {
		if c.Request.Method == http.MethodPost {
			if override := resolveOverride(c.Request); spoofableMethods[override] {
				c.Request.Method = override
			}
		}
		c.Next()
	}
}

func resolveOverride(r *http.Request) string {
	if header := r.Header.Get("X-HTTP-Method-Override"); header != "" {
		return strings.ToUpper(strings.TrimSpace(header))
	}

	// PostFormValue parses the request body as a form (url-encoded or
	// multipart) as needed and caches the result; for non-form bodies (e.g.
	// JSON APIs) it leaves the body untouched and simply returns "".
	return strings.ToUpper(strings.TrimSpace(r.PostFormValue("_method")))
}
