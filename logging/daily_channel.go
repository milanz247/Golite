package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DailyChannel writes to a date-suffixed file per calendar day (e.g.
// "golite-2026-07-13.log") and prunes files older than Days on each write
// — Laravel's "daily" log driver.
type DailyChannel struct {
	mu        sync.Mutex
	directory string
	basename  string
	days      int // 0 = keep every day's file forever
}

// NewDailyChannel builds a DailyChannel writing "<basename>-YYYY-MM-DD.log"
// files into directory, retaining the most recent days of them (0 keeps
// them all).
func NewDailyChannel(directory, basename string, days int) *DailyChannel {
	return &DailyChannel{directory: directory, basename: basename, days: days}
}

// Write appends entry to today's file, then prunes files older than
// d.days.
func (d *DailyChannel) Write(entry Entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	path := filepath.Join(d.directory, fmt.Sprintf("%s-%s.log", d.basename, entry.Time.Format("2006-01-02")))
	if err := appendEntry(path, entry); err != nil {
		return err
	}
	if d.days > 0 {
		d.prune(entry.Time)
	}
	return nil
}

func (d *DailyChannel) prune(now time.Time) {
	entries, err := os.ReadDir(d.directory)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -d.days)
	prefix, suffix := d.basename+"-", ".log"

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		dateStr := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if fileDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(d.directory, name))
		}
	}
}
