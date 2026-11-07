// Package controller handles the jobs module's HTTP surface.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/constants"
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/dto"
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/service"
	"github.com/IncepTools/go/src/web/binding"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// JobController serves the submission endpoint.
type JobController struct {
	svc *service.JobService
}

// New builds the controller.
func New(svc *service.JobService) *JobController { return &JobController{svc: svc} }

// Routes declares the endpoints.
func (c *JobController) Routes() []router.Route {
	return []router.Route{
		router.Post("/", c.enqueue).
			AsPublic().
			Summary("Submit a background job").Tag("queues").
			Accepts(dto.SubmitJob{}).
			Returns(dto.JobReceipt{}, fiber.StatusAccepted),
	}
}

func (c *JobController) enqueue(ctx fiber.Ctx) error {
	req, err := binding.Body[dto.SubmitJob](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	receipt, err := c.svc.Enqueue(ctx.Context(), req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	// 202, not 200 or 201: nothing has been done yet and no resource exists
	// at a URL. The client is being told the work was accepted, not finished.
	return response.Accepted(ctx, constants.MsgEnqueued, receipt)
}
