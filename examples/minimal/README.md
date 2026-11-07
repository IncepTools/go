# minimal

The smallest **complete** inceptools service: one domain, one module, one
route.

```bash
go run ./minimal

curl localhost:8080/api/v1/greeter/hello/world
curl localhost:8080/api/v1/greeter/hello/nobody   # 404, typed
curl localhost:8080/livez
```

## Layout

```
apps/greeter/                   the domain
  greeter_app.go                Descriptor, Providers, NewApp
  hello/                        the module
    hello_module.go             Providers, New
    constants/                  module name, messages
    dto/                        Greeting
    service/                    the one rule
    controller/                 the one route
```

Smallest *complete*, not smallest possible. Every layer the generator produces
is here, because the layout is the part worth copying and a one-file version
teaches a shape nobody should grow. The whole thing is about 25 lines of
behaviour spread across the structure it will still have at a hundred times
the size.

What is absent is the optional half: no database, no configuration struct, no
guards, no scheduler, no events. `main.go` mounts the domain and runs.

## Two things it does show, because they are not optional

**The route is a value.** A controller returns `[]router.Route` rather than
calling a register function, so the route appears in the route table and can
be asserted over in a test without starting a server. The summary, the
parameter and the response type are on the route, which is what puts them in
the generated documentation.

**The handler never maps an error to a status.** The service returns
`errs.NotFound(...)` and the framework decides the status, the envelope and
the trace id. A service says what went wrong; the wire format is not its
business — which is also why the service imports nothing from `web/`.

Next: [todo](../todo) adds state, validation, events and a scheduled job.
