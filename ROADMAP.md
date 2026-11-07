# Roadmap

This roadmap turns the framework into a production-grade Go application
platform without turning the core into a dependency-heavy monolith. The order
is intentional: contracts and execution semantics come before adapters and
convenience features. Dates are not promised; the issues are the live delivery
list.

## Product direction

inceptools should feel like a batteries-included framework while retaining Go's
best properties: explicit wiring, ordinary functions, small import surfaces,
fast startup, predictable allocations, and an escape hatch to Fiber or
`net/http` at every boundary.

The framework will unify the concepts that should be unified:

```text
module graph → provider graph → execution context → transport adapter
                                      ↓
                    HTTP / serverless / RPC / queue / event / schedule
                                      ↓
                  auth · validation · errors · telemetry · lifecycle
```

It will keep infrastructure that has a different operational contract
separate: GraphQL, WebSockets, broker clients, cloud provisioning, and vendor
SDKs should be optional packages or adapters, not mandatory imports.

## Completed foundations

**Application runtime.** Explicit modules, dependency injection, ordered
initialization and teardown, health probes, SIGHUP reload, graceful draining,
and `net/http.Handler` support for Lambda and other serverless adapters.

**HTTP pipeline.** Typed binding and validation, route metadata, middleware,
guards, interceptors, exception filters, safe response envelopes, CORS, rate
limiting, request IDs, SSE, webhooks, and route inspection.

**Background execution.** Cron and interval scheduling, bounded in-process
queues, typed events, retries, dead letters, distributed locks, idempotency,
and transactional outbox primitives.

**Observability.** OpenTelemetry tracing and metrics with standard OTLP/HTTP
configuration, W3C propagation, structured logs, error recording, and spans
across HTTP, schedules, queues, events, and arbitrary functions. The pipeline
is vendor-neutral and works with SigNoz, collectors, APMs, and other OTLP
backends.

**Data and reliability.** Scoped repositories, migrations with drift checks,
pagination, cache, retry, circuit breaker, and lifecycle-aware cleanup.

## Phase 1 — Contract-first framework (highest priority)

These are the capabilities that remove the most repeated service boilerplate
and make the existing architecture easier to trust.

### OpenAPI 3.1 and API contracts

- Generate OpenAPI 3.1 from the existing route registry, DTOs, binding rules,
  guards, errors, examples, and response metadata.
- Serve a versioned JSON document and optional Swagger UI/Redoc as an adapter.
- Add `icpt openapi check` for breaking-change detection in CI.
- Generate typed clients and server stubs through pluggable generators.
- Model pagination, idempotency, authentication schemes, deprecation, and
  webhook contracts explicitly.

This is the first priority because route metadata already exists and a single
contract can drive documentation, client generation, testing, and review.

### A complete request execution context

- Add a transport-neutral `ExecutionContext` containing service, module,
  operation, transport, route/message metadata, identity, cancellation, and
  trace context.
- Make guards, pipes, interceptors, filters, and telemetry consume that
  context consistently across HTTP, serverless, queues, events, and RPC.
- Add explicit global, module, controller, route, and handler scopes with a
  documented order and conflict rules.
- Preserve the current function-based API; no decorator or reflection runtime
  is required.

NestJS demonstrates the value of a documented middleware → guard → interceptor
→ pipe → handler → filter lifecycle. inceptools should provide the same clarity
with Go values and interfaces. See the [NestJS request lifecycle].

### Module and provider maturity

- Add module imports/exports as a checked dependency graph, with diagnostics
  for hidden or circular dependencies.
- Add dynamic module constructors for configurable packages such as cache,
  telemetry, database, and messaging adapters.
- Add provider scopes deliberately: singleton by default, request/execution
  scope only when requested, and no hidden goroutine-local state.
- Add startup graph output to `icpt doctor` and a JSON graph for CI and tooling.
- Add lifecycle hooks for providers and modules with clear failure and rollback
  semantics.

NestJS's module encapsulation and dynamic modules are useful patterns; its
decorator and reflection machinery is not a requirement for this Go API. See
the [NestJS modules] and [NestJS providers] documentation.

## Phase 2 — Production service platform

### Security and identity

- First-class authentication adapters for bearer/JWT, API keys, mTLS, and
  externally resolved identities.
- Route-level authorization policies for RBAC, ABAC, scopes, and ownership,
  with deny-by-default options and route inspection.
