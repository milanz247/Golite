// Package encryption is Golite's equivalent of Illuminate\Encryption: a
// general-purpose, authenticated-encryption service for application data —
// values an app wants to encrypt at rest (a token stored in a database
// column, a value tucked into a query string, ...), as opposed to the
// framework's own cookie/session payloads.
//
// It is deliberately a separate, independent implementation from
// app/Http/Cookie.go's encryptCookieValue/decryptCookieValue (and
// app/Http/Session/crypto.go's copy of the same). Those two exist to
// authenticated-encrypt Golite's own cookie and session payloads under a
// key that's intentionally regenerated every process restart (see
// Kernel.appKey's doc comment in app/Http/Kernel.go and
// docs/architecture.md) — a deliberate, documented design decision, not an
// oversight. Encrypter here backs Laravel's APP_KEY-equivalent instead: a
// key applications are expected to persist in .env (see config.LoadConfig)
// so encrypted values remain decryptable across restarts. Keeping the two
// independent means neither's key material or lifecycle constrains the
// other.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// KeySize is the required key length in bytes — AES-256.
const KeySize = 32

// ErrInvalidPayload is returned by Decrypt/DecryptString when the payload
// is malformed, was encrypted under a different key, or was tampered with
// — AES-256-GCM's authentication tag makes tampering detectable, and every
// failure mode collapses to this one error rather than leaking which
// specific step failed (the same reasoning as app/Http/Cookie.go's
// ErrInvalidCookie).
var ErrInvalidPayload = errors.New("encryption: the payload is invalid or has been tampered with")

// Encrypter authenticated-encrypts and decrypts values with AES-256-GCM
// under a single fixed key — Golite's equivalent of
// Illuminate\Encryption\Encrypter, the class behind Laravel's Crypt facade.
type Encrypter struct {
	key []byte
}

// GenerateKey returns a new random AES-256 key via crypto/rand — Golite's
// equivalent of `php artisan key:generate`. The caller is responsible for
// persisting it (base64-encoded) somewhere durable, such as APP_KEY in
// .env; see config.LoadConfig for what happens when no such key is found.
func GenerateKey() []byte {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		panic("golite/encryption: failed to generate a random key: " + err.Error())
	}
	return key
}

// NewEncrypter builds an Encrypter from a raw 32-byte AES-256 key. It
// panics if key is the wrong length — a misconfigured key size is a
// programmer/deployment error, not a runtime condition callers should be
// expected to handle per-call.
func NewEncrypter(key []byte) *Encrypter {
	if len(key) != KeySize {
		panic("golite/encryption: key must be exactly 32 bytes (AES-256); use GenerateKey or base64-decode APP_KEY")
	}
	return &Encrypter{key: key}
}

// EncryptString authenticated-encrypts plaintext, returning a
// base64url-encoded "nonce || ciphertext || tag" blob.
func (e *Encrypter) EncryptString(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
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

// DecryptString reverses EncryptString. Any failure — bad base64, wrong
// length, or a failed GCM authentication check — collapses to
// ErrInvalidPayload.
func (e *Encrypter) DecryptString(payload string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", ErrInvalidPayload
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(sealed) < gcm.NonceSize() {
		return "", ErrInvalidPayload
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidPayload
	}
	return string(plaintext), nil
}

// Encrypt JSON-encodes value and encrypts the result — Golite's equivalent
// of Laravel's Crypt::encrypt($value), which serializes non-string values
// before sealing them.
func (e *Encrypter) Encrypt(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return e.EncryptString(string(data))
}

// Decrypt reverses Encrypt into dest, which must be a non-nil pointer —
// Golite's equivalent of Crypt::decrypt($payload).
func (e *Encrypter) Decrypt(payload string, dest any) error {
	plaintext, err := e.DecryptString(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(plaintext), dest)
}
