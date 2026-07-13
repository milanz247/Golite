package models

// Post is Golite's example related model: a BelongsTo User, the other
// half of User.Posts' HasMany — analogous to Laravel's `Post belongsTo
// User` / `User hasMany Post` pair.
type Post struct {
	Model

	Title string `gorm:"size:255;not null" json:"title"`
	Body  string `gorm:"type:text" json:"body"`

	// UserID is the foreign key; User is the loaded association (via
	// db.Preload("User").Find(&posts)), matching GORM's BelongsTo
	// convention of a "<Field>ID" column paired with a same-named struct
	// field.
	UserID uint `gorm:"not null;index" json:"user_id"`
	User   User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName pins the table name to "posts" — see User.TableName's doc
// comment for why.
func (Post) TableName() string {
	return "posts"
}
