package events

import (
	"fmt"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/router"
	"github.com/IncepTools/go/src/web/sse"
	"github.com/gofiber/fiber/v3"
	"time"
)

type Module struct{ module.Base }

func New(*di.Scope) *Module {
	return &Module{Base: module.NewBase("events", module.WithControllers(&Controller{}))}
}

type Controller struct{}

func (*Controller) Routes() []router.Route {
	return []router.Route{router.Get("/", func(c fiber.Ctx) error {
		return sse.New(sse.Config{Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for i := 1; ; i++ {
				select {
				case <-stream.Context().Done():
					return nil
				case now := <-ticker.C:
					if err := stream.Event(sse.Event{Name: "tick", ID: fmt.Sprint(i), Data: fiber.Map{"time": now.UTC().Format(time.RFC3339)}}); err != nil {
						return err
					}
				}
			}
		}})(c)
	}).AsPublic()}
}