- A typed identity contract shared by HTTP, jobs, events, and RPC.
- CSRF, trusted proxy, request-size, content-type, secure-cookie, and security
  header policies with safe defaults.
- Audit events with actor, resource, decision, request ID, trace ID, and
  tamper-resistant delivery through the outbox.
- Secret references and redaction rules that prevent credentials from entering
  logs, spans, metrics, errors, or generated documentation.

### Config and runtime operations

- Typed config profiles for local, test, staging, and production.
- Validation reports that identify every invalid field before boot.
- Atomic reloadable configuration snapshots with versioning and rollback.
- `/livez`, `/readyz`, and detail health output with dependency timeouts,
  startup diagnostics, and readiness transitions.
- Standard shutdown and reload behavior for HTTP servers, workers, and
  serverless warm containers.

### Observability maturity

- Stable OpenTelemetry semantic conventions for HTTP, database, messaging,
  scheduler, and worker spans.
- Optional database, cache, lock, retry, breaker, and outbox instrumentation
  with query, key, token, and payload redaction.
- Trace propagation helpers for every supported broker adapter.
- Exemplars linking metrics to traces where the configured meter provider
  supports them.
- Bounded-cardinality policy and tests for every built-in metric.
- A small diagnostics endpoint for configuration, provider state, and build
  information without exposing secrets.

### Reliability and traffic control

- Bulkheads, concurrency limits, load shedding, timeout budgets, and adaptive
  backpressure.
- Retry budgets and policy composition across HTTP clients, jobs, and broker
  consumers.
- Durable queue adapters with visibility timeout, acknowledgement, retry
  scheduling, poison-message handling, and dead-letter replay.
- Exactly-once effects through documented idempotency and outbox patterns;
  never promise exactly-once delivery where the transport cannot provide it.
- Graceful rolling-deploy behavior: stop intake, drain work, flush telemetry,
  and report readiness correctly.

## Phase 3 — One programming model across transports

### Transport-neutral RPC and messaging

- A small canonical request/reply and event interface shared by HTTP, gRPC,
  NATS, Kafka, RabbitMQ, Redis Streams, SQS, and Pub/Sub adapters.
- Context propagation, auth, validation, errors, retries, timeouts, and
  telemetry applied consistently to every transport.
- Optional adapters in separate modules so importing HTTP does not pull in
  broker clients.
- Contract generation for gRPC/protobuf and message schemas, with compatibility
  checks and migration guidance.
- `icpt generate transport` for a handler, consumer, publisher, and tests.

NestJS's transport abstraction and GoFr's broad built-in service integrations
show the value of one application model across HTTP, RPC, and events. The Go
version should keep the transport implementations replaceable and avoid
requiring every adapter in the base binary. See the [NestJS microservices]
documentation and [GoFr's integration overview].

### Realtime adapters

- Optional WebSocket gateway with connection lifecycle, authentication,
  heartbeat, limits, backpressure, and trace-aware messages.
- SSE topics with replay cursor, authorization, bounded fan-out, and a broker
  backed hub; retain the current lightweight stream API for simple endpoints.
- Optional Socket.IO compatibility adapter only outside the core.

### API styles

- Optional GraphQL package with context, auth, telemetry, complexity limits,
  persisted queries, and schema generation.
- Optional gRPC gateway and content negotiation.
- Keep REST/OpenAPI as the default contract because it is the smallest common
  denominator for server, serverless, and external clients.

GraphQL and Socket.IO are valuable, but their execution and operational models
are distinct enough that they belong in adapters. This avoids the framework
turning every service into a large multi-protocol runtime.

## Phase 4 — Developer experience and application intelligence

### CLI and generators

- `icpt generate` for apps, modules, controllers, services, repositories,
  DTOs, guards, interceptors, pipes, filters, events, consumers, and tests.
- `icpt routes`, `icpt openapi`, `icpt graph`, and `icpt doctor` with machine
  readable output and actionable diagnostics.
- `icpt dev` with reload, dependency checks, local telemetry, and optional
  local infrastructure adapters.
- Generated examples that use the same code paths as production templates.

### Testing and verification

- Testkit helpers for HTTP, serverless invocation, events, queues, schedules,
  and reloads through the real application pipeline.
- Contract tests generated from OpenAPI and message schemas.
- Deterministic fake clock, retry scheduler, broker, lock, and telemetry
  exporters.
- Race, fuzz, allocation, and cancellation tests in the framework CI suite.
- `icpt lint -strict` rules for architecture, security, telemetry cardinality,
  unguarded mutation, and missing idempotency on retryable effects.

### Performance and dependency budget

- Publish reproducible routing, middleware, binding, telemetry, queue, and
  startup benchmarks with allocations and p95 latency.
- Maintain separate benchmark profiles for the minimal HTTP core and the full
  application stack.
- Keep optional integrations in separate packages and document their dependency
  cost, startup behavior, and memory profile.
- Add regression gates for hot-path allocations and cold-start duration.

## Phase 5 — Optional platform integrations

These features can make development and operations excellent, but they should
not become requirements for a self-hosted Go binary:

- local Postgres/Redis/broker orchestration and preview environments;
- service graph and architecture export;
- deployment manifests for Docker, Kubernetes, Lambda, Cloud Run, and ECS;
- secret-manager, object-storage, and cloud queue adapters;
- CI templates for contract checks, migrations, security scans, and release;
- an optional application model that tools can use for dependency diagrams and
  infrastructure planning.

Encore shows the leverage of deriving an application model for local
development, previews, infrastructure, and observability. inceptools should
expose a portable model and export hooks, while leaving provisioning to the
user's cloud or IaC system. See [Encore's application model].

