package session

import (
	"os"
	"path/filepath"
	"time"
)

// FileSessionHandler stores each session as a JSON file inside Directory
// (a configurable "storage/sessions/"-style path), named after the
// session ID, with a file mode restricting it to the owning user —
// Golite's equivalent of Laravel's "file" session driver. Survives a
// process restart, unlike MemorySessionHandler; requires the process to
// have write access to Directory.
type FileSessionHandler struct {
	Directory string
}

// NewFileSessionHandler creates a handler storing sessions under
// directory, which is created on first write if it doesn't exist.
func NewFileSessionHandler(directory string) *FileSessionHandler {
	return &FileSessionHandler{Directory: directory}
}

// path resolves id's file path within Directory. Manager.Load rejects any
// session ID that doesn't look like one this package's own crypto.go
// would have generated before it ever reaches a Handler, but this checks
// again — cheaply — as defense in depth: a Handler should never trust
// that its caller validated its input, since Handler is also the
// extension point custom (Manager.Extend) drivers plug into.
func (h *FileSessionHandler) path(id string) (string, bool) {
	if !isValidSessionID(id) {
		return "", false
	}
	return filepath.Join(h.Directory, id+".json"), true
}

func (h *FileSessionHandler) Read(id string) (string, error) {
	p, ok := h.path(id)
	if !ok {
		return "", nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (h *FileSessionHandler) Write(id string, data string) error {
	p, ok := h.path(id)
	if !ok {
		return ErrInvalidPayload
	}
	if err := os.MkdirAll(h.Directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(data), 0o600)
}

func (h *FileSessionHandler) Destroy(id string) error {
	p, ok := h.path(id)
	if !ok {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Gc removes every session file last modified more than lifetime seconds
// ago.
func (h *FileSessionHandler) Gc(lifetime int) {
	cutoff := time.Now().Add(-time.Duration(lifetime) * time.Second)

	entries, err := os.ReadDir(h.Directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(h.Directory, entry.Name()))
		}
	}
}
