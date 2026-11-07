// Package controller handles the posts module's HTTP surface.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/blog/apps/blog/posts/constants"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/dto"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/service"
	"github.com/IncepTools/go/src/web/binding"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/pagination"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// PostController serves the posts resource.
type PostController struct {
	svc *service.PostService
}

// New builds the controller.
func New(svc *service.PostService) *PostController { return &PostController{svc: svc} }

// Routes declares the endpoints.
func (c *PostController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.list).
			Summary("List posts").Tag("blog").
			QueryParam("page", "Page number").
			QueryParam("limit", "Items per page").
			QueryParam("sort", "created_at, title or published_at").
			QueryParam("search", "Match against the title").
			QueryParam("published", "Only published posts").
			Returns([]dto.PostResponse{}),

		router.Post("/", c.create).
			Guard(guard.Tenanted).
			Summary("Create a post").Tag("blog").
			Accepts(dto.CreatePost{}).
			Returns(dto.PostResponse{}, fiber.StatusCreated),

		router.Get("/:id", c.get).
			Summary("Get a post").Tag("blog").
			Param("id", "Post id").
			Returns(dto.PostResponse{}),

		router.Patch("/:id", c.update).
			Guard(guard.Tenanted).
			Summary("Update a post").Tag("blog").
			Param("id", "Post id").
			Accepts(dto.UpdatePost{}),

		router.Post("/:id/publish", c.publish).
			Guard(guard.Tenanted).
			Summary("Publish a post").Tag("blog").
			Describe("Marks the post published and records the event for delivery, in one transaction.").
			Param("id", "Post id"),

		router.Delete("/:id", c.remove).
			Guard(guard.Tenanted).
			Summary("Delete a post").Tag("blog").
			Param("id", "Post id"),
	}
}

func (c *PostController) list(ctx fiber.Ctx) error {
	q, err := pagination.FromWith(ctx, pagination.Options{
		Sortable: constants.Sortable, DefaultSort: "created_at", DefaultOrder: pagination.Desc,
	})
	if err != nil {
		return response.Fail(ctx, err)
	}
	publishedOnly, err := pipe.BoolQuery(ctx, "published", false)
	if err != nil {
		return response.Fail(ctx, err)
	}

	page, err := c.svc.List(ctx.Context(), q, publishedOnly)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.List(ctx, constants.MsgListed, service.ToResponses(page.Items), q.Page, q.Limit, page.Total)
}

func (c *PostController) get(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	p, err := c.svc.Get(ctx.Context(), id)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgFetched, service.ToResponse(p))
}

func (c *PostController) create(ctx fiber.Ctx) error {
	req, err := binding.Body[dto.CreatePost](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	p, err := c.svc.Create(ctx.Context(), req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.Created(ctx, constants.MsgCreated, service.ToResponse(p))
}

func (c *PostController) update(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	req, err := binding.Body[dto.UpdatePost](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	p, err := c.svc.Update(ctx.Context(), id, req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgUpdated, service.ToResponse(p))
}

func (c *PostController) publish(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	p, err := c.svc.Publish(ctx.Context(), id)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgPublished, service.ToResponse(p))
}

func (c *PostController) remove(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	if err := c.svc.Delete(ctx.Context(), id); err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgDeleted, nil)
}
