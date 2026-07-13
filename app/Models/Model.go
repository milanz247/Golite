// Package models holds Golite's Eloquent-style GORM models — Golite's
// equivalent of app/Models in a Laravel application.
package models

import (
	"time"

	"gorm.io/gorm"
)

// Model is the base every model embeds, matching GORM's own conventional
// column set: an auto-incrementing primary key, timestamps GORM maintains
// automatically, and a soft-delete column. Embedding this (rather than
// requiring every model to redeclare it) is what makes a model
// automatically soft-deletable and timestamped just by having a Model
// field, the same way every Laravel model implicitly gets id/
// created_at/updated_at/deleted_at from the base Eloquent class.
type Model struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
