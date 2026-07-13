# HTTP Request Handling

Files: [`app/Http/Context.go`](../app/Http/Context.go),
[`app/Http/Input.go`](../app/Http/Input.go),
[`app/Http/Cookie.go`](../app/Http/Cookie.go),
[`app/Http/UploadedFile.go`](../app/Http/UploadedFile.go),
[`app/Http/Middleware/TrimStringsMiddleware.go`](../app/Http/Middleware/TrimStringsMiddleware.go),
[`app/Http/Middleware/ConvertEmptyStringsToNullMiddleware.go`](../app/Http/Middleware/ConvertEmptyStringsToNullMiddleware.go),
[`app/Http/Middleware/TrustProxiesMiddleware.go`](../app/Http/Middleware/TrustProxiesMiddleware.go),
[`app/Http/Middleware/TrustHosts.go`](../app/Http/Middleware/TrustHosts.go)

`Context` is Golite's request object, matching the surface area of
Laravel's `Illuminate\Http\Request`: inspection helpers, a unified input
payload merging query/JSON/form data, encrypted cookies, flash/old input,
and file uploads — plus the global middleware that normalizes and secures
that input before a handler ever sees it.

## Request inspection helpers

All in `Context.go`:

```go
c.Path()                    // "/posts/5" — no query string
c.Is("admin/*")              // wildcard match against the path ("*" anywhere)
c.Url()                      // "http://host/posts/5" — no query string
c.FullUrl()                  // "http://host/posts/5?ref=email" — with it
c.Method()                   // "POST" (reflects MethodSpoofing overrides)
c.IsMethod("POST")           // case-insensitive
c.Ip()                       // client IP — see the security note below
c.Header("X-Custom", "def")  // header value, or a default
c.HasHeader("X-Custom")      // present at all, even if empty (unlike Header)
c.BearerToken()              // "Authorization: Bearer xyz" -> "xyz"
c.ExpectsJson()               // Accept: */json, +json suffix, or X-Requested-With: XMLHttpRequest
```

`Is` supports `*` anywhere in the pattern (`wildcardMatch` splits on `*`,
`regexp.QuoteMeta`s each literal segment, and joins with `.*`), not just as
a trailing wildcard.

### `Ip()` is deliberately dumb — `TrustProxiesMiddleware` is what makes it safe

```go
func (c *Context) Ip() string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}
```

