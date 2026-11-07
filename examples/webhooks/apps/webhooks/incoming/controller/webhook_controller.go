// Package controller handles the incoming module's HTTP surface.
//
// A controller reads the request, calls the service, and renders the result.
// It holds no business rules: verification and replay detection would also
// belong to a replay tool, so they live in the service.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/constants"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/dto"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/service"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// WebhookController serves the delivery endpoint.
type WebhookController struct {
	svc *service.WebhookService
}

// New builds the controller.
func New(svc *service.WebhookService) *WebhookController { return &WebhookController{svc: svc} }

// Routes declares the endpoints.
//
// The route is public because the signature IS the authentication. A sender
// has no session and no bearer token, so a guard here would lock out the
// only caller the endpoint has.
func (c *WebhookController) Routes() []router.Route {
	return []router.Route{
		router.Post("/", c.receive).
			AsPublic().
			Summary("Receive a signed webhook delivery").Tag("webhooks").
			Accepts(dto.Delivery{}).
			Returns(dto.Receipt{}, fiber.StatusAccepted),
	}
}

func (c *WebhookController) receive(ctx fiber.Ctx) error {
	// Copy the body: Fiber reuses the underlying buffer once the handler
	// returns, and the signature is computed over these exact bytes.
	body := append([]byte(nil), ctx.Body()...)

	receipt, err := c.svc.Accept(ctx.Context(), body, ctx.Get(constants.SignatureHeader))
	if err != nil {
		return response.Fail(ctx, err)
	}
	// 202, not 200: the delivery is recorded, and the subscribers run after
	// this response. Reporting success for work that has not happened yet is
	// how a sender is told to stop retrying something that then fails.
	return response.Accepted(ctx, constants.MsgAccepted, receipt)
}
