package validation

// Errors maps a field name to every failed rule's message for that field —
// Golite's equivalent of Laravel's MessageBag as returned by
// Validator::errors().
type Errors map[string][]string

// Add appends message to field's error list.
func (e Errors) Add(field, message string) {
	e[field] = append(e[field], message)
}

// Has reports whether field has at least one error.
func (e Errors) Has(field string) bool {
	return len(e[field]) > 0
}

// First returns field's first error message, or "" if it has none.
func (e Errors) First(field string) string {
	if len(e[field]) == 0 {
		return ""
	}
	return e[field][0]
}

// All flattens every field's messages into a single slice, in field-name
// order, mirroring MessageBag::all().
func (e Errors) All() []string {
	fields := make([]string, 0, len(e))
	for field := range e {
		fields = append(fields, field)
	}
	sortStrings(fields)

	var all []string
	for _, field := range fields {
		all = append(all, e[field]...)
	}
	return all
}

// Exception is what Validator.Validated returns (as an error) when
// validation fails, and what Context.Validate panics with — Golite's
// equivalent of Laravel's ValidationException, which
// app/Exceptions.Render recognizes and renders as a 422 response carrying
// Errors.
type Exception struct {
	Errors Errors
}

// Error implements the error interface. Field-level detail lives in
// Errors, not the message itself, mirroring
// ValidationException::getMessage()'s generic "The given data was
// invalid."
func (e *Exception) Error() string {
	return "the given data was invalid"
}
