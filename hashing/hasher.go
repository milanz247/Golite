// Package hashing is Golite's equivalent of Illuminate\Hashing: one-way,
// adaptive password hashing (as distinct from encryption/package, which is
// reversible). It replaces the SHA-256 stand-in that used to live directly
// in app/Providers/AppServiceProvider.go — SHA-256 is fast, which is
// exactly the wrong property for password hashing (it makes brute-forcing
// cheap); bcrypt is deliberately slow and salted per-hash.
package hashing

// Hasher is the contract every hashing driver implements — Golite's
// equivalent of Illuminate\Contracts\Hashing\Hasher.
type Hasher interface {
	// Make hashes value, embedding a fresh random salt and the driver's
	// cost parameter into the returned string so Check/NeedsRehash can
	// later be verified against it alone.
	Make(value string) string

	// Check reports whether value matches the given previously-hashed
	// value, in constant time with respect to the comparison itself.
	Check(value, hashedValue string) bool

	// NeedsRehash reports whether hashedValue was produced with different
	// parameters (e.g. a lower cost) than this driver is currently
	// configured with — call after a successful Check to opportunistically
	// re-hash and upgrade a password's cost as it ages, mirroring
	// Laravel's Hash::needsRehash.
	NeedsRehash(hashedValue string) bool
}
