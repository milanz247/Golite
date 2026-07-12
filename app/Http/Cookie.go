package http

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// ErrInvalidCookie is returned by Context.Cookie when the named cookie is
// missing, malformed, or fails authentication — including a value that was
// encrypted under a different app key (Golite's key is regenerated every
// process restart; see NewKernel and docs/security-csrf.md).
var ErrInvalidCookie = errors.New("golite: invalid or tampered cookie")

// generateAppKey returns a random AES-256 key via crypto/rand, used to
// authenticated-encrypt cookie values — Golite's equivalent of Laravel's
// APP_KEY, generated fresh per process rather than read from config. See
// Kernel.appKey's doc comment for why.
func generateAppKey() []byte {
	key := make([]byte, 32) // AES-256
	if _, err := rand.Read(key); err != nil {
		panic("golite: failed to generate app encryption key: " + err.Error())
	}
	return key
}

// encryptCookieValue authenticated-encrypts plaintext with AES-256-GCM
// under key, returning a base64url-encoded "nonce || ciphertext || tag"
// blob — Golite's equivalent of Laravel's EncryptCookies middleware.
// GCM's authentication tag is what makes a tampered value detectable on
// decryption, the property Laravel calls "signed"; AEAD gives Golite both
// confidentiality and that integrity guarantee from a single primitive.
func encryptCookieValue(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// decryptCookieValue reverses encryptCookieValue. Any failure — bad
// base64, wrong length, or a failed GCM authentication check (tampering,
// or a key mismatch) — collapses to ErrInvalidCookie rather than leaking
// which specific step failed.
func decryptCookieValue(key []byte, encoded string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidCookie
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(sealed) < gcm.NonceSize() {
		return "", ErrInvalidCookie
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return string(plaintext), nil
}
