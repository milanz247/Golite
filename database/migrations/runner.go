package migrations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Record is the schema of the "migrations" tracking table — Golite's
// equivalent of Laravel's own migrations table (id/migration/batch).
type Record struct {
	ID        uint      `gorm:"primarykey"`
	Migration string    `gorm:"size:255;not null;uniqueIndex"`
	Batch     int       `gorm:"not null;index"`
	RanAt     time.Time `gorm:"not null"`
}

// TableName pins the tracking table to "migrations", matching Laravel's
// own name for it.
func (Record) TableName() string {
	return "migrations"
}

// Runner applies and rolls back registered migrations against db,
// tracking what's already run in the "migrations" table — Golite's
// equivalent of Laravel's Illuminate\Database\Migrations\Migrator.
// A Runner holds no other state, so it's safe to construct fresh
// per-command (as artisan.go does) or reuse.
type Runner struct {
	db *gorm.DB
}

// NewRunner builds a Runner for db.
func NewRunner(db *gorm.DB) *Runner {
	return &Runner{db: db}
}

// ensureTable creates the "migrations" table if it doesn't already exist
// — idempotent, safe to call before every operation.
func (r *Runner) ensureTable() error {
	return r.db.AutoMigrate(&Record{})
}

// applied returns every migration name already recorded, plus the highest
// batch number seen (0 if none have ever run).
func (r *Runner) applied() (map[string]bool, int, error) {
	var records []Record
	if err := r.db.Find(&records).Error; err != nil {
		return nil, 0, err
	}

	seen := make(map[string]bool, len(records))
	maxBatch := 0
	for _, rec := range records {
		seen[rec.Migration] = true
		if rec.Batch > maxBatch {
			maxBatch = rec.Batch
		}
	}
	return seen, maxBatch, nil
}

// Migrate runs every registered migration that hasn't already been
// recorded, in Name (chronological) order, all under one new batch
// number — mirroring `php artisan migrate`. Each migration's Up and its
// tracking-row insert run inside a single transaction: a failing Up
// leaves that migration's own partial changes rolled back and stops the
// run immediately, without disturbing migrations from this run (or
// earlier ones) that already committed successfully. Returns the names
// of every migration actually applied, in the order they ran.
func (r *Runner) Migrate() ([]string, error) {
	if err := r.ensureTable(); err != nil {
		return nil, fmt.Errorf("golite/migrations: failed to prepare the migrations table: %w", err)
	}

	seen, maxBatch, err := r.applied()
	if err != nil {
		return nil, fmt.Errorf("golite/migrations: failed to read applied migrations: %w", err)
	}

	batch := maxBatch + 1
	var ran []string

	for _, m := range All() {
		if seen[m.Name()] {
			continue
		}

		name := m.Name()
		err := r.db.Transaction(func(tx *gorm.DB) error {
			if err := m.Up(tx); err != nil {
				return err
			}
			return tx.Create(&Record{Migration: name, Batch: batch, RanAt: time.Now()}).Error
		})
		if err != nil {
			return ran, fmt.Errorf("golite/migrations: %s failed: %w", name, err)
		}
		ran = append(ran, name)
	}

	return ran, nil
}

// Rollback reverses every migration in the most recently applied batch,
// in reverse (last-applied-first) order, deleting their tracking rows as
// it goes — mirroring `php artisan migrate:rollback`. Returns
// immediately, with a nil slice and no error, if no migrations have ever
// been applied. Each migration's Down and its tracking-row delete run in
// a single transaction, the same failure-isolation Migrate gives.
func (r *Runner) Rollback() ([]string, error) {
	if err := r.ensureTable(); err != nil {
		return nil, fmt.Errorf("golite/migrations: failed to prepare the migrations table: %w", err)
	}

	// Newest-first: within the latest batch, this is also
	// last-applied-first, the correct order to undo them in.
	var records []Record
	if err := r.db.Order("id DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("golite/migrations: failed to read applied migrations: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	latestBatch := records[0].Batch

	byName := make(map[string]Migration)
	for _, m := range All() {
		byName[m.Name()] = m
	}

	var rolledBack []string
	for _, rec := range records {
		if rec.Batch != latestBatch {
			continue
		}

		m, ok := byName[rec.Migration]
		if !ok {
			return rolledBack, fmt.Errorf("golite/migrations: %q is recorded as applied but isn't registered in this build (was its file removed or renamed?)", rec.Migration)
		}

		err := r.db.Transaction(func(tx *gorm.DB) error {
			if err := m.Down(tx); err != nil {
				return err
			}
			return tx.Delete(&Record{}, "migration = ?", rec.Migration).Error
		})
		if err != nil {
			return rolledBack, fmt.Errorf("golite/migrations: rolling back %q failed: %w", rec.Migration, err)
		}
		rolledBack = append(rolledBack, rec.Migration)
	}

	return rolledBack, nil
}
