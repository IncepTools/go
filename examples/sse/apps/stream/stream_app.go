package stream

import (
	"github.com/IncepTools/go/examples/sse/apps/stream/events"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

type App struct{ module.BaseApp }

func Descriptor() module.Descriptor {
	return module.Descriptor{Build: func(s *di.Scope) module.App { return &App{module.NewApp("stream", events.New(s))} }}
}
