// Package controller handles the hello module's HTTP surface.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/constants"
	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/dto"
	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/service"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// GreeterController serves the greeting endpoint.
type GreeterController struct {
	svc *service.GreeterService
}

// New builds the controller.
func New(svc *service.GreeterService) *GreeterController { return &GreeterController{svc: svc} }

// Routes declares the endpoints.
//
// A route is a VALUE, not a side effect of calling a register function. That
// is what puts it in the route table and what lets a test assert over the
// surface without starting a server.
func (c *GreeterController) Routes() []router.Route {
	return []router.Route{
		router.Get("/:name", c.greet).
			AsPublic().
			Summary("Greet someone").Tag("greeter").
			Param("name", "Who to greet").
			Returns(dto.Greeting{}),
	}
}

func (c *GreeterController) greet(ctx fiber.Ctx) error {
	name, err := pipe.StringParam(ctx, "name")
	if err != nil {
		return response.Fail(ctx, err)
	}
	greeting, err := c.svc.Greet(ctx.Context(), name)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgGreeted, greeting)
}
