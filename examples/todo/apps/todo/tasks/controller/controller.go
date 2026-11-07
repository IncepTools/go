// Package controller handles the tasks module's HTTP surface.
//
// A controller reads the request, calls the service, and renders the result.
// It holds no business rules: if a condition would also belong in a
// scheduled job, it belongs in the service.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/constants"
	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/dto"
	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/service"
	"github.com/IncepTools/go/web/binding"
	"github.com/IncepTools/go/web/guard"
	"github.com/IncepTools/go/web/pagination"
	"github.com/IncepTools/go/web/pipe"
	"github.com/IncepTools/go/web/response"
	"github.com/IncepTools/go/web/router"
)

// TaskController serves the tasks resource.
type TaskController struct {
	svc *service.TaskService
}

// New builds the controller.
func New(svc *service.TaskService) *TaskController { return &TaskController{svc: svc} }

// Routes declares the endpoints.
//
// Everything about a route is on one line: its guard, its documentation and
// its handler. A reviewer can see what protects each one without opening a
// single handler.
func (c *TaskController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.list).
			Summary("List tasks").Tag("todo").
			QueryParam("page", "Page number").
			QueryParam("limit", "Items per page").
			QueryParam("sort", "created_at, title or due_at").
			QueryParam("done", "Include completed tasks").
			Returns([]dto.TaskResponse{}),

		router.Post("/", c.create).
			Guard(guard.Tenanted).
			Summary("Create a task").Tag("todo").
			Accepts(dto.CreateTask{}).
			Returns(dto.TaskResponse{}, fiber.StatusCreated),

		router.Get("/:id", c.get).
			Summary("Get a task").Tag("todo").
			Param("id", "Task id").
			Returns(dto.TaskResponse{}),

		router.Patch("/:id", c.update).
			Guard(guard.Tenanted).
			Summary("Update a task").Tag("todo").
			Param("id", "Task id").
			Accepts(dto.UpdateTask{}),

		router.Post("/:id/complete", c.complete).
			Guard(guard.Tenanted).
			Summary("Mark a task done").Tag("todo").
			Param("id", "Task id"),

		router.Delete("/:id", c.remove).
			Guard(guard.Tenanted).
			Summary("Delete a task").Tag("todo").
			Param("id", "Task id"),
	}
}

func (c *TaskController) list(ctx fiber.Ctx) error {
	q, err := pagination.FromWith(ctx, pagination.Options{
		Sortable: constants.Sortable, DefaultSort: "created_at", DefaultOrder: pagination.Desc,
	})
	if err != nil {
		return response.Fail(ctx, err)
	}
	includeDone, err := pipe.BoolQuery(ctx, "done", false)
	if err != nil {
		return response.Fail(ctx, err)
	}

	items, total, err := c.svc.List(ctx.Context(), q, includeDone)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.List(ctx, constants.MsgListed, service.ToResponses(items), q.Page, q.Limit, total)
}

func (c *TaskController) get(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	t, err := c.svc.Get(ctx.Context(), id)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgFetched, service.ToResponse(t))
}

func (c *TaskController) create(ctx fiber.Ctx) error {
	req, err := binding.Body[dto.CreateTask](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	t, err := c.svc.Create(ctx.Context(), req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.Created(ctx, constants.MsgCreated, service.ToResponse(t))
}

func (c *TaskController) update(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	req, err := binding.Body[dto.UpdateTask](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	t, err := c.svc.Update(ctx.Context(), id, req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgUpdated, service.ToResponse(t))
}

func (c *TaskController) complete(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	t, err := c.svc.Complete(ctx.Context(), id)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgCompleted, service.ToResponse(t))
}

func (c *TaskController) remove(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	if err := c.svc.Delete(ctx.Context(), id); err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgDeleted, nil)
}
