// Command minimal is the smallest useful inceptools service.
//
// One domain, one module, one route, no database:
//
//	go run ./minimal
//	curl localhost:8080/api/v1/greeter/hello/world
//	curl localhost:8080/livez
//
// Everything else in the framework is optional. This is the floor.
package main

import (
	"context"
	"log"
	"os"

	"github.com/gofiber/fiber/v3"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

func main() {
	app := inceptools.New(inceptools.Options{
		Service: "minimal",
		Addr:    ":8080",
		Prefix:  "/api/v1",
	})

	if err := app.Mount(greeterDescriptor()); err != nil {
		log.Fatal(err)
	}
	if err := app.Run(context.Background()); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

// --- the domain -------------------------------------------------------

func greeterDescriptor() module.Descriptor {
	return module.Descriptor{
		Build: func(*di.Scope) module.App {
			return module.NewApp("greeter", &helloModule{
				Base: module.NewBase("hello", module.WithControllers(
					router.ControllerFunc(func() []router.Route {
						return []router.Route{
							router.Get("/:name", hello).
								Summary("Greet someone").
								Param("name", "Who to greet").
								AsPublic(),
						}
					}),
				)),
			})
		},
	}
}

type helloModule struct{ module.Base }

func hello(c fiber.Ctx) error {
	name, err := pipe.StringParam(c, "name")
	if err != nil {
		return response.Fail(c, err)
	}
	if name == "nobody" {
		// A typed error decides its own status; the handler never maps one.
		return response.Fail(c, errs.NotFound("there is no %q to greet", name).
			WithCode("NO_SUCH_PERSON"))
	}
	return response.OK(c, "greeting rendered", fiber.Map{"hello": name})
}
