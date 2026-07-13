package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// newRandomToken generates a fresh, high-entropy token (32 random bytes,
// base64url-encoded) alongside the hex-encoded SHA-256 hash of it —
// shared by remember-me tokens (remember.go) and password reset tokens
// (password_reset.go), both of which follow the same "store only the
// hash, hand the raw value to the client once" shape a password hash
// does, just with a fast hash instead of bcrypt: these tokens are
// already high-entropy random values, not human-guessable passwords, so
// bcrypt's deliberate slowness buys nothing here and would just cost
// every request that checks one.
func newRandomToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// tokenHashEquals reports whether raw hashes to storedHash, in constant
// time with respect to the comparison itself.
func tokenHashEquals(raw, storedHash string) bool {
	computed := hashToken(raw)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
