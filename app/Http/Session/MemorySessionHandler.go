package session

import (
	"sync"
	"time"
)

type memoryRecord struct {
	data       string
	lastAccess int64 // unix seconds
}

// MemorySessionHandler holds sessions in a concurrent-safe in-memory map —
// Golite's equivalent of Laravel's "array" session driver. Ideal for
// tests or a single-process deployment with no persistence requirement;
// all data is lost on restart. This is the default driver Manager wires
// up (see Manager.go).
type MemorySessionHandler struct {
	mu      sync.RWMutex
	records map[string]memoryRecord
}

// NewMemorySessionHandler creates an empty in-memory session handler.
func NewMemorySessionHandler() *MemorySessionHandler {
	return &MemorySessionHandler{records: make(map[string]memoryRecord)}
}

func (h *MemorySessionHandler) Read(id string) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.records[id].data, nil
}

func (h *MemorySessionHandler) Write(id string, data string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records[id] = memoryRecord{data: data, lastAccess: time.Now().Unix()}
	return nil
}

func (h *MemorySessionHandler) Destroy(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.records, id)
	return nil
}

// Gc removes every record last written more than lifetime seconds ago.
func (h *MemorySessionHandler) Gc(lifetime int) {
	cutoff := time.Now().Unix() - int64(lifetime)
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, rec := range h.records {
		if rec.lastAccess < cutoff {
			delete(h.records, id)
		}
	}
}
