package middleware

import (
	apphttp "Golite/app/Http"
)

// ConvertEmptyStringsToNull replaces every empty-string value in the
// unified input payload with nil, while leaving the key present —
// Golite's equivalent of Laravel's global ConvertEmptyStringsToNull
// middleware. An empty `<input>` submitted from an HTML form arrives as
// "" indistinguishably from a deliberately blank string; treating it as
// nil instead lets validation/business logic tell "field left blank"
// apart from "field explicitly set to an empty string" the same way a
// JSON API client can by omitting the key or sending null.
//
// Registered after TrimStrings (see that middleware's doc comment) so a
// whitespace-only submission is nullified too, not just a literal "".
func ConvertEmptyStringsToNull() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		converted := make(map[string]any)
		for key, value := range c.All() {
			converted[key] = nullifyEmptyInputValue(value)
		}
		c.Merge(converted)
		next()
	})
}

func nullifyEmptyInputValue(value any) any {
	if s, ok := value.(string); ok && s == "" {
		return nil
	}
	return value
}
