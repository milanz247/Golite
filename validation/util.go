package validation

import "sort"

// sortStrings is a tiny indirection so errors.go doesn't need its own
// "sort" import line duplicated across files; kept here alongside the
// other small data-shape helpers this package needs.
func sortStrings(s []string) {
	sort.Strings(s)
}

// lookup resolves a possibly dot-notated field name (e.g. "address.city")
// against data, mirroring Laravel's Arr::get used throughout the
// Validator. It returns ok=false if any segment along the path is absent
// or not a nested map.
func lookup(data map[string]any, field string) (any, bool) {
	var cur any = data
	start := 0
	for i := 0; i <= len(field); i++ {
		if i == len(field) || field[i] == '.' {
			segment := field[start:i]
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			v, ok := m[segment]
			if !ok {
				return nil, false
			}
			cur = v
			start = i + 1
		}
	}
	return cur, true
}

// isEmptyValue reports whether value should be treated as "not provided"
// for validation purposes: a missing key, nil, an empty string, or an
// empty slice. Laravel's non-"required"-family rules simply don't fire
// against an empty value (only required/present/filled inspect presence
// itself) — see Validator.run in validator.go.
func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	}
	return false
}
