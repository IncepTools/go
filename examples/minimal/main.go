// Command minimal is the smallest COMPLETE inceptools service.
//
// One domain, one module, one route, no database:
//
//	go run ./minimal
//	curl localhost:8080/api/v1/greeter/hello/world
//	curl localhost:8080/api/v1/greeter/hello/nobody   # 404, typed
//	curl localhost:8080/livez
//
// Smallest complete, not smallest possible. Every layer the generator
// produces is here — constants, dto, service, controller, module, domain —
// because the layout is the part worth copying, and a one-file version
// teaches a shape nobody should grow.
//
// What is absent is the optional half: no database, no configuration struct,
// no guards, no scheduler, no events.
package main

import (
	"context"
	"log"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/minimal/apps/greeter"
)

func main() {
	app := inceptools.New(inceptools.Options{
		Service: "minimal",
		Addr:    ":8080",
		Prefix:  "/api/v1",
	})

	// Mounting the domain is the whole wiring. The domain registers its
	// modules' providers and builds them; nothing here knows what a greeter
	// needs.
	if err := app.Mount(greeter.Descriptor()); err != nil {
		log.Fatal(err)
	}
	if err := app.Run(context.Background()); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
