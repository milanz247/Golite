package middleware

import (
	"net"
	"strings"

	apphttp "Golite/app/Http"
)

// TrustProxies resolves the real client IP from the X-Forwarded-For
// header, but only when the request's immediate peer is in Proxies —
// Golite's equivalent of Laravel's TrustProxies middleware. This is what
// makes Context.Ip's "never read a forwarded header directly" stance
// (see its doc comment) safe to rely on elsewhere: any code that later
// calls Context.Ip is trusting that *this* middleware already did the
// validation, so it must run early, before anything that depends on the
// client's address (rate limiting, audit logging, ...).
//
// Without TrustProxies, X-Forwarded-For is exactly as trustworthy as any
// other client-supplied header — i.e. not at all — since a direct client
// can set it to anything. Only a proxy address you actually control and
// list here makes the header meaningful.
type TrustProxies struct {
	// Proxies lists the trusted proxies' IPs or CIDR ranges (e.g.
	// "10.0.0.0/8"). The literal "*" trusts any peer — appropriate only
	// when every request genuinely arrives through a known reverse proxy
	// (e.g. inside a private network with no direct public access), never
	// on a directly internet-facing server.
	Proxies []string
}

// NewTrustProxies constructs a TrustProxies middleware trusting the given
// proxy addresses/CIDR ranges.
func NewTrustProxies(proxies ...string) *TrustProxies {
	return &TrustProxies{Proxies: proxies}
}

// Handle rewrites the request's RemoteAddr to the client address reported
// by X-Forwarded-For, if and only if the current RemoteAddr (the
// immediate TCP peer) is a trusted proxy.
func (m *TrustProxies) Handle(c *apphttp.Context, next func(), _ ...string) {
	if peer := m.trustedPeerHost(c.Request.RemoteAddr); peer != "" {
		if forwarded := c.Header("X-Forwarded-For"); forwarded != "" {
			if clientIP := firstForwardedAddress(forwarded); clientIP != "" {
				c.Request.RemoteAddr = net.JoinHostPort(clientIP, peerPort(c.Request.RemoteAddr))
			}
		}
	}
	next()
}

// trustedPeerHost returns remoteAddr's host part if it's in m.Proxies, or
// "" if remoteAddr is malformed or untrusted.
func (m *TrustProxies) trustedPeerHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}

	for _, trusted := range m.Proxies {
		if trusted == "*" {
			return host
		}
		if strings.Contains(trusted, "/") {
			if _, cidr, err := net.ParseCIDR(trusted); err == nil && cidr.Contains(ip) {
				return host
			}
			continue
		}
		if trusted == host {
			return host
		}
	}
	return ""
}

// firstForwardedAddress returns the left-most address in an
// X-Forwarded-For header — the original client, by convention, with every
// address after it added by a proxy along the chain.
func firstForwardedAddress(forwarded string) string {
	parts := strings.Split(forwarded, ",")
	return strings.TrimSpace(parts[0])
}

func peerPort(remoteAddr string) string {
	if _, port, err := net.SplitHostPort(remoteAddr); err == nil {
		return port
	}
	return "0"
}
