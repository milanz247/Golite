# Validation

Files: [`validation/validator.go`](../validation/validator.go),
[`validation/rules.go`](../validation/rules.go),
[`validation/errors.go`](../validation/errors.go),
[`validation/util.go`](../validation/util.go),
[`app/Http/Context.go`](../app/Http/Context.go) (`Context.Validate`)

Golite's `validation` package is the equivalent of `Illuminate\Validation`
— pipe-separated rule strings (`"required|email|min:3"`) checked against a
plain `map[string]any` payload, designed to run directly against
`Context.All()`'s unified input (query + JSON/form body; see
[http-requests.md](http-requests.md)).

## `Validator`

```go
v := validation.Make(data, map[string]string{
	"name":  "required|string|min:2",
	"email": "required|email",
})

v.Fails()       // bool
v.Passes()      // bool
v.Errors()      // validation.Errors — map[string][]string
v.Validated()   // (map[string]any, error) — only the declared fields, or *Exception on failure
```

`Validator` runs its rules lazily, once, the first time any of
`Fails`/`Passes`/`Errors`/`Validated` is called — every subsequent call on
the same `*Validator` reuses that result.

### Presence semantics (matching Laravel, not a simplification)

A rule other than `required` simply doesn't fire against a field that's
absent from `data` or empty (`""`, `nil`, an empty slice) — only
`required` itself reports that as a failure. This is genuine Laravel
behavior, not a shortcut: it's what lets `"email"` alone (no `nullable`)
correctly treat a field the client just didn't send as valid, while still
catching a field that *was* sent with a malformed value.

## Built-in rules

| Rule | Checks |
|---|---|
| `required` | present and non-empty |
| `nullable` | always passes — documents "this field is optional" |
| `string` | Go `string` |
| `numeric` | a number, or a string parseable as one |
| `integer` | a whole number, or a string parseable as one |
| `boolean` | `bool`, or `"1"/"0"/"true"/"false"/"yes"/"no"` |
| `email` | `net/mail.ParseAddress` |
| `url` | has both a scheme and a host |
| `min:n` | string rune-length / slice length / number >= n |
| `max:n` | string rune-length / slice length / number <= n |
| `size:n` | string rune-length / slice length / number == n |
| `in:a,b,c` | value equals one of the listed options |
| `alpha` | letters only |
| `alpha_num` | letters and digits only |
| `confirmed` | matches `<field>_confirmation` |
| `same:other` | matches another field's value |
| `different:other` | differs from another field's value |

Nested fields use dot notation (`"address.city": "required"`), resolved
against nested `map[string]any` values the same way JSON bodies decode.

### Custom rules — `validation.Extend`

```go
type RuleFunc func(field string, value any, params []string, data map[string]any) string // "" = valid

validation.Extend("even", func(field string, value any, _ []string, _ map[string]any) string {
	n, ok := value.(float64)
	if !ok || int(n)%2 != 0 {
		return fmt.Sprintf("The %s field must be even.", field)
	}
	return ""
})
```

`Extend` also overrides a built-in rule of the same name, mirroring
`Validator::extend`.

## `Context.Validate` — automatic 422s

```go
func (c *Context) Validate(rules map[string]string) map[string]any
```

`Context.Validate` is the realistic entry point: it validates
`c.All()` and, on success, returns the validated subset — but on
**failure, it panics** with a `*validation.Exception` instead of
returning an error, mirroring Laravel's `$request->validate($rules)`
throwing `ValidationException` automatically. Paired with
[`RecoverMiddleware`](error-handling.md) (registered globally in
`public/main.go`), that panic is caught and rendered as a 422 response
with field errors — no handler needs its own `if v.Fails() { ... }`
branch:

```go
kernel.POST("/register", apphttp.Responder(func(c *apphttp.Context) any {
	validated := c.Validate(map[string]string{
		"name":     "required|string|min:2",
		"email":    "required|email",
		"password": "required|min:6|confirmed",
	})
	return map[string]any{"status": "registered", "user": validated}
}))
```

A failing request to this route gets, with no code in the handler for it:

```json
{
  "message": "the given data was invalid",
  "errors": {
    "email": ["The email field must be a valid email address."],
    "password": ["The password field must be at least 6.", "The password field confirmation does not match."]
  }
}
```

`Context.Validate` requires `RecoverMiddleware` to be registered — without
it, the panic reaches Go's own `net/http` per-connection recover instead,
which drops the connection with no response body. Code that wants manual
control instead of the panic-based flow can call `validation.Make(...)`
directly and branch on `Fails()`/`Errors()` itself; see `/account` in
[`routes/web.go`](../routes/web.go) for that older, non-panicking style
alongside `/register` for the automatic one.

## Demo route

`POST /register` in [`routes/web.go`](../routes/web.go), described above.
