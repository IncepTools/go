package events

import (
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

type App struct{ module.BaseApp }

func Descriptor() module.Descriptor {
	return module.Descriptor{Build: func(s *di.Scope) module.App { return &App{module.NewApp("events", notifications.New(s))} }}
}
