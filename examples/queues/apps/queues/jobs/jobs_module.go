package jobs

import (
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/queue"
	"github.com/IncepTools/go/src/web/router"
	"github.com/gofiber/fiber/v3"
)

type Module struct{ module.Base }

func New(s *di.Scope) *Module {
	return &Module{Base: module.NewBase("jobs", module.WithControllers(&Controller{queue: di.MustResolve[*queue.Queue](s)}))}
}

type Controller struct{ queue *queue.Queue }

func (c *Controller) Routes() []router.Route {
	return []router.Route{router.Post("/", c.enqueue).AsPublic()}
}
func (c *Controller) enqueue(ctx fiber.Ctx) error {
	id, err := c.queue.Enqueue(ctx.Context(), ctx.Body())
	if err != nil {
		return err
	}
	return ctx.Status(fiber.StatusAccepted).JSON(fiber.Map{"id": id})
}
