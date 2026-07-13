// Package logging is Golite's equivalent of Illuminate\Log: a driver-based,
// PSR-3-leveled logging engine (Manager, mirroring
// app/Http/Session's SessionManager pattern), with "single", "daily", and
// "stack" channels out of the box.
package logging

import "time"

// Level is a PSR-3 / RFC 5424 severity level, ordered from least to most
// severe — the same eight levels Laravel's Log facade exposes.
type Level int

const (
	Debug Level = iota
	Info
	Notice
	Warning
	Error
	Critical
	Alert
	Emergency
)

// String returns the lowercase level name used in log output (e.g.
// "warning").
func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Notice:
		return "notice"
	case Warning:
		return "warning"
	case Error:
		return "error"
	case Critical:
		return "critical"
	case Alert:
		return "alert"
	case Emergency:
		return "emergency"
	default:
		return "unknown"
	}
}

// Entry is one structured log record, handed to a Channel's Write.
type Entry struct {
	Time    time.Time
	Level   Level
	Message string
	Context map[string]any
}

// Logger is the leveled logging contract — implemented by both a single
// bound channel and by Manager itself (which delegates to its default
// channel) — Golite's equivalent of Illuminate\Log\Logger / the Log
// facade's instance methods.
type Logger interface {
	Log(level Level, message string, context ...map[string]any)
	Debug(message string, context ...map[string]any)
	Info(message string, context ...map[string]any)
	Notice(message string, context ...map[string]any)
	Warning(message string, context ...map[string]any)
	Error(message string, context ...map[string]any)
	Critical(message string, context ...map[string]any)
	Alert(message string, context ...map[string]any)
	Emergency(message string, context ...map[string]any)
}

// Channel is a single log destination — Golite's equivalent of a Monolog
// handler underlying one Laravel log channel.
type Channel interface {
	Write(entry Entry) error
}

// boundChannel adapts a bare Channel (which only knows how to Write one
// Entry) into the full leveled Logger interface, filling in Entry.Time and
// the optional context map. Manager.Channel returns one of these; Manager
// itself reuses the same adaptation for its own Logger methods against the
// default channel.
type boundChannel struct {
	channel Channel
}

func (b *boundChannel) Log(level Level, message string, context ...map[string]any) {
	var ctx map[string]any
	if len(context) > 0 {
		ctx = context[0]
	}
	// A logging failure (disk full, permissions, ...) must never itself
	// crash the request it's trying to log about — same reasoning as
	// net/http's own logger. There's nowhere safe to surface the error
	// from here.
	_ = b.channel.Write(Entry{Time: time.Now(), Level: level, Message: message, Context: ctx})
}

func (b *boundChannel) Debug(message string, context ...map[string]any) {
	b.Log(Debug, message, context...)
}
func (b *boundChannel) Info(message string, context ...map[string]any) {
	b.Log(Info, message, context...)
}
func (b *boundChannel) Notice(message string, context ...map[string]any) {
	b.Log(Notice, message, context...)
}
func (b *boundChannel) Warning(message string, context ...map[string]any) {
	b.Log(Warning, message, context...)
}
func (b *boundChannel) Error(message string, context ...map[string]any) {
	b.Log(Error, message, context...)
}
func (b *boundChannel) Critical(message string, context ...map[string]any) {
	b.Log(Critical, message, context...)
}
func (b *boundChannel) Alert(message string, context ...map[string]any) {
	b.Log(Alert, message, context...)
}
func (b *boundChannel) Emergency(message string, context ...map[string]any) {
	b.Log(Emergency, message, context...)
}
