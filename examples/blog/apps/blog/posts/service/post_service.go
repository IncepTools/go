// Package service holds the posts module's business rules.
//
// It imports nothing from web/. It takes a context and plain arguments and
// returns a value or a typed error, so the same logic serves an HTTP
// request, a scheduled job and a test without any of them pretending to be
// the others.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/IncepTools/go/examples/blog/apps/blog/posts/constants"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/dto"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/entity"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/repository"
	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/data/gormx"
	"github.com/IncepTools/go/src/data/repo"
	"github.com/IncepTools/go/src/reliability/outbox"
	"github.com/IncepTools/go/src/web/pagination"
)

// PostService holds the rules for posts.
type PostService struct {
	repo *repository.PostRepository
	db   *gorm.DB
}

// New builds the service.
func New(r *repository.PostRepository, db *gorm.DB) *PostService {
	return &PostService{repo: r, db: db}
}

// List returns a page of the caller's posts.
func (s *PostService) List(ctx context.Context, q pagination.Query, publishedOnly bool) (repo.Page[entity.Post], error) {
	scopes := []gormx.Scope{}
	if publishedOnly {
		scopes = append(scopes, gormx.Where("published_at IS NOT NULL"))
	}
	if q.Search != "" {
		scopes = append(scopes, gormx.Where("title LIKE ?", "%"+q.Search+"%"))
	}
	return s.repo.List(ctx, q, scopes...)
}

// Get returns one post, or NotFound when it does not exist or is not the
// caller's.
func (s *PostService) Get(ctx context.Context, id uuid.UUID) (*entity.Post, error) {
	return s.repo.FindByID(ctx, id)
}

// Create stores a new post.
func (s *PostService) Create(ctx context.Context, req dto.CreatePost) (*entity.Post, error) {
	tenant := ctxkit.TenantOf(ctx)
	if tenant.Zero() {
		return nil, errs.Unauthorized("no account context").WithCode("NO_TENANT")
	}

	// Checked here for a good message AND enforced by a unique index in the
	// migration. The check alone is a race: two concurrent creates both see
	// nothing and both insert. The index alone gives the user a constraint
	// violation instead of a sentence. You need both.
	taken, err := s.repo.Exists(ctx, gormx.Where("slug = ?", req.Slug))
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, errs.Conflict("a post with the slug %q already exists", req.Slug).
			WithCode("SLUG_TAKEN")
	}

	now := time.Now().UTC()
	post := &entity.Post{
		ID:        uuid.New(),
		AccountID: tenant.AccountID,
		Title:     req.Title,
		Slug:      req.Slug,
		Body:      req.Body,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, post); err != nil {
		return nil, err
	}
	return post, nil
}

// Update applies the fields the client actually sent.
func (s *PostService) Update(ctx context.Context, id uuid.UUID, req dto.UpdatePost) (*entity.Post, error) {
	// A map, not a struct: GORM skips zero-valued struct fields, so a
	// struct update could never clear the body.
	fields := map[string]any{"updated_at": time.Now().UTC()}
	if req.Title != nil {
		fields["title"] = *req.Title
	}
	if req.Body != nil {
		fields["body"] = *req.Body
	}
	if len(fields) == 1 {
		return nil, errs.Invalid("nothing to update").WithCode("EMPTY_UPDATE")
	}
	if err := s.repo.UpdateByID(ctx, id, fields); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id)
}

// Publish marks a post published and records the event for delivery.
//
// The state change and the message are ONE transaction. Publishing after
// the commit means a crash in between leaves a published post nothing
// downstream knows about; publishing before means an event for a post that
// does not exist. There is no ordering of two systems that fixes that.
func (s *PostService) Publish(ctx context.Context, id uuid.UUID) (*entity.Post, error) {
	var out *entity.Post

	err := repo.Transaction(ctx, s.db, func(ctx context.Context) error {
		// FOR UPDATE, so two concurrent publishes cannot both see it
		// unpublished and both emit an event.
		post, err := s.repo.LockByID(ctx, id)
		if err != nil {
			return err
		}
		if post.Published() {
			return errs.Conflict("this post is already published").
				WithCode("ALREADY_PUBLISHED")
		}

		now := time.Now().UTC()
		if err := s.repo.UpdateByID(ctx, id, map[string]any{
			"published_at": now,
			"updated_at":   now,
		}); err != nil {
			return err
		}
		post.PublishedAt = &now
		out = post

		return outbox.Enqueue(ctx, s.db, constants.TopicPublished, dto.PostPublished{
			PostID:    post.ID.String(),
			AccountID: post.AccountID.String(),
			Slug:      post.Slug,
			Title:     post.Title,
			Published: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes a post.
func (s *PostService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteByID(ctx, id)
}

// ToResponse maps the entity to the public shape.
func ToResponse(p *entity.Post) dto.PostResponse {
	return dto.PostResponse{
		ID:          p.ID.String(),
		Title:       p.Title,
		Slug:        p.Slug,
		Body:        p.Body,
		Published:   p.Published(),
		PublishedAt: p.PublishedAt,
		CreatedAt:   p.CreatedAt,
	}
}

// ToResponses maps a page.
func ToResponses(items []entity.Post) []dto.PostResponse {
	out := make([]dto.PostResponse, 0, len(items))
	for i := range items {
		out = append(out, ToResponse(&items[i]))
	}
	return out
}
