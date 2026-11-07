# minimal

The smallest useful inceptools service: one domain, one module, one route.

```bash
go run ./minimal

curl localhost:8080/api/v1/greeter/hello/world
curl localhost:8080/api/v1/greeter/hello/nobody   # 404, typed
curl localhost:8080/livez
```

It is one file, and the point is what is **absent**: no database, no
configuration struct, no guards, no scheduler. Everything else in the
framework is optional.

Two things it does show, because they are not optional:

- The route is a **value** returned by a controller, so it appears in the
  route table and can be asserted over in a test.
- The handler returns `errs.NotFound(...)` and never maps an error to a
  status. The envelope, the trace id and the status all follow from that.

Next: [todo](../todo) adds a module with state, validation and events.