`Ip()` only ever reads `Request.RemoteAddr` — **never** an
`X-Forwarded-For`/`X-Real-IP` header directly, because those are ordinary
client-supplied headers an attacker can set to anything. The only thing
that should ever promote a forwarded address into `RemoteAddr` is
[`TrustProxiesMiddleware`](#trustproxiesmiddleware), and only after
validating the *immediate* peer is a proxy you actually trust. This is the
same "secure by construction" shape as [CSRF](security-csrf.md)'s
double-submit cookie: the safety property lives in one place, and every
consumer downstream of it (`Ip()`, and anything that calls it — rate
limiting, audit logs, ...) inherits it for free.

## The unified input payload

```go
c.All() map[string]any                                   // everything, merged
c.Input(key string, defaultValue ...any) any              // one value, with a default
c.Query(key string, defaultValue ...string) string         // query string ONLY, never body
c.Has(keys ...string) bool                                 // every key present
c.HasAny(keys ...string) bool                               // at least one key present
c.Only(keys ...string) map[string]any                        // subset
c.Except(keys ...string) map[string]any                       // everything but
c.Boolean(key string) bool                                     // "1"/"true"/"on"/"yes" -> true
c.Merge(data map[string]any)                                    // overlay, overwriting
c.MergeIfMissing(data map[string]any)                             // overlay, keeping existing
```

### How it's built — `resolveInput`

`c.input` is built lazily, once per request, and cached:

1. **Query string** first — every `URL.Query()` key becomes an entry.
2. **Body, overlaid on top** (body wins on key collision — Laravel's same
   precedence), based on `Content-Type`:
   - `application/json` → `io.ReadAll` the body, `json.Unmarshal` into
     `map[string]any`, then **restore `Request.Body`** from the bytes
     already read (`io.NopCloser(bytes.NewReader(data))`) so anything else
     that wants to read the raw body later still can.
   - anything else (`application/x-www-form-urlencoded`,
     `multipart/form-data`, or no body) → `r.ParseMultipartForm` (which
     also handles the url-encoded case), then `r.PostForm` — body values
     only, never query ones, which is exactly the "body" half this needs.

A single-valued field collapses to a plain `string`; a genuinely
multi-valued one (`?tag=a&tag=b`) stays a `[]string`.

**This composes safely with everything else that reads the body.**
`MethodSpoofingMiddleware` and `VerifyCsrfToken` both call
`r.PostFormValue(...)`, which internally triggers the same
`ParseForm`/`ParseMultipartForm` machinery `resolveInput` uses — and Go's
`*http.Request` caches that parse, so calling it again later (from
`resolveInput`, in a handler) reuses the cached result rather than
re-reading an already-consumed body. For JSON bodies specifically, neither
of those two middleware ever touches the body at all (Go's `ParseForm`
only reads it for exactly `application/x-www-form-urlencoded`), so it's
still untouched — and unread — by the time `resolveInput` gets to it.

### `Only`/`Except` don't include files

Files are handled entirely separately (see [below](#file-uploads)) — the
unified payload is text/JSON values only. Mixing `*UploadedFile` into a
`map[string]any` would complicate `Only`/`Except`/`Boolean` for little
benefit, since file-handling code almost always wants `HasFile`/`File`
directly rather than pulling a file out of a generic map.

## Encrypted, authenticated cookies

```go
c.SetCookie(name, value string, maxAge int) error
c.Cookie(name string) (string, error)
```

Both are backed by AES-256-GCM (`app/Http/Cookie.go`):

```go
func encryptCookieValue(key []byte, plaintext string) (string, error) {
	// AES-256-GCM: nonce || Seal(nonce, plaintext) -> base64url
}
func decryptCookieValue(key []byte, encoded string) (string, error) {
	// reverse; any failure (bad base64, wrong length, failed auth tag) -> ErrInvalidCookie
}
```

GCM is an AEAD cipher, so one primitive gives both **confidentiality**
(the cookie's contents aren't readable without the key) and **integrity**
— tampering fails the authentication tag check and `decryptCookieValue`
returns `ErrInvalidCookie` rather than silently accepting garbage. That
integrity property is what Laravel calls a cookie being "signed"; AEAD
gets you that and encryption from a single call, rather than two separate
mechanisms.

`SetCookie` sets `HttpOnly`, `SameSite=Lax`, and `Secure` (via
`IsSecureRequest`, same helper used elsewhere) automatically.

### `Kernel.appKey`: generated per-process, not loaded from config

```go
appKey: generateAppKey(), // crypto/rand, 32 bytes — in NewKernel
```

Unlike Laravel's `APP_KEY` (a config value meant to survive restarts and
be shared across a deployment), Golite generates a fresh key with
`crypto/rand` every time `NewKernel` runs. This is a deliberate, documented
simplification, not an oversight — it's the same tradeoff already made for
the default `"memory"` session driver (see [sessions.md](sessions.md)):
a lightweight, single-process framework with no persistence story yet is
honestly served by a key that doesn't survive a restart, rather than a
half-implemented "persistent" key that's actually just as ephemeral in
practice. **Practical implication:** a cookie set before a restart won't
decrypt after one — `Context.Cookie` returns `ErrInvalidCookie`, not a
crash, so this fails safe. (This same `appKey` also encrypts the stateless
`"cookie"` session driver's payload — see [sessions.md](sessions.md#the-handler-interface-and-built-in-drivers).)

## Flash data and `Old` input

```go
c.Flash()             // copy the current unified input into the session
c.Old(key string) string  // read a value flashed on the *previous* request
```

The classic use is repopulating a form after a validation failure:

```go
kernel.POST("/contact", func(c *apphttp.Context) {
	if !c.Has("email") {
		// .WithInput() does the same thing c.Flash() + c.Redirect() used to
		// do as two steps — see docs/responses.md for the fluent Response
		// factory this now goes through.
		c.Redirect("/contact", http.StatusFound).WithInput().Send(c)
		return
	}
	// ...
}).Middleware("session", "csrf")

kernel.GET("/contact", func(c *apphttp.Context) {
	c.JSON(http.StatusOK, map[string]string{
		"old_email":  c.Old("email"),
		"csrf_token": c.CsrfToken(),
	})
}).Middleware("session", "csrf")
```

### One-shot semantics: visible on the *next* request, then gone

Laravel's flash data survives for exactly one additional request, not
indefinitely — `Context.Flash`/`Old` are a thin, form-specific layer over
the session engine's own `Flash`/`Get` (see
[sessions.md](sessions.md#flash-data) for the full two-key-set rotation
mechanism, `Session.ageFlash`, called once per request from
`Manager.Load`).

So data flashed on request *N* is invisible on *N* itself, becomes readable
via `Old()` on request *N+1*, and is gone by *N+2*. Verified directly:
flashing a value, reading it back on the immediately following request,
then confirming a third request no longer sees it.

`Old` returns a `string` (matching its exact signature), so `Flash`
stringifies each input value first — a plain `string` passes through, a
`[]string` joins with `", "`, and anything else (from a JSON body) is
`json.Marshal`ed.

## File uploads

```go
c.HasFile(key string) bool
c.File(key string) (*UploadedFile, error)
```

```go
type UploadedFile struct {
	Filename string // client-submitted — untrusted, never used as a path directly
	Size     int64
}

func (f *UploadedFile) IsValid() bool
func (f *UploadedFile) Path() string
func (f *UploadedFile) Extension() string
func (f *UploadedFile) Store(destinationDir string) (string, error)
func (f *UploadedFile) StoreAs(destinationDir, filename string) (string, error)
```

### `Path()` is always valid — Golite copies every upload to a real temp file

Go's own multipart parser only spills an upload to disk once it exceeds
the in-memory threshold passed to `ParseMultipartForm`; a small file stays
entirely in memory, with no path at all. Since `Path()` needs to work
unconditionally (matching PHP, which always spools uploads to a temp
file), `Context.File` copies every upload — regardless of size — into a
fresh `os.CreateTemp` file. The tradeoff is one extra disk write even for
tiny uploads, in exchange for `Path()` never needing a "sometimes this
returns an error" escape hatch.

Every temp file `File()` creates is tracked on the `Context` and removed
automatically once the request finishes (`Context.cleanupTempFiles`,
called from `Kernel.ServeHTTP` after the response is sent) — unless
`Store`/`StoreAs` already moved it elsewhere, in which case there's
nothing left at the original path to remove (the `os.Remove` call still
runs; its "not found" error is simply ignored). Verified: no
`golite-upload-*` files left behind in the OS temp directory after a
request that called `Store`, nor after one that didn't.

### `Extension()` sniffs content, never trusts the filename or `Content-Type`

```go
detected := http.DetectContentType(buf[:n]) // first 512 bytes
```

Both the client-submitted filename and the `Content-Type` the client sent
alongside the upload are attacker-controlled. `Extension()` instead sniffs
the actual bytes via the standard library's `http.DetectContentType`, maps
that to a MIME type, and resolves an extension from `mime.ExtensionsByType`
— falling back to the submitted filename's extension only if detection
comes back empty.

### `Store`/`StoreAs`: move first, copy as a fallback

```go
if err := os.Rename(f.tempPath, destPath); err == nil {
	f.tempPath = destPath
	return destPath, nil
}
// ... open + io.Copy + remove original, for cross-device destinations
```

`os.Rename` is atomic and cheap when the temp file and destination are on
the same filesystem (the common case — both usually live under the same
app storage volume); `StoreAs` falls back to copy-then-remove only when
`Rename` fails, e.g. a destination on a different mounted volume.

`Store` generates the filename itself — a random 16-byte hex string plus
the sniffed extension — specifically so a caller never has to sanitize the
client-submitted filename (path traversal, reserved device names, ...) to
use it safely. `StoreAs` takes an explicit filename for when a caller
wants control, but it's still the caller's job to make sure that filename
is safe if it's derived from anything user-supplied.

## Normalization and trust middleware

Four new global middleware, all registered in
[`public/main.go`](../public/main.go) (not `Kernel.go` — see
[middleware.md](middleware.md) for why the kernel can't reference concrete
middleware types from the `Middleware` subpackage without an import
cycle):

```go
app.Kernel.UseMiddleware(
	appMiddleware.NewTrustHosts(),                       // no patterns = disabled
	appMiddleware.NewTrustProxies("127.0.0.1", "::1"),
	appMiddleware.MethodSpoofing(),
	appMiddleware.TrimStrings(),
	appMiddleware.ConvertEmptyStringsToNull(),
	appMiddleware.Logger(),
)
```

Order matters here: host/proxy trust must be resolved before anything
reads the client's address or the `Host` header; method spoofing before
routing; `TrimStrings` before `ConvertEmptyStringsToNull` (so a
whitespace-only value like `"   "` is trimmed to `""` first and therefore
still gets nullified); `Logger` last, so it captures the whole request.

### `TrimStringsMiddleware` / `ConvertEmptyStringsToNullMiddleware`

Both work the same way: force-resolve the unified input via `c.All()`,
transform every value, and write the result back with `c.Merge()`.
`TrimStrings` trims leading/trailing whitespace from every `string` (and
every element of a `[]string`); `ConvertEmptyStringsToNull` replaces an
exactly-empty `string` with `nil`, **keeping the key present** — so
handler code can tell "field submitted blank" apart from "field never
submitted at all" the same way a JSON API client could by sending `null`
versus omitting the key.

### `TrustProxiesMiddleware`

```go
type TrustProxies struct {
	Proxies []string // IPs, CIDR ranges, or "*"
}
```

Rewrites `Request.RemoteAddr` to the left-most address in
`X-Forwarded-For` — **only if** the current `RemoteAddr` (the immediate
TCP peer) matches an entry in `Proxies`. Without this middleware (or with
an empty `Proxies` list), `X-Forwarded-For` is never consulted at all, and
`Context.Ip()` — along with anything built on it — reflects the raw TCP
peer.

### `TrustHosts`

```go
type TrustHosts struct {
	Patterns []string // exact hosts, or "*.example.com" for any subdomain
}
```

Rejects a request with `400 Bad Request` if its `Host` header doesn't
match any configured pattern. An empty `Patterns` (the default in
`public/main.go`'s demo) disables the check — a lightweight framework has
no way to know a deployment's real domain(s) in advance, so this needs
explicit configuration to do anything, unlike `TrimStrings`/
`ConvertEmptyStringsToNull`, which are safe defaults everywhere. Guards
against Host header injection: code that builds absolute URLs (password
reset links, redirects) from the incoming `Host` header rather than a
fixed domain can be tricked into pointing at an attacker's server if the
header isn't validated first.

## Known simplifications

- **`Kernel.appKey` doesn't survive a restart** — see
  [above](#kernelappkey-generated-per-process-not-loaded-from-config).
  Same tradeoff as the default `"memory"` session driver; both are
  documented, both fail safe rather than silently.
- **No request size limits beyond the 32 MiB passed to
  `ParseMultipartForm`.** A production deployment fielding untrusted
  uploads should add its own limit (e.g. wrapping `Request.Body` in
  `http.MaxBytesReader`) ahead of anything that calls `File`/`All`.
- **`Extension()`'s tie-break among multiple valid extensions for one MIME
  type is "shortest string," not "most conventional."** `text/plain` can
  resolve to `.asc` instead of the more expected `.txt`, depending on what
  `mime.ExtensionsByType` returns on a given system. Still a genuinely
  valid extension for the detected content type — just not always the one
  a human would pick first.

See [middleware.md](middleware.md) for how these fit into the middleware
pipeline generally, and [sessions.md](sessions.md) for the session engine
`Flash`/`Old`/`CsrfToken` all build on.
