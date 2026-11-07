// Package controller handles the notifications module's HTTP surface.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/constants"
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/dto"
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/service"
	"github.com/IncepTools/go/src/web/binding"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// OrderController serves the order endpoint.
type OrderController struct {
	svc *service.OrderService
}

// New builds the controller.
func New(svc *service.OrderService) *OrderController { return &OrderController{svc: svc} }

// Routes declares the endpoints.
func (c *OrderController) Routes() []router.Route {
	return []router.Route{
		router.Post("/orders", c.place).
			AsPublic().
			Summary("Place an order and publish the event").Tag("events").
			Accepts(dto.PlaceOrder{}).
			Returns(dto.OrderReceipt{}, fiber.StatusCreated),
	}
}

func (c *OrderController) place(ctx fiber.Ctx) error {
	req, err := binding.Body[dto.PlaceOrder](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	// ctx.Context(), not context.Background(): the handler's context carries
	// the trace id and the deadline, so the sync subscriber's logs join this
	// request and a cancelled request stops the work it started.
	receipt, err := c.svc.Place(ctx.Context(), req)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.Created(ctx, constants.MsgPlaced, receipt)
}
