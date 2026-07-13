package session

// CookieSessionHandler is Golite's stateless session driver: the entire
// session payload is encrypted and carried in the browser's own cookie,
// rather than being looked up by ID from any server-side store — Golite's
// equivalent of Laravel's "cookie" session driver.
//
// It still implements Handler, satisfying Manager's uniform driver
// contract, but Read/Write/Destroy/Gc are all no-ops: there is nothing
// server-side to read, write, destroy, or garbage-collect. The real work
// happens through Encode/Decode, which Manager calls directly instead of
// Read/Write whenever this driver is active (see Manager.Load/Save) —
// PHP's SessionHandlerInterface has no notion of "the ID and the payload
// are the same thing," so this driver can't be made to fit that shape
// without those two extra, driver-specific methods.
type CookieSessionHandler struct {
	key []byte
}

// NewCookieSessionHandler creates a stateless handler that
// authenticated-encrypts session payloads with key (AES-256-GCM — the
// same primitive app/Http/Cookie.go uses for Context.SetCookie, so this
// driver is exactly as secure as any other encrypted cookie Golite sets).
func NewCookieSessionHandler(key []byte) *CookieSessionHandler {
	return &CookieSessionHandler{key: key}
}

func (h *CookieSessionHandler) Read(id string) (string, error) { return "", nil }
func (h *CookieSessionHandler) Write(id, data string) error    { return nil }
func (h *CookieSessionHandler) Destroy(id string) error        { return nil }
func (h *CookieSessionHandler) Gc(lifetime int)                {}

// Encode encrypts payload for safe storage directly in a cookie value.
func (h *CookieSessionHandler) Encode(payload string) (string, error) {
	return encryptValue(h.key, payload)
}

// Decode reverses Encode, verifying the payload's authenticity —
// returning ErrInvalidPayload for anything tampered with or encrypted
// under a different key.
func (h *CookieSessionHandler) Decode(raw string) (string, error) {
	return decryptValue(h.key, raw)
}
