// Package validation is Golite's equivalent of Illuminate\Validation: rule
// strings ("required|email|min:3") checked against a plain
// map[string]any payload, producing field-keyed error messages — designed
// to run directly against Context.All()'s unified input payload (see
// Context.Validate in app/Http/Context.go).
package validation

import "sort"

// Validator checks data against a set of "field": "rule1|rule2:param"
// specs, mirroring Laravel's Illuminate\Validation\Validator. Build one
// with Make; it lazily runs its rules on first use (Fails/Passes/Errors/
// Validated all trigger — and share — that single run).
type Validator struct {
	data   map[string]any
	fields []string
	rules  map[string][]string
	errors Errors
	ran    bool
}

// Make builds a Validator for data against rules, where each rules value
// is a "|"-separated list of rule specs (e.g. "required|string|max:255"),
// mirroring Validator::make($data, $rules).
func Make(data map[string]any, rules map[string]string) *Validator {
	v := &Validator{
		data:   data,
		rules:  make(map[string][]string, len(rules)),
		errors: Errors{},
	}
	for field, spec := range rules {
		v.fields = append(v.fields, field)
		v.rules[field] = splitRules(spec)
	}
	sort.Strings(v.fields) // deterministic Errors()/Validated() iteration
	return v
}

func splitRules(spec string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(spec); i++ {
		if i == len(spec) || spec[i] == '|' {
			if i > start {
				out = append(out, spec[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// run executes every declared rule exactly once. Laravel semantics: a rule
// other than the "required" family simply doesn't fire against a field
// that's absent or empty — only "required" itself reports that as an
// error — so a field with no value and no "required" rule passes silently
// (this is what lets "email|nullable"-style optional fields work without
// every rule needing its own presence check).
func (v *Validator) run() {
	for _, field := range v.fields {
		value, present := lookup(v.data, field)
		empty := !present || isEmptyValue(value)

		for _, spec := range v.rules[field] {
			name, params := parseRuleSpec(spec)
			if empty && name != "required" {
				continue
			}
			fn, ok := lookupRule(name)
			if !ok {
				panic("golite/validation: no rule registered under \"" + name + "\" (register it with validation.Extend)")
			}
			if msg := fn(field, value, params, v.data); msg != "" {
				v.errors.Add(field, msg)
			}
		}
	}
}

func (v *Validator) ensureRan() {
	if !v.ran {
		v.run()
		v.ran = true
	}
}

// Fails reports whether any field failed validation.
func (v *Validator) Fails() bool {
	v.ensureRan()
	return len(v.errors) > 0
}

// Passes reports whether every field passed validation.
func (v *Validator) Passes() bool {
	return !v.Fails()
}

// Errors returns every field's failure messages, keyed by field name.
func (v *Validator) Errors() Errors {
	v.ensureRan()
	return v.errors
}

// Validated returns the subset of data covering only the fields that were
// declared in the rule set, mirroring $validator->validated(). If
// validation failed, it returns a *validation.Exception carrying Errors()
// instead — the same exception Context.Validate panics with, and that
// app/Exceptions.Render renders as a 422 response.
func (v *Validator) Validated() (map[string]any, error) {
	if v.Fails() {
		return nil, &Exception{Errors: v.errors}
	}
	out := make(map[string]any, len(v.fields))
	for _, field := range v.fields {
		if value, ok := lookup(v.data, field); ok {
			out[field] = value
		}
	}
	return out, nil
}
