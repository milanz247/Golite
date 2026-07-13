package session

import (
	"fmt"
	"sync"
	"time"
)

// Manager coordinates loading and saving sessions against whichever
// driver is active, and owns the per-session locks RouteDefinition.Block
// uses — Golite's equivalent of Laravel's Illuminate\Session\SessionManager.
type Manager struct {
	mu       sync.RWMutex
	handlers map[string]func() Handler // registered driver factories, by name
	driver   string                    // active driver name
	handler  Handler                   // resolved instance of the active driver, created once

	cookieName string
	lifetime   int // seconds

	locks lockRegistry
}

// NewManager creates a Manager with the built-in "memory", "file", and
// "cookie" drivers already registered (see Extend to add more), "memory"
// active by default, cookieName as its session cookie's name, and
// lifetimeSeconds as both the cookie's MaxAge and the cutoff Gc uses.
// appKey is used only by the "cookie" driver, to encrypt session payloads
// carried directly in the browser — pass the same key used elsewhere for
// cookie encryption (Kernel.appKey) so it's exactly as secure as any other
// encrypted cookie Golite sets.
func NewManager(cookieName string, lifetimeSeconds int, appKey []byte) *Manager {
	m := &Manager{
		handlers:   make(map[string]func() Handler),
		driver:     "memory",
		cookieName: cookieName,
		lifetime:   lifetimeSeconds,
		locks:      newLockRegistry(),
	}
	m.Extend("memory", func() Handler { return NewMemorySessionHandler() })
	m.Extend("file", func() Handler { return NewFileSessionHandler("storage/sessions") })
	m.Extend("cookie", func() Handler { return NewCookieSessionHandler(appKey) })
	return m
}

// CookieName returns the name of the cookie sessions are tracked by.
func (m *Manager) CookieName() string {
	return m.cookieName
}

// Lifetime returns the configured session lifetime, in seconds.
func (m *Manager) Lifetime() int {
	return m.lifetime
}

// Extend registers a driver factory under name, so RouteDefinition-free
// code (typically a service provider) can add a custom session backend —
// Redis, a database, DynamoDB, anything satisfying Handler — without
// Manager needing to know about it in advance:
//
//	manager.Extend("redis", func() session.Handler { return NewRedisSessionHandler(client) })
//	manager.Driver("redis")
func (m *Manager) Extend(name string, factory func() Handler) {
	m.mu.Lock()
	m.handlers[name] = factory
	if name == m.driver {
		m.handler = nil // force re-resolution if this replaces the active driver
	}
	m.mu.Unlock()
}

// Driver switches the active driver by name, resolved lazily (on first
// Load/Save after the switch) via whatever factory was registered for it.
// Panics if name was never registered — a configuration error, caught
// immediately at boot rather than surfacing as a mysterious failure on
// the first request.
func (m *Manager) Driver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.handlers[name]; !ok {
		panic(fmt.Sprintf("golite/session: no session driver registered under %q", name))
	}
	m.driver = name
	m.handler = nil
}

func (m *Manager) resolveHandler() Handler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handler != nil {
		return m.handler
	}
	factory, ok := m.handlers[m.driver]
	if !ok {
		panic(fmt.Sprintf("golite/session: no session driver registered under %q", m.driver))
	}
	m.handler = factory()
	return m.handler
}

// Load resolves a Session from cookieValue (the current session cookie's
// raw value, or "" if the request carried none): for the stateless
// "cookie" driver, cookieValue *is* the encrypted payload, decoded
// directly; for every other driver, it's treated as a session ID looked
// up via the active Handler's Read. Any failure along the way — an empty
// or malformed cookie value, an ID that fails isValidSessionID, a Read
// error, undecodable JSON, a failed decryption — is treated identically
// to "no session yet": a fresh Session with a newly generated ID, never a
// client-supplied one (accepting an attacker-chosen ID would open the
// door to session fixation).
func (m *Manager) Load(cookieValue string) *Session {
	handler := m.resolveHandler()

	if stateless, ok := handler.(*CookieSessionHandler); ok {
		sess := loadStateless(stateless, cookieValue)
		sess.ageFlash()
		return sess
	}

	if cookieValue == "" || !isValidSessionID(cookieValue) {
		return newSession(generateRandomToken())
	}

	payload, err := handler.Read(cookieValue)
	if err != nil || payload == "" {
		return newSession(generateRandomToken())
	}

	sess, err := decodeSession(cookieValue, payload)
	if err != nil {
		return newSession(generateRandomToken())
	}
	sess.ageFlash()
	return sess
}

func loadStateless(handler *CookieSessionHandler, cookieValue string) *Session {
	if cookieValue == "" {
		return newSession(generateRandomToken())
	}
	payload, err := handler.Decode(cookieValue)
	if err != nil {
		return newSession(generateRandomToken())
	}
	sess, err := decodeSession(generateRandomToken(), payload)
	if err != nil {
		return newSession(generateRandomToken())
	}
	return sess
}

// Save persists sess and returns whatever the session cookie's value
// should become. For the stateless "cookie" driver, that's the freshly
// encrypted payload itself; for every other driver, it's sess.ID() — and
// if sess.ID() differs from originalID (Session.Regenerate/Invalidate was
// called during the request), the old ID's record is destroyed first, so
// a stale ID can't be replayed to resurrect the previous session state.
// originalID should be "" for a session that was never associated with an
// incoming cookie (a brand new visitor).
func (m *Manager) Save(sess *Session, originalID string) (cookieValue string, err error) {
	handler := m.resolveHandler()

	payload, err := sess.encode()
	if err != nil {
		return "", err
	}

	if stateless, ok := handler.(*CookieSessionHandler); ok {
		return stateless.Encode(payload)
	}

	if originalID != "" && sess.ID() != originalID {
		_ = handler.Destroy(originalID)
	}
	if err := handler.Write(sess.ID(), payload); err != nil {
		return "", err
	}
	return sess.ID(), nil
}

// IsStateless reports whether the active driver is the "cookie" one, whose
// cookie value carries the encoded session payload itself rather than an
// ID — see StartSessionMiddleware, which needs to know this to decide
// whether the session cookie can be queued before the rest of the chain
// runs (safe for an ID, since it's stable barring a Regenerate/Invalidate
// call) or only after (required for the encoded payload, which isn't known
// until the handler has finished mutating the session).
func (m *Manager) IsStateless() bool {
	_, ok := m.resolveHandler().(*CookieSessionHandler)
	return ok
}

// Lock acquires an exclusive, per-session-ID lock, waiting up to timeout
// before giving up — the mechanism behind RouteDefinition.Block. Returns
// ok=false (and a nil unlock) on timeout, in which case the caller should
// proceed without having acquired the lock rather than block forever.
func (m *Manager) Lock(id string, timeout time.Duration) (unlock func(), ok bool) {
	return m.locks.acquire(id, timeout)
}

// Gc runs garbage collection on the active driver, using Lifetime as the
// cutoff. Golite has no built-in scheduler to call this periodically —
// wire it up to whatever job-running mechanism your deployment already
// has (a cron entry invoking a small command, a ticker goroutine started
// from public/main.go, ...).
func (m *Manager) Gc() {
	m.resolveHandler().Gc(m.lifetime)
}
