// Package dto holds the tasks module's request and response shapes.
package dto

import (
	"time"

	"github.com/IncepTools/go/src/core/errs"
)

// CreateTask is the create request.
type CreateTask struct {
	Title string `json:"title" validate:"required,min=3,max=120"`
	Notes string `json:"notes" validate:"omitempty,max=2000"`
	// Priority is optional and defaulted below, which is why the rule is
	// omitempty+oneof rather than required: a client that says nothing gets
	// "normal" rather than a 400.
	Priority string     `json:"priority" validate:"omitempty,oneof=low normal high"`
	DueAt    *time.Time `json:"due_at"`
	Tags     []string   `json:"tags" validate:"omitempty,max=10,dive,required,max=24"`
}

// Normalize implements binding.Normalizer: it runs before validation, so the
// rules see the value that will actually be stored.
func (c *CreateTask) Normalize() {
	c.Title = trimSpace(c.Title)
	for i := range c.Tags {
		c.Tags[i] = lower(trimSpace(c.Tags[i]))
	}
}

// SetDefaults implements binding.Defaulter: it runs before validation, so a
// default can satisfy a `required` rule.
func (c *CreateTask) SetDefaults() {
	if c.Priority == "" {
		c.Priority = "normal"
	}
}

// Validate implements binding.SelfValidator for the rule a tag cannot
// express. It runs after the tag rules, so the fields are already well-formed.
func (c *CreateTask) Validate() error {
	if c.DueAt != nil && c.DueAt.Before(time.Now()) {
		return errs.Invalid("due_at is in the past").
			WithDetails(errs.Detail{
				Field:   "due_at",
				Message: "due_at must be in the future",
				Rule:    "future",
			})
	}
	return nil
}

// UpdateTask is the update request.
//
// Every field is a POINTER. With a plain string, there is no way to tell "the
// client did not send this" from "the client sent an empty value" — so a
// request that only renames would silently clear the notes, and a request to
// set a flag to false would do nothing at all.
type UpdateTask struct {
	Title    *string    `json:"title" validate:"omitempty,min=3,max=120"`
	Notes    *string    `json:"notes" validate:"omitempty,max=2000"`
	Priority *string    `json:"priority" validate:"omitempty,oneof=low normal high"`
	DueAt    *time.Time `json:"due_at"`
}

// TaskResponse is what the API returns.
//
// It is a separate type from the stored shape on purpose. Returning the
// stored type publishes every field you ever add to it — including the
// internal ones — and turns a schema change into an unannounced API change.
type TaskResponse struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes,omitempty"`
	Priority    string     `json:"priority"`
	Tags        []string   `json:"tags,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func lower(s string) string {
	out := []byte(s)
	for i, b := range out {
		if b >= 'A' && b <= 'Z' {
			out[i] = b + 32
		}
	}
	return string(out)
}
