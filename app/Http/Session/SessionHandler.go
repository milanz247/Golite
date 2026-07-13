// Package session is Golite's driver-based session engine, analogous to
// Laravel's Illuminate\Session package: a pluggable storage contract
// (Handler), an expressive session object (Session) with flash data and
// atomic-ish helpers, and a Manager that ties a request's session cookie
// to whichever driver is active.
//
// This package deliberately has no dependency on app/Http (the parent
// package) — app/Http imports this package for the Session type, so the
// reverse would be an import cycle. Anything this package needs that
// might look like it belongs in app/Http (cookie encryption, "is this
// request secure" detection) is duplicated here instead; see crypto.go.
package session

// Handler is Golite's session storage contract, mirroring PHP's
// SessionHandlerInterface exactly: a session's entire state is read and
// written as a single opaque string (Session handles serializing its own
// data to and from that string — see Session.go's encode/decode), keyed
// by session ID. Every driver (MemorySessionHandler, FileSessionHandler,
// CookieSessionHandler, or a custom one registered via
// Manager.Extend) implements this.
type Handler interface {
	// Read returns the stored payload for id, or "" if there is none —
	// not an error; a missing session is a normal, expected case (a new
	// visitor, or one whose session expired).
	Read(id string) (string, error)

	// Write persists data as the payload for id, creating or overwriting
	// whatever was there.
	Write(id string, data string) error

	// Destroy deletes whatever is stored for id. Not an error if there
	// was nothing to delete.
	Destroy(id string) error

	// Gc removes records older than lifetime seconds. Intended to be
	// called periodically (e.g. from a scheduled job) rather than on
	// every request — see each driver's own doc comment for how it
	// tracks a record's age.
	Gc(lifetime int)
}
