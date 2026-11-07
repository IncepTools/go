package queues

import (
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

type App struct{ module.BaseApp }

func Descriptor() module.Descriptor {
	return module.Descriptor{Build: func(s *di.Scope) module.App { return &App{module.NewApp("queues", jobs.New(s))} }}
}
