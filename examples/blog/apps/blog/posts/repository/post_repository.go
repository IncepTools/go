// Package repository is the posts module's data access.
package repository

import (
	"gorm.io/gorm"

	"github.com/IncepTools/go/examples/blog/apps/blog/posts/constants"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/entity"
	"github.com/IncepTools/go/src/data/repo"
)

// PostRepository is the data access for posts.
//
// It embeds the generic repository, which applies the account filter to
// every query and refuses one with no account on the context. Add methods
// here for anything the generic surface does not cover; do not reach past
// it to the *gorm.DB from a service.
type PostRepository struct {
	*repo.Repository[entity.Post]
}

// New builds the repository.
func New(db *gorm.DB) *PostRepository {
	return &PostRepository{
		Repository: repo.New[entity.Post](db,
			repo.WithEntityName(constants.EntityName),
			repo.Tenanted(),
			repo.WithSortable(constants.Sortable...),
		),
	}
}
