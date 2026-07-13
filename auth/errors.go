package auth

import "errors"

var (
	// ErrEmailTaken is returned by Guard.Register when the email is
	// already registered — detected from the database's own unique-index
	// violation, not a preceding SELECT, which would leave a
	// check-then-insert race between two concurrent registrations for
	// the same email.
	ErrEmailTaken = errors.New("auth: that email is already registered")

	// ErrInvalidCredentials is returned by Guard.Attempt for either a
	// nonexistent email or a wrong password — deliberately the same
	// error for both, so a caller can't distinguish "no such account"
	// from "wrong password" through error inspection.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")

	// ErrInvalidRememberToken is returned by Guard.UserByRememberToken
	// when the token doesn't match, the user has none currently issued
	// (never, or cleared by logout), or the cookie was malformed.
	ErrInvalidRememberToken = errors.New("auth: invalid or expired remember-me token")

	// ErrInvalidResetToken is returned by Guard.ResetPassword for a
	// wrong, already-used, or expired token.
	ErrInvalidResetToken = errors.New("auth: invalid or expired password reset token")

	// ErrUserNotFound is returned by Guard.CreatePasswordResetToken when
	// no user has the given email.
	ErrUserNotFound = errors.New("auth: no user with that email")
)
