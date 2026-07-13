package models

// User is Golite's user model — the record the top-level auth package's
// Guard builds authentication around (see
// app/Http/Controllers/AuthController.go and docs/authentication.md).
// Embedding Model gives it ID/CreatedAt/UpdatedAt/DeletedAt for free (see
// Model.go).
type User struct {
	Model

	Name     string `gorm:"size:255;not null" json:"name"`
	Email    string `gorm:"size:255;not null;uniqueIndex" json:"email"`
	Password string `gorm:"size:255;not null" json:"-"`

	// RememberToken holds the SHA-256 hash of the current "remember me"
	// token — empty if none has ever been issued, or the most recent one
	// was cleared by logout. Never the raw token itself, the same
	// reasoning Password holds a bcrypt hash rather than the plaintext.
	// See auth.Guard.IssueRememberToken.
	RememberToken string `gorm:"size:255" json:"-"`
}

// TableName pins the table name to "users" explicitly rather than relying
// on GORM's pluralization, so it stays stable and predictable regardless
// of GORM's naming-strategy defaults — the migration in
// database/migrations/2026_07_13_000001_create_users_table.go creates
// exactly this table.
func (User) TableName() string {
	return "users"
}
