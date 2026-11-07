// Package dto holds the posts module's request and response shapes.
package dto

import (
	"strings"
	"time"
)

// CreatePost is the create request.
type CreatePost struct {
	Title string `json:"title" validate:"required,min=3,max=160"`
	Slug  string `json:"slug" validate:"omitempty,min=3,max=160,slug"`
	Body  string `json:"body" validate:"omitempty,max=50000"`
}

// Normalize runs before validation, so the rules see the value that will
// actually be stored.
func (c *CreatePost) Normalize() {
	c.Title = strings.TrimSpace(c.Title)
	c.Slug = strings.ToLower(strings.TrimSpace(c.Slug))
}

// SetDefaults runs before validation, so a derived slug can satisfy the
// slug rule rather than forcing the client to send one.
func (c *CreatePost) SetDefaults() {
	if c.Slug == "" {
		c.Slug = Slugify(c.Title)
	}
}

// UpdatePost is the update request.
//
// Every field is a POINTER, so "the client did not send this" and "the
// client sent an empty value" are distinguishable. With a plain string a
// request that only renames would silently clear the body.
type UpdatePost struct {
	Title *string `json:"title" validate:"omitempty,min=3,max=160"`
	Body  *string `json:"body" validate:"omitempty,max=50000"`
}

// PostResponse is what the API returns.
//
// A separate type from the entity on purpose: returning the entity
// publishes every column you ever add to it and turns a schema change into
// an unannounced API change.
type PostResponse struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Body        string     `json:"body,omitempty"`
	Published   bool       `json:"published"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// PostPublished is the outbox payload. It is a deliberate, versioned shape:
// a consumer reads this, not the entity, so an internal column can change
// without breaking anything downstream.
type PostPublished struct {
	PostID    string    `json:"post_id"`
	AccountID string    `json:"account_id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Published time.Time `json:"published_at"`
}

// Slugify turns a title into a URL segment.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
