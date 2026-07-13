package migrations

import (
	"gorm.io/gorm"

	"Golite/auth"
)

func init() {
	Register(&CreatePasswordResetTokensTable{})
}

// CreatePasswordResetTokensTable creates the "password_reset_tokens"
// table from auth.PasswordResetToken's own GORM schema tags — Golite's
// equivalent of Laravel's 0001_01_01_000000_create_password_reset_tokens_table.php.
type CreatePasswordResetTokensTable struct{}

// Name is this migration's tracking key — see Migration's doc comment.
func (CreatePasswordResetTokensTable) Name() string {
	return "2026_07_13_000002_create_password_reset_tokens_table"
}

// Up creates the table.
func (CreatePasswordResetTokensTable) Up(db *gorm.DB) error {
	return db.Migrator().CreateTable(&auth.PasswordResetToken{})
}

// Down drops it.
func (CreatePasswordResetTokensTable) Down(db *gorm.DB) error {
	return db.Migrator().DropTable(&auth.PasswordResetToken{})
}
