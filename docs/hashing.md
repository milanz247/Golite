# Hashing

Files: [`hashing/hasher.go`](../hashing/hasher.go),
[`hashing/bcrypt.go`](../hashing/bcrypt.go),
[`hashing/manager.go`](../hashing/manager.go),
[`app/Providers/AppServiceProvider.go`](../app/Providers/AppServiceProvider.go)
(the `"hash"` binding)

Golite's `hashing` package is the equivalent of `Illuminate\Hashing` — the
`Hash` facade's one-way, adaptive password hashing, as distinct from
[`encryption`](encryption.md) (which is reversible). It replaces the
SHA-256 stand-in that used to live directly in `AppServiceProvider.go`:
SHA-256 is *fast*, exactly the wrong property for password hashing (it
makes brute-forcing cheap); bcrypt is deliberately slow and salts every
hash automatically.

## The `Hasher` interface and drivers

```go
// hashing/hasher.go
type Hasher interface {
	Make(value string) string
	Check(value, hashedValue string) bool
	NeedsRehash(hashedValue string) bool
}
```

`BcryptHasher` (`hashing/bcrypt.go`) is the only built-in driver, wrapping
`golang.org/x/crypto/bcrypt` — the same default Laravel ships with:

```go
hasher := hashing.NewBcryptHasher(10) // cost <= 0 falls back to bcrypt.DefaultCost
hashed := hasher.Make("s3cr3t")       // "$2a$10$..."
hasher.Check("s3cr3t", hashed)        // true, constant-time
hasher.NeedsRehash(hashed)            // true if hashed's cost != 10
```

`Make` panics rather than returning an error — bcrypt only fails for a
cost outside its valid range or a value over 72 bytes, both
configuration/programmer errors, not conditions a caller can meaningfully
recover from per-call. This also keeps `Make`'s signature call-compatible
with the dummy `Hasher` it replaces, so
[`PostController`](../app/Http/Controllers/PostController.go)'s and
[`UserController`](../app/Http/Controllers/UserController.go)'s local
`Hasher`/`hashService` interfaces (`Make(string) string`) needed no
changes at all.

## `Manager` — the `"hash"` container binding

```go
// hashing/manager.go
type Manager struct { /* ... */ }

func NewManager(defaultDriver string) *Manager
func (m *Manager) Extend(name string, h Hasher)
func (m *Manager) Driver(name string) Hasher // panics if name isn't registered
func (m *Manager) Make(value string) string
func (m *Manager) Check(value, hashedValue string) bool
func (m *Manager) NeedsRehash(hashedValue string) bool
```

`Manager` is a driver registry — the same shape as
[`app/Http/Session/SessionManager`](sessions.md) — and implements `Hasher`
itself by delegating to its configured default driver, so it drops
straight into any code that only knew about a bare `Hasher` before.
`AppServiceProvider.Register` builds and binds it:

```go
hasher := hashing.NewManager(cfg.Hash.Driver) // HASH_DRIVER, default "bcrypt"
hasher.Extend("bcrypt", hashing.NewBcryptHasher(cfg.Hash.BcryptCost)) // HASH_BCRYPT_COST, default 10
c.Bind("hash", hasher)
```

Registering a second driver (e.g. an Argon2id implementation) is just
another `Extend` call, mirroring `Hash::extend`.

## Usage

```go
hasher := c.App.Make("hash").(*hashing.Manager)
hashed := hasher.Make(password)
if hasher.Check(candidate, hashed) {
	// ...
}
```

## Demo routes

`POST /hash/make` and `POST /hash/check`, handled by
[`HashController`](../app/Http/Controllers/HashController.go) (`Make`/
`Check`, constructor-injected with the same `Hasher` interface
`PostController` uses — wired up in [`routes/web.go`](../routes/web.go)),
hash a password and verify a candidate against it.
