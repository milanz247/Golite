package models

// User is Golite's example Eloquent-style model, analogous to Laravel's
// App\Models\User. Embedding Model gives it ID/CreatedAt/UpdatedAt/
// DeletedAt for free (see Model.go); Posts demonstrates a HasMany
// association the same way Laravel's `public function posts()` would,
// just declared structurally via a struct tag instead of a method.
type User struct {
	Model

	Name     string `gorm:"size:255;not null" json:"name"`
	Email    string `gorm:"size:255;not null;uniqueIndex" json:"email"`
	Password string `gorm:"size:255;not null" json:"-"`

	// Posts is the inverse side of Post.User's BelongsTo — GORM infers
	// the "user_id" foreign key from the User type name by convention,
	// matched explicitly here for clarity. Loaded on demand via
	// db.Preload("Posts").Find(&users), never eagerly by default.
	Posts []Post `gorm:"foreignKey:UserID" json:"posts,omitempty"`
}

// TableName pins the table name to "users" explicitly rather than relying
// on GORM's pluralization, so it stays stable and predictable regardless
// of GORM's naming-strategy defaults — the migration in
// database/migrations/2026_07_13_000001_create_users_table.go creates
// exactly this table.
func (User) TableName() string {
	return "users"
}
