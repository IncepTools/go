# Roadmap

What is planned, what is deliberately absent, and why. Dates are not
promised; order is roughly priority.

The [issues](https://github.com/IncepTools/go/issues) are the live list.
This is the shape behind them.

## Towards 1.0

The API is close to settled. What stands between here and a stable promise:

**Soak time.** The surface needs to survive a few real services before it
is frozen for a major version. Nothing is more expensive than promising
stability on an API that has only ever had one user.

**An OpenAPI generator.** The route metadata is already collected —
`Application.RoutesJSON` has the methods, paths, guards, parameters and
sample request and response types. Turning that into an OpenAPI 3.1
document is mechanical, and it is the single highest-value thing left:
generated clients for free, and a diffable public surface.

**A cursor-paging helper.** Offset paging degrades, and it is unstable
under concurrent inserts. The framework should offer keyset paging for
exports and syncs rather than leaving every module to reinvent it.

**Benchmarks, with numbers in the README.** Claims about overhead should be
measurable, and right now they are not stated at all.

## Likely, after that

**Metrics.** Request, job, breaker and outbox counters behind a small
interface, with a Prometheus adapter. Deliberately not a hard dependency on
any one metrics library.

**Tracing.** OpenTelemetry spans around the request pipeline, the
repository and the outbox relay, correlated with the trace id that already
flows through everything.

**A queue.** The outbox gets work out of a transaction reliably; there is
nothing here that consumes it with retries and a dead-letter path. Today
that is your broker's job, which is the right answer for most people.

**`icpt generate` for more shapes.** A guard, an interceptor, a migration
from an entity.

## Deliberately absent

These come up, and the answer is no unless something changes.

**GraphQL.** A different enough model that bolting it on would compromise
the HTTP design for both. A separate module could sit alongside.

**WebSockets and SSE.** Fiber handles them; the framework adds nothing
except a route that does not fit the request/response envelope. Reach past
to `app.Fiber`.

**Message-broker transports as a first-class concept.** The outbox plus
your broker's own client is simpler, more debuggable, and does not pretend
a network call is a method call.

**Internationalisation of error messages.** Error *codes* are the stable
contract and are meant to be translated by the client, which knows the
user's locale. A framework translating them server-side is translating the
wrong layer.

**An ORM of our own.** GORM is used in exactly one package and the rest of
the framework does not know your database exists. That is a feature.

**Autowiring by struct tag.** It saves three lines per type and costs a
runtime failure whose cause is a tag nobody read.

## Changing this

Open a [discussion](https://github.com/IncepTools/go/discussions). The
argument that moves something off "deliberately absent" is a concrete
failure it would prevent, not a capability it would add — see
[GOVERNANCE.md](GOVERNANCE.md).
