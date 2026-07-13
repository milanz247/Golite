package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// ErrInvalidPayload is returned when a signed/encrypted session payload
// (used by CookieSessionHandler) is missing, malformed, or fails
// authentication.
var ErrInvalidPayload = errors.New("golite/session: invalid or tampered session payload")

// generateRandomToken returns a URL-safe, base64-encoded random token
// backed by crypto/rand — used for both session IDs and CSRF tokens. Its
// output alphabet (base64.RawURLEncoding: A-Za-z0-9-_) is deliberately
// free of "/" and ".", which matters for FileSessionHandler: a session ID
// generated here can never be used to escape its storage directory (see
// isValidSessionID, which rejects anything a legitimate ID couldn't be).
func generateRandomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read only fails if the OS CSPRNG is unavailable, a
		// condition no caller can meaningfully recover from.
		panic("golite/session: failed to generate a secure token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// encryptValue authenticated-encrypts plaintext with AES-256-GCM under
// key, returning a base64url-encoded "nonce || ciphertext || tag" blob —
// the same scheme app/Http/Cookie.go uses for encrypted cookies,
// duplicated here (rather than imported) because this package must not
// depend on app/Http: app/Http needs to import this package for the
// Session type, and the reverse import would be a cycle.
func encryptValue(key []byte, plaintext string) (string, error) {
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

// decryptValue reverses encryptValue. Any failure — bad base64, wrong
// length, or a failed GCM authentication check — collapses to
// ErrInvalidPayload.
func decryptValue(key []byte, encoded string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidPayload
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
		return "", ErrInvalidPayload
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidPayload
	}
	return string(plaintext), nil
}

// isValidSessionID reports whether id could plausibly be one
// generateRandomToken produced — base64url characters only, non-empty.
// SessionManager.Load checks this before ever passing a client-supplied
// cookie value to a Handler, since e.g. FileSessionHandler builds a
// filesystem path from the ID: a malicious cookie value like
// "../../etc/passwd" must never reach os.ReadFile/WriteFile. Any ID
// failing this check is treated exactly like an unknown one — a fresh
// session is started instead.
func isValidSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
