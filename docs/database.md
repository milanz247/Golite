# Database, ORM & Migrations

Files: [`config/database.go`](../config/database.go),
[`app/Providers/DatabaseServiceProvider.go`](../app/Providers/DatabaseServiceProvider.go),
[`app/Models/`](../app/Models/) (`Model.go`, `User.go`, `Post.go`),
[`database/migrations/`](../database/migrations/) (`migration.go`, `runner.go`,
and the migration files themselves),
[`artisan.go`](../artisan.go)

Golite's database layer is built on [GORM](https://gorm.io) against MySQL
— Golite's equivalent of Laravel's Eloquent ORM plus its migration
system, with an `artisan`-equivalent CLI to drive it.

## Configuration — `DatabaseConfig`

```go
// config/database.go
type DatabaseConfig struct {
	Host, Port, Database, Username, Password, Charset string
	MaxIdleConns, MaxOpenConns                         int
	ConnMaxLifetime                                    time.Duration
}
```

Read from `.env` (`DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`,
`DB_PASSWORD`, `DB_CHARSET`, plus the pool-tuning
`DB_MAX_IDLE_CONNS`/`DB_MAX_OPEN_CONNS`/`DB_CONN_MAX_LIFETIME_MINUTES`),
all with defaults suited to a local MySQL install — see
[`.env.example`](../.env.example). `Config.DB` is populated by
`LoadConfig()` the same as every other section (see
[configuration.md](configuration.md)).

## `DatabaseServiceProvider` and the `"db"` binding

```go
func OpenDatabase(cfg config.DatabaseConfig) (*gorm.DB, error)
```

`OpenDatabase` builds a MySQL DSN, opens the GORM connection, and
configures the underlying `*sql.DB`'s pool (`SetMaxIdleConns`/
`SetMaxOpenConns`/`SetConnMaxLifetime`) from `cfg`. It's exported
specifically so both `DatabaseServiceProvider.Register` *and*
`artisan.go`'s CLI commands can open the identical connection without
duplicating DSN/pool logic — the CLI never boots the full HTTP
application (no `Kernel`, no other providers), so it can't just resolve
`"db"` from a running container the way a controller does.

```go
app.Register(&providers.DatabaseServiceProvider{})
```

registered in `public/main.go` (right after `AppServiceProvider`), binds
the resulting `*gorm.DB` into the container under `"db"`.

**A connection failure here is deliberately non-fatal.** Golite's HTTP
demo has always run with zero external dependencies, and MySQL isn't
guaranteed to be running on a fresh clone. `Register` logs a clear
warning and leaves `"db"` unbound rather than panicking — the rest of the
app (routing, sessions, everything from earlier features) keeps working;
only code that actually calls `c.Make("db")` on an unconfigured setup
gets an immediate, obvious nil-interface panic at that specific call
site. Contrast this with `artisan.go`'s `migrate`/`migrate:rollback`,
which are meaningless without a database and so exit non-zero
immediately on a connection failure instead.

### Using `"db"` from a controller

