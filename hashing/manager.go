package hashing

import (
	"fmt"
	"sync"
)

// Manager is Golite's driver-based hashing engine — the equivalent of
// Illuminate\Hashing\HashManager, and the concrete type bound into the
// container under "hash" by AppServiceProvider. It implements Hasher
// itself by delegating to its configured default driver, so it can be used
// exactly where a bare Hasher was used before, while also supporting
// Extend for registering additional drivers (e.g. an Argon2id
// implementation) — the same driver-registry shape as
// app/Http/Session/SessionManager.
type Manager struct {
	mu            sync.RWMutex
	drivers       map[string]Hasher
	defaultDriver string
}

// NewManager creates a Manager with no drivers registered yet; register at
// least defaultDriver via Extend before use.
func NewManager(defaultDriver string) *Manager {
	return &Manager{
		drivers:       make(map[string]Hasher),
		defaultDriver: defaultDriver,
	}
}

// Extend registers a Hasher implementation under name, mirroring
// Laravel's Hash::extend.
func (m *Manager) Extend(name string, h Hasher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[name] = h
}

// Driver returns the Hasher registered under name, panicking if none was
// registered — an unrecognized driver name is a configuration error to
// catch immediately, not a runtime condition to propagate as an error
// value (mirroring gosession.Manager.Driver).
func (m *Manager) Driver(name string) Hasher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.drivers[name]
	if !ok {
		panic(fmt.Sprintf("golite/hashing: no driver registered under %q", name))
	}
	return h
}

// Make hashes value using the default driver.
func (m *Manager) Make(value string) string {
	return m.Driver(m.defaultDriver).Make(value)
}

// Check verifies value against hashedValue using the default driver.
func (m *Manager) Check(value, hashedValue string) bool {
	return m.Driver(m.defaultDriver).Check(value, hashedValue)
}

// NeedsRehash reports whether hashedValue should be re-hashed under the
// default driver's current parameters.
func (m *Manager) NeedsRehash(hashedValue string) bool {
	return m.Driver(m.defaultDriver).NeedsRehash(hashedValue)
}
