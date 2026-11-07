package notifications

import (
	"context"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/web/router"
	"github.com/gofiber/fiber/v3"
	"log/slog"
)

type OrderPlaced struct {
	ID string `json:"id"`
}
type Module struct{ module.Base }

func New(s *di.Scope) *Module {
	bus := di.MustResolve[*events.Bus](s)
	logger := di.MustResolve[*slog.Logger](s)
	events.On(bus, "orders.audit", events.Async, func(_ context.Context, e OrderPlaced) error { logger.Info("order placed", "id", e.ID); return nil })
	return &Module{Base: module.NewBase("notifications", module.WithControllers(&Controller{bus: bus}))}
}

type Controller struct{ bus *events.Bus }

func (c *Controller) Routes() []router.Route {
	return []router.Route{router.Post("/orders", c.publish).AsPublic()}
}
func (c *Controller) publish(ctx fiber.Ctx) error {
	var e OrderPlaced
	if err := ctx.Bind().Body(&e); err != nil {
		return err
	}
	if err := events.Publish(ctx.Context(), c.bus, e); err != nil {
		return err
	}
	return ctx.JSON(fiber.Map{"published": true})
}
