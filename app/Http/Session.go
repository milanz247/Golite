package http

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
)

// SessionCookieName is the cookie Golite uses to identify a browser's
// server-side session, analogous to Laravel's default "laravel_session"
// cookie.
const SessionCookieName = "golite_session"

// csrfTokenKey is the key the CSRF token is stored under inside a
// Session's value store.
const csrfTokenKey = "_csrf_token"

// Session is a per-browser, server-side key/value store, analogous to
// Laravel's Illuminate\Session\Store. A Session is created lazily, the
// first time a request calls Context.Session, and lives in a SessionStore
// for the lifetime of the process — Golite ships only an in-memory driver;
// see docs/security-csrf.md for the tradeoffs and how to add a persistent
// one.
type Session struct {
	ID string

	mu     sync.RWMutex
	values map[string]string
}

func newSession(id string) *Session {
	return &Session{ID: id, values: make(map[string]string)}
}

// Get returns a stored value, or "" if key was never set.
func (s *Session) Get(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[key]
}

// Put stores a value under key.
func (s *Session) Put(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

// Token returns the session's CSRF token, generating and persisting one —
// via crypto/rand, exactly once per session — the first time it's
// requested, mirroring Laravel's Session::token() (populated once by
// Session::regenerateToken() when the session is first created).
func (s *Session) Token() string {
	s.mu.RLock()
	token := s.values[csrfTokenKey]
	s.mu.RUnlock()
	if token != "" {
		return token
	}

	generated := generateSecureToken()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.values[csrfTokenKey]; existing != "" {
		// Lost a race with a concurrent request for the same brand-new
		// session; keep whichever token was stored first so both requests
		// agree on it.
		return existing
	}
	s.values[csrfTokenKey] = generated
	return generated
}

// generateSecureToken returns a URL-safe, base64-encoded random token
// backed by crypto/rand — used for both session IDs and CSRF tokens.
func generateSecureToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read only fails if the OS CSPRNG is unavailable, a
		// condition no caller can meaningfully recover from.
		panic("golite: failed to generate a secure token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// SessionStore is a thread-safe, in-memory session registry keyed by
// session ID — Golite's minimal equivalent of Laravel's session driver
// abstraction (file/database/redis/...), sufficient for a lightweight,
// single-process framework and straightforward to swap for a persistent
// store later without changing Context's Session API.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionStore creates an empty, in-memory session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

func (s *SessionStore) find(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *SessionStore) create() *Session {
	sess := newSession(generateSecureToken())
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	return sess
}

// IsSecureRequest reports whether r should be treated as an HTTPS request,
// checking both a direct TLS connection and the conventional
// X-Forwarded-Proto header set by a reverse proxy that terminates TLS
// upstream of the Go process. Used to decide whether cookies carry the
// Secure flag; a deployment behind a proxy that doesn't set this header
// should set it, or terminate TLS in-process, for cookies to be marked
// Secure correctly.
func IsSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
