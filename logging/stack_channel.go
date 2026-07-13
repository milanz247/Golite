package logging

// StackChannel fans one entry out to multiple channels — Laravel's "stack"
// log driver (e.g. channels: ["single", "slack"]).
type StackChannel struct {
	channels []Channel
}

// NewStackChannel builds a StackChannel that writes every entry to each of
// channels, in order.
func NewStackChannel(channels ...Channel) *StackChannel {
	return &StackChannel{channels: channels}
}

// Write writes entry to every underlying channel, continuing past
// individual failures and returning the first error encountered (if any)
// once all have been attempted.
func (s *StackChannel) Write(entry Entry) error {
	var firstErr error
	for _, ch := range s.channels {
		if err := ch.Write(entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
