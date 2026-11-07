// Package service holds the hello module's business rules.
//
// Note what it does NOT import: nothing from web/. Even here, where the rule
// is one comparison, keeping it out of the handler is what lets it be tested
// without a request and reused without an HTTP call.
package service

import (
	"context"

	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/dto"
	"github.com/IncepTools/go/src/core/errs"
)

// GreeterService renders greetings.
type GreeterService struct{}

// NewGreeterService builds the service.
func NewGreeterService() *GreeterService { return &GreeterService{} }

// Greet returns the greeting, or a typed error.
//
// The error carries its own kind, so the handler never maps one to a status.
// That is the whole mechanism: a service says what went wrong, the framework
// decides what the wire looks like.
func (s *GreeterService) Greet(_ context.Context, name string) (dto.Greeting, error) {
	if name == "nobody" {
		return dto.Greeting{}, errs.NotFound("there is no %q to greet", name).
			WithCode("NO_SUCH_PERSON")
	}
	return dto.Greeting{Hello: name}, nil
}
