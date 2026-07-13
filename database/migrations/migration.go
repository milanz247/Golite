// Package migrations is Golite's equivalent of Laravel's
// database/migrations directory: one file per schema change, each
// self-registering via init() so importing this package (as artisan.go
// and Runner both do) is enough to discover every migration that exists
// in the current build — no filesystem scanning needed, since Go already
// compiled every file in this directory into it.
package migrations

import (
	"sort"
	"sync"

	"gorm.io/gorm"
)

// Migration is Golite's equivalent of a Laravel migration class: Up
// applies the change, Down reverses it. Name identifies the migration
// both as its DB-tracked key (in the "migrations" table — see
// runner.go) and its ordering key, and should be the migration's
// timestamped file name without extension, e.g.
// "2026_07_13_000001_create_users_table" — see
// database/migrations/2026_07_13_000001_create_users_table.go for a
// worked example, and artisan.go's make:migration command for
// generating new ones with this convention already applied.
type Migration interface {
	Name() string
	Up(db *gorm.DB) error
	Down(db *gorm.DB) error
}

var (
	registryMu sync.Mutex
	registry   []Migration
)

// Register adds m to the set of known migrations. Every migration file in
// this package calls it from its own init(), which Go runs automatically
// the moment anything imports "Golite/database/migrations" — see the
// package doc comment above.
func Register(m Migration) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, m)
}

// All returns every registered migration, sorted by Name. Lexicographic
// order matches chronological order as long as every migration follows
// the "YYYY_MM_DD_HHMMSS_description" naming convention, which is what
// gives Runner.Migrate/Rollback a well-defined, deterministic order to
// apply and reverse migrations in.
func All() []Migration {
	registryMu.Lock()
	defer registryMu.Unlock()

	out := make([]Migration, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
