package migrations

import (
	"gorm.io/gorm"

	"Golite/app/Models"
)

func init() {
	Register(&CreateUsersTable{})
}

// CreateUsersTable creates the "users" table from models.User's own GORM
// schema tags — Golite's equivalent of Laravel's
// 2014_10_12_000000_create_users_table.php.
type CreateUsersTable struct{}

// Name is this migration's tracking key — see Migration's doc comment.
func (CreateUsersTable) Name() string {
	return "2026_07_13_000001_create_users_table"
}

// Up creates the table.
func (CreateUsersTable) Up(db *gorm.DB) error {
	return db.Migrator().CreateTable(&models.User{})
}

// Down drops it.
func (CreateUsersTable) Down(db *gorm.DB) error {
	return db.Migrator().DropTable(&models.User{})
}
