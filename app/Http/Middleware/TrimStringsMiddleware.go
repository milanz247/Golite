package middleware

import (
	"strings"

	apphttp "Golite/app/Http"
)

// TrimStrings trims leading and trailing whitespace from every string
// value in the unified input payload (query + body) — Golite's equivalent
// of Laravel's global TrimStrings middleware. It must run before
// ConvertEmptyStringsToNull (see that middleware's doc comment) so a
// whitespace-only value like "   " is trimmed to "" first, and therefore
// still gets nullified.
func TrimStrings() apphttp.Middleware {
	return apphttp.MiddlewareFunc(func(c *apphttp.Context, next func()) {
		trimmed := make(map[string]any)
		for key, value := range c.All() {
			trimmed[key] = trimInputValue(value)
		}
		c.Merge(trimmed)
		next()
	})
}

func trimInputValue(value any) any {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		out := make([]string, len(v))
		for i, s := range v {
			out[i] = strings.TrimSpace(s)
		}
		return out
	default:
		return value
	}
}
