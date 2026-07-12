package middleware

import (
	"net"
	"net/http"
	"strings"

	apphttp "Golite/app/Http"
)

// TrustHosts rejects any request whose Host header doesn't match one of
// Patterns — Golite's equivalent of Laravel's TrustHosts middleware. It
// guards against HTTP Host header injection: an application that builds
// URLs (password-reset links, redirects, ...) from the incoming Host
// header rather than a fixed, configured domain can be tricked into
// generating links that point at an attacker's server if the Host header
// isn't validated first.
type TrustHosts struct {
	// Patterns lists the allowed hosts. An entry may start with "*." to
	// match any subdomain (e.g. "*.example.com" matches "api.example.com"
	// but not "example.com" itself — list both explicitly if the bare
	// domain should also be trusted); anything else must match exactly.
	// An empty Patterns trusts every host, i.e. disables the check —
	// Golite's default, since a lightweight framework has no way to know
	// the deployment's real domain(s) in advance.
	Patterns []string
}

// NewTrustHosts constructs a TrustHosts middleware trusting the given host
// patterns.
func NewTrustHosts(patterns ...string) *TrustHosts {
	return &TrustHosts{Patterns: patterns}
}

// Handle rejects the request with 400 Bad Request if its Host header
// doesn't match any configured pattern.
func (m *TrustHosts) Handle(c *apphttp.Context, next func(), _ ...string) {
	if len(m.Patterns) == 0 {
		next()
		return
	}

	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	for _, pattern := range m.Patterns {
		if matchesHostPattern(pattern, host) {
			next()
			return
		}
	}

	c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid host header"})
}

func matchesHostPattern(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
		// suffix is e.g. ".example.com"; require at least one label before
		// it, so the pattern doesn't also match the bare domain.
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return false
}
