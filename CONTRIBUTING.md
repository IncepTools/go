# Contributing

Thanks for taking the time. This is short, and the parts that look like
rules are there because this is a framework: every decision in it is
repeated across every service that imports it.

## Getting set up

```bash
git clone https://github.com/IncepTools/go
cd go

make ci          # tidy, fmt, vet, test -race, examples
make build       # builds bin/icpt
make run-todo    # a runnable example on :8080
make help        # everything else
```

Go 1.26 or newer (the floor Fiber v3 sets). `golangci-lint` is optional
locally and runs in CI.

## Where things live

```
src/            all library code
  core/         errors, context, logging, config, DI, lifecycle, modules
  web/          routing, guards, interceptors, binding, responses, health
  data/         the scoped repository and database error translation
  platform/     cache, events, locks, scheduling
  reliability/  retry, breaker, idempotency, outbox
  testkit/      the in-memory test harness
tests/          every test, mirroring src/ and internal/
internal/       the CLI's own packages — unreachable from outside
cmd/icpt/       the CLI
examples/       four runnable services — a SEPARATE Go module
docs/           one page per capability
inceptools.go   the application factory, at the module root
```

Three things about this that are deliberate:

**`src/` is unusual for Go**, where packages normally sit at the module
root. It is here because this framework's whole argument is that a
predictable layout is worth more than the freedom to put things anywhere,
and a repository that did not follow its own advice would be hard to take
seriously. The cost is an extra path segment in every import.

**Tests live in `tests/`, not beside the code.** They mirror the package
path and are all external (`package errs_test`), so they exercise the
public API the way a consumer does. Coverage therefore needs `-coverpkg` —
`make cover` does it; a bare `go test -cover` reports zero, because the
test packages contain no statements of their own.

**Examples are the exception.** A `example_test.go` sits *beside* the
package it documents, because pkg.go.dev only renders examples it finds in
the package's own directory. They are documentation that happens to
compile, not tests — behaviour goes in `tests/`.

The examples module is separate so that a GORM dialector and a database
driver do not end up in the `go.sum` of every project that imports this
library. `make examples` builds it; the root `go build ./...` does not.

## The one rule that matters

**Explain the failure, not the mechanism.**

A comment that restates the code is noise. A comment that names the thing
that goes wrong without it is the most valuable line in the file, because
it survives the refactor that deletes the code around it.

```go
// Bad: says what the next line says.
// Set the account from the context.
tenant := ctxkit.TenantOf(ctx)

// Good: says what breaks without it.
// The context wins over the payload, always: otherwise a caller can create
// a row under another account by setting the field.
tenant := ctxkit.TenantOf(ctx)
```

The same applies to tests. `TestCreate` tells a reader nothing;
`TestReusingAKeyWithADifferentBodyIsRefused` tells them what is guaranteed,
and the failure message should say which real problem just came back:

```go
t.Fatalf("handler ran %d times; a retried POST must not provision twice", n)
```

## Code

- Every exported identifier has a doc comment, starting with its name.
- Errors crossing a package boundary are `*errs.Error`. Anything else
  becomes an unexplained 500 at the edge — correct, but useless.
- Anything that can be cancelled takes a `context.Context` first.
- Background work that outlives a request uses `ctxkit.Detach`, not the
  request context. The request context is cancelled when the client hangs
  up, and the side effect silently never happens.
- No new dependency in the root module without a reason in the pull
  request. A framework's dependencies become every importer's. CLI-only
  dependencies go under `internal/`; example-only ones go in the examples
  module. See [GOVERNANCE.md](GOVERNANCE.md).

## Tests

- `make ci` must pass. It runs with `-race`, deliberately: the container,
  the event bus, the scheduler and the breaker are all concurrent, and a
  race in any of them is a production defect no single-threaded test would
  find.
- A bug fix comes with a test that fails without the fix.
- Prefer a test through the real pipeline — `testkit.Mount` — over one with
  the HTTP layer mocked away. Most of what this framework does lives in
  that pipeline, and a mock of it quietly diverges.
- A new test goes in `tests/<same path as the package>`.

## Changing behaviour

A change that leaves a document describing the old behaviour has not
landed. In the same pull request:

- update the package's doc comment if the reasoning changed,
- update `README.md` and anything under `docs/` that mentions it,
- add a `CHANGELOG.md` entry under **Unreleased**.

## Changing generated code

`internal/scaffold/templates.go` is the shape every new module in every
service built with this framework will take. Treat a change there as a
change to all of them:

- The output must build and pass `icpt lint -strict`. CI checks this by
  scaffolding a module and compiling it.
- Prefer removing a line people delete anyway over adding one they will
  forget to add.

## Compatibility

Before 1.0 the API may change between minor versions, and the changelog
says how. After 1.0 it will not change within a major version.

An exported identifier is a promise. If you are unsure whether something
should be exported, do not export it — adding to the surface later is easy,
removing from it is not. A new method on an exported **interface** is
breaking; add a second interface and check for it with a type assertion,
which is how every optional capability here already works.

See [RELEASING.md](RELEASING.md).

## Commits and pull requests

Conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
`chore:`) — the release notes are generated from them. One logical change
per pull request; a formatting sweep bundled with a behaviour change hides
the behaviour change.

## Security

Not here. See [SECURITY.md](SECURITY.md).
