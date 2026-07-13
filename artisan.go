// Command artisan is Golite's console driver — the equivalent of Laravel's
// `php artisan`. Run it with `go run artisan.go <command>` from the
// project root; see printUsage below for the full command list.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"Golite/app/Providers"
	"Golite/config"
	"Golite/database/migrations"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "migrate":
		runMigrate()
	case "migrate:rollback":
		runRollback()
	case "make:migration":
		if len(os.Args) < 3 || strings.TrimSpace(os.Args[2]) == "" {
			fmt.Fprintln(os.Stderr, "Usage: go run artisan.go make:migration <name>")
			os.Exit(1)
		}
		runMakeMigration(os.Args[2])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Golite Artisan

Usage:
  go run artisan.go <command> [arguments]

Available commands:
  migrate                  Run all pending migrations
  migrate:rollback         Roll back the last batch of migrations
  make:migration <name>    Create a new migration file in database/migrations/`)
}

// connectDatabase loads config and opens the same GORM connection
// DatabaseServiceProvider would, reusing its DSN-building and
// connection-pool setup — see OpenDatabase's doc comment. Unlike the
// service provider (which degrades gracefully when MySQL isn't
// reachable, since the HTTP demo doesn't require a database), a failure
// here is fatal: migrate/migrate:rollback have nothing meaningful to do
// without one.
func connectDatabase() *gorm.DB {
	cfg := config.LoadConfig()

	db, err := providers.OpenDatabase(cfg.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "golite: could not connect to the database: %v\n", err)
		fmt.Fprintln(os.Stderr, "Check DB_HOST/DB_PORT/DB_DATABASE/DB_USERNAME/DB_PASSWORD in .env.")
		os.Exit(1)
	}
	return db
}

func runMigrate() {
	db := connectDatabase()

	ran, err := migrations.NewRunner(db).Migrate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "golite: migration failed: %v\n", err)
		os.Exit(1)
	}

	if len(ran) == 0 {
		fmt.Println("Nothing to migrate.")
		return
	}
	for _, name := range ran {
		fmt.Printf("Migrated:  %s\n", name)
	}
}

func runRollback() {
	db := connectDatabase()

	rolledBack, err := migrations.NewRunner(db).Rollback()
	if err != nil {
		fmt.Fprintf(os.Stderr, "golite: rollback failed: %v\n", err)
		os.Exit(1)
	}

	if len(rolledBack) == 0 {
		fmt.Println("Nothing to roll back.")
		return
	}
	for _, name := range rolledBack {
		fmt.Printf("Rolled back:  %s\n", name)
	}
}

// migrationsDir is where make:migration writes new files, and the import
// path Runner discovers them through (see the database/migrations package
// doc comment for why no separate registration step is needed beyond
// writing the file).
const migrationsDir = "database/migrations"

var nonWordRunRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// slugify normalizes a user-supplied migration name into
// lower_snake_case, e.g. "Create Users Table" or "create-users-table"
// both become "create_users_table".
func slugify(name string) string {
	s := nonWordRunRe.ReplaceAllString(strings.TrimSpace(name), "_")
	return strings.ToLower(strings.Trim(s, "_"))
}

// studlyCase turns lower_snake_case into StudlyCase, e.g.
// "create_users_table" -> "CreateUsersTable" — used to derive the
// generated migration's Go struct name from its slugified file name.
func studlyCase(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

const migrationTemplate = `package migrations

import "gorm.io/gorm"

func init() {
	Register(&%[1]s{})
}

// %[1]s is a generated migration — fill in Up/Down, then run
// ` + "`go run artisan.go migrate`" + `.
type %[1]s struct{}

// Name is this migration's tracking key; leave it matching the file name.
func (%[1]s) Name() string {
	return %[2]q
}

// Up applies the migration, e.g.:
//   return db.Migrator().CreateTable(&models.YourModel{})
//   return db.Exec("ALTER TABLE ... ").Error
func (%[1]s) Up(db *gorm.DB) error {
	return nil
}

// Down reverses Up, e.g.:
//   return db.Migrator().DropTable("your_table")
func (%[1]s) Down(db *gorm.DB) error {
	return nil
}
`

func runMakeMigration(rawName string) {
	slug := slugify(rawName)
	if slug == "" {
		fmt.Fprintf(os.Stderr, "golite: %q isn't a usable migration name (nothing left after removing non-alphanumeric characters)\n", rawName)
		os.Exit(1)
	}

	timestamp := time.Now().Format("2006_01_02_150405")
	migrationName := timestamp + "_" + slug
	// Struct names can't start with a digit, so the timestamp can't lead
	// (unlike migrationName, which is a string value, not an identifier) —
	// "Migration" + the timestamp with its underscores stripped keeps it a
	// valid, still-unique-per-second Go identifier: e.g.
	// "Migration20260713215110AddBioToUsersTable".
	structName := "Migration" + strings.ReplaceAll(timestamp, "_", "") + studlyCase(slug)
	fileName := migrationName + ".go"
	path := filepath.Join(migrationsDir, fileName)

	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "golite: failed to create %s: %v\n", migrationsDir, err)
		os.Exit(1)
	}
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "golite: %s already exists\n", path)
		os.Exit(1)
	}

	contents := fmt.Sprintf(migrationTemplate, structName, migrationName)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "golite: failed to write %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("Created migration: %s\n", path)
}
