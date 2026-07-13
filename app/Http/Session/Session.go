package session

import (
	"encoding/json"
	"sync"
)

// csrfTokenKey is the session key the CSRF token is stored under —
// matching Laravel's own Session key name for the same value, "_token".
const csrfTokenKey = "_token"

// Session is Golite's expressive, driver-backed session object, analogous
// to Laravel's Illuminate\Session\Store. A Session is decoded fresh from
// its driver's stored payload at the start of every request (see
// Manager.Load) — unlike a middleware instance or the route table, it is
// never a long-lived object shared across concurrent requests for the
// same ID, so its own mutex here guards against concurrent access
// *within* a single request (a goroutine spawned by a handler, a
// middleware and the handler it wraps) rather than across requests. That
// cross-request case — two concurrent requests for the same session ID
// each loading a copy, mutating it, and whichever saves last silently
// discarding the other's changes — is what RouteDefinition.Block exists
// to prevent instead; see docs/sessions.md.
type Session struct {
	mu sync.RWMutex

	id     string
	values map[string]any

	// oldFlash holds the keys readable *this* request cycle (flashed on
	// the previous one); newFlash holds keys flashed *this* cycle,
	// readable starting next cycle. ageFlash rotates old -> gone,
	// new -> old once per request; see its doc comment.
	oldFlash map[string]bool
	newFlash map[string]bool
}

func newSession(id string) *Session {
	return &Session{
		id:       id,
		values:   make(map[string]any),
		oldFlash: make(map[string]bool),
		newFlash: make(map[string]bool),
	}
}

// ID returns the session's current ID. It can change mid-request via
// Regenerate/Invalidate — callers that captured it earlier (Manager.Save's
// originalID parameter) should keep their own copy rather than assume it's
// stable.
func (s *Session) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// ---------------------------------------------------------------------------
// Core attribute access
// ---------------------------------------------------------------------------

// Get retrieves a value, returning defaultValue[0] if key is absent. If
// defaultValue[0] is a func() any, it's called lazily and its result
// returned — Laravel's support for a plain default value *or* a default
// resolver closure.
func (s *Session) Get(key string, defaultValue ...any) any {
	s.mu.RLock()
	v, ok := s.values[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	return resolveDefault(defaultValue)
}

func resolveDefault(defaultValue []any) any {
	if len(defaultValue) == 0 {
		return nil
	}
	if resolver, isFunc := defaultValue[0].(func() any); isFunc {
		return resolver()
	}
	return defaultValue[0]
}

// Put stores value under key.
func (s *Session) Put(key string, value any) {
	s.mu.Lock()
	s.values[key] = value
	s.mu.Unlock()
}

// Push appends value to the slice stored at key, creating it if absent —
// Laravel's Session::push(), for growing a session value that represents
// a list (e.g. a stack of recently viewed product IDs).
func (s *Session) Push(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, _ := s.values[key].([]any)
	s.values[key] = append(existing, value)
}

// Pull retrieves a value and removes it from the session in one step —
// Laravel's Session::pull(), typically used for "read this once" values.
func (s *Session) Pull(key string, defaultValue ...any) any {
	s.mu.Lock()
	v, ok := s.values[key]
	if ok {
		delete(s.values, key)
	}
	s.mu.Unlock()
	if ok {
		return v
	}
	return resolveDefault(defaultValue)
}

// All returns a copy of every session value.
func (s *Session) All() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]any, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// Has reports whether key is present *and* its value isn't nil — Laravel's
// Session::has(), which treats a key explicitly set to null as absent.
func (s *Session) Has(key string) bool {
	s.mu.RLock()
	v, ok := s.values[key]
	s.mu.RUnlock()
	return ok && v != nil
}

// Exists reports whether key is present at all, even if its value is nil
// — Laravel's Session::exists(), which (unlike Has) does not treat a null
// value as absent.
func (s *Session) Exists(key string) bool {
	s.mu.RLock()
	_, ok := s.values[key]
	s.mu.RUnlock()
	return ok
}

// Missing is the inverse of Exists (not of Has) — Laravel's
// Session::missing() is defined the same way.
func (s *Session) Missing(key string) bool {
	return !s.Exists(key)
}

// Forget removes the given keys.
func (s *Session) Forget(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.values, k)
	}
}

// Flush removes every session value, including flash data.
func (s *Session) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = make(map[string]any)
	s.oldFlash = make(map[string]bool)
	s.newFlash = make(map[string]bool)
}

// ---------------------------------------------------------------------------
// Numeric helpers
// ---------------------------------------------------------------------------