## Deliberately not in the core

- Runtime decorators, reflection-driven autowiring, or annotation magic.
- A framework-owned ORM or query language.
- A proprietary telemetry backend or log format.
- Mandatory GraphQL, WebSocket, broker, cloud, or infrastructure SDKs.
- Claims of exactly-once delivery or transparent distributed transactions.
- Automatic production schema mutation.
- A framework-specific deployment control plane.

These may be available through companion modules. The core should remain useful
for a small HTTP binary, a long-running worker, a Lambda handler, and a
multi-transport service without forcing all of them to pay for each other's
dependencies.

## Priority summary

| Priority | Outcome | Main deliverables |
|---|---|---|
| P0 | Trustworthy contracts | OpenAPI 3.1, compatibility checks, execution context, provider graph |
| P1 | Production readiness | auth policies, config snapshots, semantic telemetry, bulkheads, durable queues |
| P2 | Transport parity | RPC/message adapters, propagation, schemas, WebSocket/SSE upgrades |
| P3 | Developer velocity | generators, local dev, testkit, graph/doctor, contract testing |
| P4 | Optional platform | previews, service model, deployment and cloud adapters |

## Research inputs

This roadmap takes the useful ideas from the compared projects without copying
their trade-offs:

- [NestJS modules], [providers], [request lifecycle], [guards], [interceptors],
  [pipes], and [microservices] — module boundaries, execution ordering, and a
  common model across transports.
- [Fiber documentation] and [Fiber contrib packages] — fast HTTP primitives,
  optional integrations, WebSockets, SSE, and keeping external dependencies out
  of the core.
- [GoFr observability] — production defaults for logs, metrics, cron, SQL,
  pub/sub, correlation, and OpenTelemetry.
- [Encore application model] — local development, previews, service graphs, and
  infrastructure derived from a portable application description.
- The listed Go projects — Gin, Echo, Chi, go-zero, go-kit, Kratos, fasthttp,
  goa, go-swagger, Kitex, Hertz, and others — inform the performance, RPC,
  design-first, and microservice adapter priorities. They are reference points,
  not dependencies.

The [GitHub issues] are the implementation queue. A roadmap item should become
an issue with an API sketch, dependency impact, failure modes, tests, docs, and
an example before it is considered ready to build.

[NestJS modules]: https://docs.nestjs.com/modules
[NestJS providers]: https://docs.nestjs.com/providers
[NestJS request lifecycle]: https://docs.nestjs.com/faq/request-lifecycle
[NestJS guards]: https://docs.nestjs.com/guards
[NestJS interceptors]: https://docs.nestjs.com/interceptors
[NestJS pipes]: https://docs.nestjs.com/pipes
[NestJS microservices]: https://docs.nestjs.com/microservices
[Fiber documentation]: https://docs.gofiber.io/
[Fiber contrib packages]: https://docs.gofiber.io/blog/fiber-v3-contrib-packages/
[GoFr observability]: https://gofr.dev/docs/quick-start/observability
[GoFr's integration overview]: https://gofr.dev/faq
[Encore application model]: https://encore.dev/docs/understanding-encore
[GitHub issues]: https://github.com/IncepTools/go/issues
