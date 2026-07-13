package session

import (
	"sync"
	"time"
)

// lockRegistry serializes concurrent requests that share the same session
// ID — Golite's in-process equivalent of Laravel's cache-based session
// locking (Laravel typically backs ->block() with a Redis/Memcached
// atomic lock; a single Go process has no need for an external store,
// since an in-memory sync.Mutex already provides mutual exclusion across
// every goroutine handling every request). Each session ID gets its own
// *sync.Mutex, created on first use and never removed — a small, bounded
// amount of long-term memory per distinct session ID that has ever called
// .Block(), acceptable for a lightweight framework.
type lockRegistry struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newLockRegistry() lockRegistry {
	return lockRegistry{locks: make(map[string]*sync.Mutex)}
}

func (r *lockRegistry) mutexFor(id string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[id]
	if !ok {
		m = &sync.Mutex{}
		r.locks[id] = m
	}
	return m
}

// pollInterval is deliberately short: session-blocking contention is
// expected to be rare and brief (two near-simultaneous AJAX requests from
// the same browser tab), so a request waiting on the lock should notice
// it's free again quickly rather than adding much latency on top of
// however long the lock-holder took.
const pollInterval = 10 * time.Millisecond

// acquire blocks (via polling — see pollInterval) until id's lock is free
// or timeout elapses. On success, the caller must call the returned
// unlock exactly once. On timeout, returns ok=false and a nil unlock;
// crucially, it does *not* leave a goroutine blocked waiting to acquire
// the mutex in the background (which would eventually succeed, invisibly
// to the timed-out caller, and never release it) — sync.Mutex.TryLock
// (Go 1.18+) makes that possible without one.
func (r *lockRegistry) acquire(id string, timeout time.Duration) (unlock func(), ok bool) {
	m := r.mutexFor(id)

	if timeout <= 0 {
		m.Lock()
		return m.Unlock, true
	}

	deadline := time.Now().Add(timeout)
	for {
		if m.TryLock() {
			return m.Unlock, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(pollInterval)
	}
}
