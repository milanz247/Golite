# Encryption

Files: [`encryption/encrypter.go`](../encryption/encrypter.go),
[`config/app.go`](../config/app.go) (`APP_KEY` loading),
[`app/Providers/AppServiceProvider.go`](../app/Providers/AppServiceProvider.go)
(the `"encrypter"` binding)

Golite's `encryption` package is the equivalent of `Illuminate\Encryption`
— the class behind Laravel's `Crypt` facade: general-purpose,
authenticated encryption for application data (a token before it's stored
in a database column, a value tucked into a signed URL, ...), independent
of the framework's own cookie/session encryption.

## Why a separate encrypter from cookies/sessions

Golite already had AES-256-GCM encryption before this package existed —
`app/Http/Cookie.go`'s `encryptCookieValue`/`decryptCookieValue`, duplicated
again in `app/Http/Session/crypto.go` to avoid an import cycle. Both exist
specifically to protect Golite's own cookie and session payloads, under a
key (`Kernel.appKey`) that's **deliberately regenerated every process
restart** — a documented design decision (see
[architecture.md](architecture.md)) appropriate for a single-process
framework where nothing needs to outlive the process anyway.

`encryption.Encrypter` is a completely independent implementation, backed
by a **persisted** key — `APP_KEY` in `.env`, Laravel's own convention —
so values it encrypts stay decryptable across restarts. Keeping the two
apart means neither's key lifecycle constrains the other: cookies/sessions
never need `APP_KEY` to exist, and application data encrypted via
`Encrypter` never breaks just because the process restarted.

## `Encrypter`

```go
type Encrypter struct { /* unexported key */ }

func GenerateKey() []byte                          // random 32-byte AES-256 key
func NewEncrypter(key []byte) *Encrypter            // panics if len(key) != 32

func (e *Encrypter) EncryptString(plaintext string) (string, error)
func (e *Encrypter) DecryptString(payload string) (string, error)

func (e *Encrypter) Encrypt(value any) (string, error)   // JSON-encodes, then EncryptString
func (e *Encrypter) Decrypt(payload string, dest any) error // reverses Encrypt into dest
```

Every payload is AES-256-GCM sealed as `nonce || ciphertext || tag`, then
base64url-encoded — GCM's authentication tag is what makes tampering
detectable on decryption (the property Laravel calls "signed"), so a
single AEAD primitive gives both confidentiality and integrity, the same
approach `app/Http/Cookie.go` uses for cookies. Any decryption failure —
bad base64, wrong length, or a failed GCM auth check (tampering or a key
mismatch) — collapses to `ErrInvalidPayload` rather than leaking which
step failed.

```go
encrypter := c.App.Make("encrypter").(*encryption.Encrypter)

payload, err := encrypter.EncryptString("a secret message")
// payload -> "5zzrrI0lJeB68pitAcvYhtANaXz789THVVYpj8AMhkkJ5iQ8gEpU"

value, err := encrypter.DecryptString(payload)
// value -> "a secret message"
```

`Encrypt`/`Decrypt` round-trip arbitrary values (not just strings) through
JSON, mirroring `Crypt::encrypt($value)`'s serialization step:

```go
type Preferences struct{ Theme string; Notify bool }

payload, _ := encrypter.Encrypt(Preferences{Theme: "dark", Notify: true})

var prefs Preferences
_ = encrypter.Decrypt(payload, &prefs)
```

## `APP_KEY` and `config.LoadConfig`

`config.AppConfig.Key` is a 32-byte AES-256 key, decoded from the `APP_KEY`
environment variable (either `base64:...`, matching Laravel's own format,
or a bare base64 string). `AppServiceProvider.Register` binds an
`*encryption.Encrypter` built from it into the container under
`"encrypter"`.

If `APP_KEY` is unset or invalid, `config.loadAppKey` generates an
ephemeral key for that process only and logs a warning — the same
"works immediately after a fresh clone, degrades gracefully" tradeoff
`Kernel.appKey` already makes (see [architecture.md](architecture.md)).
Values encrypted under an ephemeral key stop decrypting the moment the
process restarts, so set a real `APP_KEY` in `.env` before relying on
`Encrypter` for anything that needs to survive one:

```bash
openssl rand -base64 32
# APP_KEY=base64:<output> in .env
```

## Demo routes

`GET /crypto/encrypt?value=...` and `GET /crypto/decrypt?payload=...`,
handled by
[`CryptoController`](../app/Http/Controllers/CryptoController.go) (`Encrypt`/
`Decrypt`, constructor-injected with the container's `*encryption.Encrypter`
— wired up in [`routes/web.go`](../routes/web.go)), round-trip a value
through the encrypter and demonstrate a tampered/garbage payload being
rejected with a 422 rather than a decrypted-garbage response.
