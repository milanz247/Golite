package auth

import (
	"errors"
	"time"

	"gorm.io/gorm"

	models "Golite/app/Models"
)

// passwordResetTokenTTL is how long a password reset token stays valid —
// Laravel's own default (config('auth.passwords.users.expire'), 60
// minutes).
const passwordResetTokenTTL = time.Hour

// PasswordResetToken is the "password_reset_tokens" table's schema —
// Golite's equivalent of Laravel's own table of the same name. It lives
// in the auth package (not app/Models) because, unlike User, application
// code never queries it directly — it's purely Guard's own bookkeeping
// for CreatePasswordResetToken/ResetPassword.
type PasswordResetToken struct {
	Email     string    `gorm:"primaryKey;size:255"`
	TokenHash string    `gorm:"size:255;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

// TableName pins the table name — see models.User.TableName's doc
// comment for why.
func (PasswordResetToken) TableName() string {
	return "password_reset_tokens"
}

// CreatePasswordResetToken generates a token for email (replacing any
// previously issued one), stores its hash, and returns the raw value.
//
// There is no mail system in Golite yet, so this can't actually email
// the token the way Laravel's Password::sendResetLink does — the caller
// (AuthController.ForgotPassword) returns it directly in the JSON
// response instead. That's fine for local development, but it means
// this endpoint necessarily reveals whether an email is registered
// (ErrUserNotFound vs. a token) — a real deployment needs to wire up
// actual email delivery, and change the controller to return an
// identical response either way, before this is safe to expose
// publicly.
func (g *Guard) CreatePasswordResetToken(email string) (string, error) {
	var user models.User
	if err := g.db.Where("email = ?", email).First(&user).Error; err != nil {
		return "", ErrUserNotFound
	}

	raw, hash, err := newRandomToken()
	if err != nil {
		return "", err
	}

	var existing PasswordResetToken
	err = g.db.Where("email = ?", email).First(&existing).Error
	switch {
	case err == nil:
		existing.TokenHash = hash
		existing.CreatedAt = time.Now()
		if err := g.db.Save(&existing).Error; err != nil {
			return "", err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		record := PasswordResetToken{Email: email, TokenHash: hash, CreatedAt: time.Now()}
		if err := g.db.Create(&record).Error; err != nil {
			return "", err
		}
	default:
		return "", err
	}

	return raw, nil
}

// ResetPassword verifies token for email — it must match the stored hash
// and be within passwordResetTokenTTL — then updates the user's password
// (bcrypt-hashed) and deletes the token so it can't be reused.
func (g *Guard) ResetPassword(email, token, newPassword string) error {
	var record PasswordResetToken
	if err := g.db.Where("email = ?", email).First(&record).Error; err != nil {
		return ErrInvalidResetToken
	}
	if time.Since(record.CreatedAt) > passwordResetTokenTTL {
		g.db.Delete(&record)
		return ErrInvalidResetToken
	}
	if !tokenHashEquals(token, record.TokenHash) {
		return ErrInvalidResetToken
	}

	var user models.User
	if err := g.db.Where("email = ?", email).First(&user).Error; err != nil {
		return ErrInvalidResetToken
	}
	user.Password = g.hasher.Make(newPassword)
	if err := g.db.Save(&user).Error; err != nil {
		return err
	}

	g.db.Delete(&record)
	return nil
}
