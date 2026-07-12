package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"strings"
)

// resolveInput lazily builds the unified input payload — query string
// parameters overlaid with the request body (JSON, url-encoded form, or
// multipart form fields; whichever the Content-Type indicates) — caching
// the result on the Context so repeated calls within one request are
// cheap and consistent. Body values take precedence over query values on
// key collision, matching Laravel's Request::all().
//
// Reading a JSON body consumes c.Request.Body, so it's restored via a new
// reader over the bytes already read, exactly like the standard library's
// own ParseForm/ParseMultipartForm cache their result on *http.Request and
// make later calls safe to repeat — so resolveInput, MethodSpoofing's and
// VerifyCsrfToken's PostFormValue calls, and any handler code can all call
// into this machinery in any order without conflicting.
func (c *Context) resolveInput() {
	if c.inputResolved {
		return
	}
	c.inputResolved = true
	c.input = make(map[string]any)

	for key, values := range c.Request.URL.Query() {
		c.input[key] = flattenInputValues(values)
	}

	mediaType, _, _ := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))

	if mediaType == "application/json" {
		data, err := io.ReadAll(c.Request.Body)
		if err == nil {
			c.Request.Body = io.NopCloser(bytes.NewReader(data))
		}
		if len(data) > 0 {
			var body map[string]any
			if err := json.Unmarshal(data, &body); err == nil {
				for k, v := range body {
					c.input[k] = v
				}
			}
		}
		return
	}

	// application/x-www-form-urlencoded and multipart/form-data both end
	// up populating r.PostForm (body-only — never query values) once
	// parsed; ParseMultipartForm parses the url-encoded case too.
	_ = c.Request.ParseMultipartForm(32 << 20)
	for key, values := range c.Request.PostForm {
		c.input[key] = flattenInputValues(values)
	}
}

// flattenInputValues collapses a single-element []string (the common case
// for a form field or query parameter) down to a plain string, matching
// how Laravel's ParameterBag exposes single values; a genuinely
// multi-valued field (e.g. repeated "tag=a&tag=b") stays a []string.
func flattenInputValues(values []string) any {
	if len(values) == 1 {
		return values[0]
	}
	return values
}

// All returns a copy of the entire unified input payload (query + body).
func (c *Context) All() map[string]any {
	c.resolveInput()
	out := make(map[string]any, len(c.input))
	for k, v := range c.input {
		out[k] = v
	}
	return out
}

// Input returns a single value from the unified input payload, or
// defaultValue[0] if key isn't present.
func (c *Context) Input(key string, defaultValue ...any) any {
	c.resolveInput()
	if v, ok := c.input[key]; ok {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// Query returns a single value from the URL query string only — never the
// request body — or defaultValue[0] if key isn't present.
func (c *Context) Query(key string, defaultValue ...string) string {
	if v := c.Request.URL.Query().Get(key); v != "" {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// Has reports whether every given key is present in the unified input.
func (c *Context) Has(keys ...string) bool {
	c.resolveInput()
	for _, key := range keys {
		if _, ok := c.input[key]; !ok {
			return false
		}
	}
	return true
}

// HasAny reports whether at least one given key is present in the unified
// input.
func (c *Context) HasAny(keys ...string) bool {
	c.resolveInput()
	for _, key := range keys {
		if _, ok := c.input[key]; ok {
			return true
		}
	}
	return false
}

// Only returns a copy of the unified input restricted to the given keys
// (absent keys are simply omitted).
func (c *Context) Only(keys ...string) map[string]any {
	c.resolveInput()
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if v, ok := c.input[key]; ok {
			out[key] = v
		}
	}
	return out
}

// Except returns a copy of the unified input with the given keys removed.
func (c *Context) Except(keys ...string) map[string]any {
	c.resolveInput()
	excluded := make(map[string]bool, len(keys))
	for _, key := range keys {
		excluded[key] = true
	}
	out := make(map[string]any, len(c.input))
	for k, v := range c.input {
		if !excluded[k] {
			out[k] = v
		}
	}
	return out
}

// Boolean interprets the input at key the way Laravel's Request::boolean()
// does: "1", "true", "on", and "yes" (case-insensitive, surrounding
// whitespace ignored) are true; a missing key, or any other value, is
// false.
func (c *Context) Boolean(key string) bool {
	value := c.Input(key)
	if value == nil {
		return false
	}
	s, ok := value.(string)
	if !ok {
		s = fmt.Sprint(value)
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// Merge overlays data onto the unified input, overwriting any existing
// keys — equivalent to Laravel's Request::merge().
func (c *Context) Merge(data map[string]any) {
	c.resolveInput()
	for k, v := range data {
		c.input[k] = v
	}
}

// MergeIfMissing overlays data onto the unified input without overwriting
// keys that are already present — equivalent to Request::mergeIfMissing().
func (c *Context) MergeIfMissing(data map[string]any) {
	c.resolveInput()
	for k, v := range data {
		if _, exists := c.input[k]; !exists {
			c.input[k] = v
		}
	}
}

// stringifyInputValue renders an input value (as stored in c.input, so a
// string, a []string, or anything JSON-decoded — nil, bool, float64,
// map[string]any, []any) as a single string, for Context.Flash to persist
// into the session (which, like Context.Old's return type, is
// string-only).
func stringifyInputValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}