// Increment adds steps (default 1) to the numeric value at key (treating
// an absent or non-numeric value as 0) and returns the new value.
func (s *Session) Increment(key string, steps ...int) int {
	step := 1
	if len(steps) > 0 {
		step = steps[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := toInt(s.values[key]) + step
	s.values[key] = next
	return next
}

// Decrement subtracts steps (default 1) from the numeric value at key —
// the inverse of Increment.
func (s *Session) Decrement(key string, steps ...int) int {
	step := 1
	if len(steps) > 0 {
		step = steps[0]
	}
	return s.Increment(key, -step)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64: // the shape a JSON-decoded number takes after a save/reload round trip
		return int(n)
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Session lifecycle: regeneration and invalidation
// ---------------------------------------------------------------------------

// Regenerate assigns a new, cryptographically random session ID, keeping
// the session's data — Laravel's Session::regenerate(), used to prevent
// session fixation attacks (call it right after a successful login, so an
// ID an attacker may have fixated on the victim's browser before
// authentication stops being valid).
func (s *Session) Regenerate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = generateRandomToken()
	return s.id
}

// Invalidate regenerates the session ID *and* discards all existing
// session data (including flash state) — Laravel's Session::invalidate(),
// typically called on logout.
func (s *Session) Invalidate() {
	s.mu.Lock()
	s.id = generateRandomToken()
	s.values = make(map[string]any)
	s.oldFlash = make(map[string]bool)
	s.newFlash = make(map[string]bool)
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// CSRF token
// ---------------------------------------------------------------------------

// Token returns the session's CSRF token, generating and persisting one —
// via crypto/rand, exactly once per session — the first time it's
// requested. Race-safe: two concurrent first-requests for a brand-new
// session both generating a token will agree on whichever was stored
// first, rather than each trusting its own.
func (s *Session) Token() string {
	s.mu.RLock()
	if token, ok := s.values[csrfTokenKey].(string); ok && token != "" {
		s.mu.RUnlock()
		return token
	}
	s.mu.RUnlock()

	generated := generateRandomToken()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.values[csrfTokenKey].(string); ok && existing != "" {
		return existing
	}
	s.values[csrfTokenKey] = generated
	return generated
}

// ---------------------------------------------------------------------------
// Flash data
// ---------------------------------------------------------------------------

// Flash stores value under key, readable via Get starting with the *next*
// request that loads this session, for exactly one cycle — Laravel's
// Session::flash().
func (s *Session) Flash(key string, value any) {
	s.mu.Lock()
	s.values[key] = value
	s.newFlash[key] = true
	delete(s.oldFlash, key)
	s.mu.Unlock()
}

// Now stores value under key, readable via Get only during the *current*
// request cycle — gone by the time the next request loads this session —
// Laravel's Session::now().
func (s *Session) Now(key string, value any) {
	s.mu.Lock()
	s.values[key] = value
	s.oldFlash[key] = true
	delete(s.newFlash, key)
	s.mu.Unlock()
}

// Reflash re-marks every currently "old" flash key (visible this request)
// as "new" (visible next request too), extending its lifetime by one more
// cycle — Laravel's Session::reflash().
func (s *Session) Reflash() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.oldFlash {
		s.newFlash[k] = true
	}
}

// Keep re-marks specific "old" flash keys as "new", extending just those
// — Laravel's Session::keep().
func (s *Session) Keep(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		if s.oldFlash[k] {
			s.newFlash[k] = true
		}
	}
}

// ageFlash rotates the flash generations: whatever was "old" (visible
// during the request that's now ending) is discarded from the session
// entirely, and whatever was "new" (flashed during that same request) is
// promoted to "old", becoming visible for the request now starting.
// Called once per request by Manager.Load, immediately after decoding —
// never called mid-request, so a Flash()/Now() call made by the current
// request's own handler is never prematurely aged.
func (s *Session) ageFlash() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key := range s.oldFlash {
		delete(s.values, key)
	}
	s.oldFlash = s.newFlash
	s.newFlash = make(map[string]bool)
}

// ---------------------------------------------------------------------------
// Serialization: how a Session becomes the string payload a Handler
// stores, and back.
// ---------------------------------------------------------------------------

type sessionPayload struct {
	Values   map[string]any `json:"values"`
	FlashOld []string       `json:"flash_old"`
	FlashNew []string       `json:"flash_new"`
}

func (s *Session) encode() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	payload := sessionPayload{
		Values:   s.values,
		FlashOld: keysOf(s.oldFlash),
		FlashNew: keysOf(s.newFlash),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSession(id string, raw string) (*Session, error) {
	var payload sessionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}

	sess := newSession(id)
	if payload.Values != nil {
		sess.values = payload.Values
	}
	for _, k := range payload.FlashOld {
		sess.oldFlash[k] = true
	}
	for _, k := range payload.FlashNew {
		sess.newFlash[k] = true
	}
	return sess, nil
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
