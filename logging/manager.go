package logging

import (
	"fmt"
	"sync"
)

// Manager is Golite's driver-based logging engine — the equivalent of
// Illuminate\Log\LogManager, and the concrete type bound into the
// container under "log" by AppServiceProvider. It implements Logger
// itself by delegating to its configured default channel, and supports
// Extend for registering additional named channels — the same registry
// shape as app/Http/Session/SessionManager and hashing.Manager.
type Manager struct {
	mu             sync.RWMutex
	channels       map[string]Channel
	defaultChannel string
}

// NewManager creates a Manager with no channels registered yet; register
// at least defaultChannel via Extend before logging through it.
func NewManager(defaultChannel string) *Manager {
	return &Manager{
		channels:       make(map[string]Channel),
		defaultChannel: defaultChannel,
	}
}

// Extend registers ch under name, mirroring Laravel's
// Log::extend/config('logging.channels.*').
func (m *Manager) Extend(name string, ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = ch
}

// Channel returns a Logger bound to the named channel, mirroring
// Log::channel($name) — e.g. m.Channel("daily").Warning(...) logs to the
// daily channel specifically, regardless of the configured default.
func (m *Manager) Channel(name string) Logger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[name]
	if !ok {
		panic(fmt.Sprintf("golite/logging: no channel registered under %q", name))
	}
	return &boundChannel{channel: ch}
}

// The methods below make *Manager itself satisfy Logger by delegating to
// the default channel, so most call sites (including
// app/Http/Middleware/RecoverMiddleware.go) never need to call Channel
// explicitly.

func (m *Manager) Log(level Level, message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Log(level, message, context...)
}
func (m *Manager) Debug(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Debug(message, context...)
}
func (m *Manager) Info(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Info(message, context...)
}
func (m *Manager) Notice(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Notice(message, context...)
}
func (m *Manager) Warning(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Warning(message, context...)
}
func (m *Manager) Error(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Error(message, context...)
}
func (m *Manager) Critical(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Critical(message, context...)
}
func (m *Manager) Alert(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Alert(message, context...)
}
func (m *Manager) Emergency(message string, context ...map[string]any) {
	m.Channel(m.defaultChannel).Emergency(message, context...)
}
