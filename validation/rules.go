package validation

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// RuleFunc validates a single field's value and returns an error message,
// or "" when the value is valid. params comes from parsing "rule:a,b" (see
// parseRuleSpec); data is the full input payload, needed by cross-field
// rules like "confirmed" and "same". RuleFunc is Golite's equivalent of
// the closure form Laravel's Validator::extend accepts.
type RuleFunc func(field string, value any, params []string, data map[string]any) string

var (
	registryMu sync.RWMutex
	registry   = map[string]RuleFunc{}
)

func init() {
	Extend("required", ruleRequired)
	Extend("nullable", ruleNullable)
	Extend("string", ruleString)
	Extend("numeric", ruleNumeric)
	Extend("integer", ruleInteger)
	Extend("boolean", ruleBoolean)
	Extend("email", ruleEmail)
	Extend("url", ruleURL)
	Extend("min", ruleMin)
	Extend("max", ruleMax)
	Extend("size", ruleSize)
	Extend("in", ruleIn)
	Extend("alpha", ruleAlpha)
	Extend("alpha_num", ruleAlphaNum)
	Extend("confirmed", ruleConfirmed)
	Extend("same", ruleSame)
	Extend("different", ruleDifferent)
}

// Extend registers a validation rule under name — either a new custom
// rule, or an override of a built-in one — mirroring Laravel's
// Validator::extend. Safe to call from multiple goroutines/init functions.
func Extend(name string, fn RuleFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = fn
}

func lookupRule(name string) (RuleFunc, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	fn, ok := registry[name]
	return fn, ok
}

// parseRuleSpec splits one "|"-separated rule entry, e.g. "min:3", into its
// name and comma-separated parameters, mirroring how Laravel parses
// "min:3" out of "required|string|min:3".
func parseRuleSpec(spec string) (name string, params []string) {
	name, rest, hasParams := strings.Cut(spec, ":")
	if !hasParams || rest == "" {
		return name, nil
	}
	return name, strings.Split(rest, ",")
}

func humanize(field string) string {
	return strings.ReplaceAll(field, "_", " ")
}

// --- built-in rules --------------------------------------------------------

func ruleRequired(field string, value any, _ []string, _ map[string]any) string {
	if isEmptyValue(value) {
		return fmt.Sprintf("The %s field is required.", humanize(field))
	}
	return ""
}

func ruleNullable(string, any, []string, map[string]any) string { return "" }

func ruleString(field string, value any, _ []string, _ map[string]any) string {
	if _, ok := value.(string); !ok {
		return fmt.Sprintf("The %s field must be a string.", humanize(field))
	}
	return ""
}

func isNumeric(value any) bool {
	switch v := value.(type) {
	case float64, float32, int, int64:
		return true
	case string:
		_, err := strconv.ParseFloat(v, 64)
		return err == nil
	}
	return false
}

func ruleNumeric(field string, value any, _ []string, _ map[string]any) string {
	if !isNumeric(value) {
		return fmt.Sprintf("The %s field must be a number.", humanize(field))
	}
	return ""
}

func ruleInteger(field string, value any, _ []string, _ map[string]any) string {
	switch v := value.(type) {
	case int, int64:
		return ""
	case float64:
		if v == float64(int64(v)) {
			return ""
		}
	case string:
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			return ""
		}
	}
	return fmt.Sprintf("The %s field must be an integer.", humanize(field))
}

func ruleBoolean(field string, value any, _ []string, _ map[string]any) string {
	switch v := value.(type) {
	case bool:
		return ""
	case string:
		switch strings.ToLower(v) {
		case "1", "0", "true", "false", "yes", "no":
			return ""
		}
	case float64:
		if v == 0 || v == 1 {
			return ""
		}
	}
	return fmt.Sprintf("The %s field must be true or false.", humanize(field))
}

func ruleEmail(field string, value any, _ []string, _ map[string]any) string {
	s, ok := value.(string)
	if !ok {
		return fmt.Sprintf("The %s field must be a valid email address.", humanize(field))
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return fmt.Sprintf("The %s field must be a valid email address.", humanize(field))
	}
	return ""
}

func ruleURL(field string, value any, _ []string, _ map[string]any) string {
	s, ok := value.(string)
	if ok {
		u, err := url.ParseRequestURI(s)
		if err == nil && u.Scheme != "" && u.Host != "" {
			return ""
		}
	}
	return fmt.Sprintf("The %s field must be a valid URL.", humanize(field))
}

// sizeOf returns the "size" Laravel would use for min/max/size: a string's
// rune length, a slice's element count, or a number's own value.
func sizeOf(value any) (float64, bool) {
	switch v := value.(type) {
	case string:
		return float64(len([]rune(v))), true
	case []any:
		return float64(len(v)), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func ruleMin(field string, value any, params []string, _ map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	n, ok := sizeOf(value)
	min, err := strconv.ParseFloat(params[0], 64)
	if !ok || err != nil || n < min {
		return fmt.Sprintf("The %s field must be at least %s.", humanize(field), params[0])
	}
	return ""
}

func ruleMax(field string, value any, params []string, _ map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	n, ok := sizeOf(value)
	max, err := strconv.ParseFloat(params[0], 64)
	if !ok || err != nil || n > max {
		return fmt.Sprintf("The %s field may not be greater than %s.", humanize(field), params[0])
	}
	return ""
}

func ruleSize(field string, value any, params []string, _ map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	n, ok := sizeOf(value)
	want, err := strconv.ParseFloat(params[0], 64)
	if !ok || err != nil || n != want {
		return fmt.Sprintf("The %s field must be %s.", humanize(field), params[0])
	}
	return ""
}

func ruleIn(field string, value any, params []string, _ map[string]any) string {
	s := fmt.Sprintf("%v", value)
	for _, allowed := range params {
		if s == allowed {
			return ""
		}
	}
	return fmt.Sprintf("The selected %s is invalid.", humanize(field))
}

var (
	alphaPattern    = regexp.MustCompile(`^[a-zA-Z]+$`)
	alphaNumPattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
)

func ruleAlpha(field string, value any, _ []string, _ map[string]any) string {
	s, ok := value.(string)
	if !ok || !alphaPattern.MatchString(s) {
		return fmt.Sprintf("The %s field must only contain letters.", humanize(field))
	}
	return ""
}

func ruleAlphaNum(field string, value any, _ []string, _ map[string]any) string {
	s, ok := value.(string)
	if !ok || !alphaNumPattern.MatchString(s) {
		return fmt.Sprintf("The %s field must only contain letters and numbers.", humanize(field))
	}
	return ""
}

func ruleConfirmed(field string, value any, _ []string, data map[string]any) string {
	confirmation, ok := lookup(data, field+"_confirmation")
	if !ok || fmt.Sprintf("%v", confirmation) != fmt.Sprintf("%v", value) {
		return fmt.Sprintf("The %s field confirmation does not match.", humanize(field))
	}
	return ""
}

func ruleSame(field string, value any, params []string, data map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	other, ok := lookup(data, params[0])
	if !ok || fmt.Sprintf("%v", other) != fmt.Sprintf("%v", value) {
		return fmt.Sprintf("The %s field must match %s.", humanize(field), humanize(params[0]))
	}
	return ""
}

func ruleDifferent(field string, value any, params []string, data map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	other, ok := lookup(data, params[0])
	if ok && fmt.Sprintf("%v", other) == fmt.Sprintf("%v", value) {
		return fmt.Sprintf("The %s field must be different from %s.", humanize(field), humanize(params[0]))
	}
	return ""
}
