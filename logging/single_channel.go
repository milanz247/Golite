package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SingleChannel appends every entry, one line each, to a single file —
// Laravel's "single" log driver, the framework's default.
type SingleChannel struct {
	mu   sync.Mutex
	path string
}

// NewSingleChannel builds a SingleChannel writing to path, creating its
// parent directory on first write.
func NewSingleChannel(path string) *SingleChannel {
	return &SingleChannel{path: path}
}

// Write appends entry to the channel's file, in Laravel's familiar
// "[timestamp] channel.LEVEL: message {context}" line format.
func (s *SingleChannel) Write(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendEntry(s.path, entry)
}

// appendEntry opens path in append mode (creating it and any missing
// parent directories), writes one formatted line, and closes it —
// shared by SingleChannel and DailyChannel (which is a SingleChannel per
// calendar day).
func appendEntry(path string, entry Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "[%s] golite.%s: %s%s\n",
		entry.Time.Format("2006-01-02 15:04:05"),
		entry.Level.String(),
		entry.Message,
		formatContext(entry.Context),
	)
	return err
}

func formatContext(ctx map[string]any) string {
	if len(ctx) == 0 {
		return ""
	}
	b, err := json.Marshal(ctx)
	if err != nil {
		return ""
	}
	return " " + string(b)
}
