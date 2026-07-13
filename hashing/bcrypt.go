package hashing

import "golang.org/x/crypto/bcrypt"

// BcryptHasher is Golite's default Hasher — bcrypt, the same default
// Laravel ships with, via the standard golang.org/x/crypto/bcrypt
// implementation.
type BcryptHasher struct {
	// Cost is bcrypt's work factor (4-31). Higher is slower and more
	// resistant to brute-forcing; Laravel's own default is 10.
	Cost int
}

// NewBcryptHasher builds a BcryptHasher with the given cost, falling back
// to bcrypt.DefaultCost (10) for cost <= 0.
func NewBcryptHasher(cost int) *BcryptHasher {
	if cost <= 0 {
		cost = bcrypt.DefaultCost
	}
	return &BcryptHasher{Cost: cost}
}

// Make hashes value with bcrypt. bcrypt.GenerateFromPassword only fails for
// a cost outside bcrypt's valid range or a value over 72 bytes — both
// programmer/configuration errors rather than conditions a caller can
// meaningfully recover from at the call site — so Make panics instead of
// returning an error, keeping its signature call-compatible with the
// dummy Hasher it replaces (see app/Http/Controllers/PostController.go's
// and UserController.go's local Hasher/hashService interfaces, which
// neither needed to change).
func (b *BcryptHasher) Make(value string) string {
	hashed, err := bcrypt.GenerateFromPassword([]byte(value), b.Cost)
	if err != nil {
		panic("golite/hashing: failed to hash value: " + err.Error())
	}
	return string(hashed)
}

// Check reports whether value matches hashedValue.
// bcrypt.CompareHashAndPassword itself runs in constant time.
func (b *BcryptHasher) Check(value, hashedValue string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedValue), []byte(value)) == nil
}

// NeedsRehash reports whether hashedValue's embedded cost differs from
// this hasher's configured Cost.
func (b *BcryptHasher) NeedsRehash(hashedValue string) bool {
	cost, err := bcrypt.Cost([]byte(hashedValue))
	if err != nil {
		return true
	}
	return cost != b.Cost
}
