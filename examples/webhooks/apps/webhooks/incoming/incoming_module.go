package incoming

import (
	"context"
	"encoding/json"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/web/router"
	"github.com/IncepTools/go/src/web/webhook"
	"github.com/gofiber/fiber/v3"
)

type Received struct {
	Action string `json:"action"`
}
type Module struct {
	module.Base
	bus *events.Bus
}

func New(s *di.Scope) *Module {
	return &Module{Base: module.NewBase("incoming", module.WithControllers(&Controller{bus: di.MustResolve[*events.Bus](s)})), bus: di.MustResolve[*events.Bus](s)}
}

type Controller struct{ bus *events.Bus }

func (c *Controller) Routes() []router.Route {
	return []router.Route{router.Post("/", c.receive).AsPublic()}
}
func (c *Controller) receive(ctx fiber.Ctx) error {
	body := append([]byte(nil), ctx.Body()...)
	if err := webhook.Verify(body, "development-secret", ctx.Get("X-Hub-Signature-256")); err != nil {
		return fiber.ErrUnauthorized
	}
	var e Received
	if err := json.Unmarshal(body, &e); err != nil {
		return err
	}
	_ = events.Publish(context.Background(), c.bus, e)
	return ctx.JSON(fiber.Map{"accepted": true})
}
