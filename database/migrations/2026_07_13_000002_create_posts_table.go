package migrations

import (
	"gorm.io/gorm"

	"Golite/app/Models"
)

func init() {
	Register(&CreatePostsTable{})
}

// CreatePostsTable creates the "posts" table, whose user_id foreign key
// (see models.Post) depends on "users" already existing — Name's
// "..._000002_..." timestamp sorts after CreateUsersTable's
// "..._000001_...", which is what guarantees Runner.Migrate applies them
// in the right order.
type CreatePostsTable struct{}

// Name is this migration's tracking key — see Migration's doc comment.
func (CreatePostsTable) Name() string {
	return "2026_07_13_000002_create_posts_table"
}

// Up creates the table.
func (CreatePostsTable) Up(db *gorm.DB) error {
	return db.Migrator().CreateTable(&models.Post{})
}

// Down drops it.
func (CreatePostsTable) Down(db *gorm.DB) error {
	return db.Migrator().DropTable(&models.Post{})
}
