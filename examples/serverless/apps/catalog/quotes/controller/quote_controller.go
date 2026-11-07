// Package controller handles the quotes module's HTTP surface.
package controller

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/constants"
	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/dto"
	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/service"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// QuoteController serves the quote endpoint.
type QuoteController struct {
	svc *service.QuoteService
}

// New builds the controller.
func New(svc *service.QuoteService) *QuoteController { return &QuoteController{svc: svc} }

// Routes declares the endpoints.
//
// There is no reload route here on purpose. Reloading is the host's
// business — SIGHUP on a server, a control-plane callback in a function —
// and exposing it as an endpoint makes a configuration refresh something any
// caller can trigger.
func (c *QuoteController) Routes() []router.Route {
	return []router.Route{
		router.Get("/:symbol", c.quote).
			AsPublic().
			Summary("Quote one symbol").Tag("catalog").
			Param("symbol", "The symbol to quote").
			Returns(dto.Quote{}),
	}
}

func (c *QuoteController) quote(ctx fiber.Ctx) error {
	symbol, err := pipe.StringParam(ctx, "symbol")
	if err != nil {
		return response.Fail(ctx, err)
	}
	quote, err := c.svc.Quote(ctx.Context(), symbol)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgQuoted, quote)
}