Both of Golite's controller DI styles work here (see
[controllers.md](controllers.md#dependency-injection-two-flavors)) — most
naturally, method injection, since `*gorm.DB` is a concrete type
`Container.ResolveType` matches directly:

```go
func (u *UserController) Index(c *apphttp.Context, db *gorm.DB) {
	var users []models.User
	db.Find(&users)
	c.JSON(http.StatusOK, users)
}
```

wired up the same way as any other injected controller:
`kernel.GET("/users", apphttp.Inject(kernel.Container(), userController.Index))`.

## Models — `app/Models/`

```go
// Model.go — embedded by every model
type Model struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
```

`User` and `Post` demonstrate GORM's association conventions — a HasMany/
BelongsTo pair, the same shape as Laravel's `User hasMany Post` /
`Post belongsTo User`:

```go
type User struct {
	Model
	Name, Email, Password string
	Posts []Post `gorm:"foreignKey:UserID" json:"posts,omitempty"`
}

type Post struct {
	Model
	Title, Body string
	UserID uint
	User   User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
```

Both pin their table name explicitly (`TableName() string`) rather than
relying on GORM's pluralization — stable and predictable regardless of
GORM's naming-strategy defaults, and matching exactly what their
migrations create. Associations are **never loaded eagerly** — a plain
`db.Find(&users)` leaves `Posts` empty; use `db.Preload("Posts").Find(&users)`
when you actually need them, the same opt-in Laravel requires via
`User::with('posts')`.

## Migrations — `database/migrations/`

### The `Migration` interface and self-registration

```go
// migration.go
type Migration interface {
	Name() string
	Up(db *gorm.DB) error
	Down(db *gorm.DB) error
}

func Register(m Migration)
func All() []Migration // sorted by Name — chronological, given the naming convention below
```

Every migration file registers itself from its own `init()`:

```go
// database/migrations/2026_07_13_000001_create_users_table.go
func init() {
	Register(&CreateUsersTable{})
}

type CreateUsersTable struct{}

func (CreateUsersTable) Name() string { return "2026_07_13_000001_create_users_table" }
func (CreateUsersTable) Up(db *gorm.DB) error   { return db.Migrator().CreateTable(&models.User{}) }
func (CreateUsersTable) Down(db *gorm.DB) error { return db.Migrator().DropTable(&models.User{}) }
```

Because every migration file lives in the same `migrations` package, Go
compiles all of them into any binary that imports
`"Golite/database/migrations"` — importing the package is enough to run
every migration file's `init()`, with **no separate filesystem-scanning
step** the way Laravel's migrator has to do at runtime. `Name()` doubles
as the row `Rollback` looks up in the `migrations` table and the sort key
`All()` orders by — hence the `YYYY_MM_DD_HHMMSS_description` convention:
lexicographic order on that string *is* chronological order.

`Up`/`Down` use whatever `*gorm.DB` calls fit — `db.Migrator().CreateTable(&model{})`
(shown above, letting the model's own struct tags define the schema),
`db.Migrator().AddColumn(...)`, or a raw `db.Exec("ALTER TABLE ...")` for
anything GORM's migrator API doesn't cover directly.

### `Runner` — applying and rolling back

```go
// runner.go
type Record struct { // the "migrations" tracking table
	ID        uint
	Migration string
	Batch     int
	RanAt     time.Time
}

func NewRunner(db *gorm.DB) *Runner
func (r *Runner) Migrate() ([]string, error)
func (r *Runner) Rollback() ([]string, error)
```

`Migrate` ensures the `migrations` table exists (`AutoMigrate(&Record{})`,
idempotent), diffs `All()` against what's already recorded, and runs
every pending migration **in `Name()` order**, all under one new,
incremented batch number. Each migration's `Up` and its tracking-row
insert run inside a single `db.Transaction(...)` — a failing `Up` rolls
back just that migration's own partial changes and stops the run
immediately, leaving everything that already committed (this run or
earlier) untouched.

`Rollback` finds the **most recent batch number**, and undoes every
migration in it — in reverse (`ORDER BY id DESC`) order, so a migration
that depends on an earlier one (like `posts` depending on `users`) is
torn down before its dependency, mirroring the same "last-in,
first-out" semantics Laravel's `migrate:rollback` uses. Each migration's
`Down` and its tracking-row delete are transactional the same way
`Migrate`'s are.

## The `artisan` CLI

```bash
go run artisan.go migrate              # run all pending migrations
go run artisan.go migrate:rollback     # roll back the most recent batch
go run artisan.go make:migration <name> # scaffold a new migration file
```

`migrate`/`migrate:rollback` call `connectDatabase()` (a thin wrapper
around `config.LoadConfig()` + `providers.OpenDatabase`), then
`migrations.NewRunner(db).Migrate()`/`.Rollback()`, printing each
migration name as it runs and exiting non-zero on any failure.

`make:migration <name>` slugifies `<name>` into `lower_snake_case`,
prefixes it with the current `YYYY_MM_DD_HHMMSS` timestamp, and writes a
template file to `database/migrations/` with empty `Up`/`Down` bodies
already wired up to `Register` — e.g.
`go run artisan.go make:migration "add bio to users table"` produces
`database/migrations/2026_07_13_215153_add_bio_to_users_table.go`,
`Name()` returning `"2026_07_13_215153_add_bio_to_users_table"`, and a
Go struct named `Migration20260713215153AddBioToUsersTable` — the
`"Migration"` prefix (plus stripping the timestamp's underscores) is
required because Go identifiers can't start with a digit, unlike the
`Name()` *string*, which keeps the readable underscored form. Running
`go run artisan.go migrate` immediately afterward picks the new file up
automatically, with no separate registration step (see the
self-registration explanation above) — fill in its `Up`/`Down` first.

## Verified end-to-end

This entire flow — `migrate` creating `users`/`posts` with the exact
schema `User`/`Post`'s struct tags declare (including the `email`
`uniqueIndex` and `posts.user_id`'s FK index), a second `migrate` being a
correct no-op, `migrate:rollback` dropping both tables in dependency-safe
reverse order, and `make:migration` generating a file that compiles and
gets picked up by the very next `migrate` — was run against a real local
MySQL instance while building this feature, not just compiled.
