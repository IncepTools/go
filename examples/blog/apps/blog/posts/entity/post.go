// Package entity holds the posts module's stored shapes.
package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Post is the stored shape of a blog post.
//
// It must stay in step with the migration. A field whose column does not
// exist compiles, runs, and fails at query time with an error that reads
// like a driver bug.
type Post struct {
	ID uuid.UUID `gorm:"type:text;primaryKey" json:"id"`

	// AccountID is the ownership boundary. Every query against this table
	// is filtered by it — by the repository, not by each caller.
	AccountID uuid.UUID `gorm:"type:text;not null;index" json:"account_id"`

	Title string `gorm:"type:text;not null" json:"title"`
	Slug  string `gorm:"type:text;not null" json:"slug"`
	Body  string `gorm:"type:text;not null;default:''" json:"body"`

	PublishedAt *time.Time `json:"published_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName pins the table explicitly rather than relying on GORM's
// pluraliser, which disagrees with hand-written migrations often enough to
// matter.
func (Post) TableName() string { return "posts" }

// Published reports whether the post is visible to readers.
func (p Post) Published() bool { return p.PublishedAt != nil }
