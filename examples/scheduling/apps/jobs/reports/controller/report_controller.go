// Package controller handles the reports module's HTTP surface.
//
// A scheduled job with no way to ask how it is doing is a job that fails
// unnoticed. These routes are that surface; the schedule itself is declared
// in the module.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/constants"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/dto"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/service"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// ReportController serves the job status endpoints.
type ReportController struct {
	svc *service.ReportService
}

// New builds the controller.
func New(svc *service.ReportService) *ReportController { return &ReportController{svc: svc} }

// Routes declares the endpoints.
func (c *ReportController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.list).
			AsPublic().
			Summary("Last outcome of every scheduled job").Tag("scheduling").
			Returns([]dto.RunStatus{}),

		router.Get("/:job", c.get).
			AsPublic().
			Summary("Last outcome of one job").Tag("scheduling").
			Param("job", "Job name, e.g. reports.daily").
			Returns(dto.RunStatus{}),
	}
}

func (c *ReportController) list(ctx fiber.Ctx) error {
	return response.OK(ctx, constants.MsgStatus, c.svc.All(ctx.Context()))
}

func (c *ReportController) get(ctx fiber.Ctx) error {
	job, err := pipe.StringParam(ctx, "job")
	if err != nil {
		return response.Fail(ctx, err)
	}
	st, err := c.svc.Status(ctx.Context(), job)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgStatus, st)
}
