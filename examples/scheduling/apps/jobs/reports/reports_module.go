package reports

import (
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

type Module struct{ module.Base }

func New(*di.Scope) *Module { return &Module{Base: module.NewBase("reports")} }
