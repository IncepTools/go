# Contributing

Thanks for taking the time. This document is short, and the parts that look
like rules are there because this is a framework: every decision in it is
repeated across every service that imports it.

## Getting set up

```bash
git clone https://github.com/IncepTools/go
cd go
make ci          # tidy-check, fmt-check, vet, test -race
make build       # builds bin/icpt
make example     # runs the example service on :8080
```

Go 1.26 or newer (the floor Fiber v3 sets). No other tooling is required; `golangci-lint` is optional
locally and runs in CI.

## The one rule that matters

**Explain the failure, not the mechanism.**

A comment that restates the code is noise. A comment that names the thing that
goes wrong without it is the most valuable line in the file, because it
survives the refactor that deletes the code around it.

```go
// Bad: says what the next line says.
// Set the tenant from the context.
tenant := ctxkit.TenantOf(ctx)

// Good: says what breaks without it.
// The context wins over the payload, always: otherwise a caller can create a
// row under another account by setting the field.
tenant := ctxkit.TenantOf(ctx)
```

The same applies to tests. `TestCreate` tells a reader nothing;
`TestReusingAKeyWithADifferentBodyIsRefused` tells them what is guaranteed,
and the failure message should say which real-world problem just came back.

## Code

- Every exported identifier has a doc comment, starting with its name.
- Errors returned across a package boundary are `*errs.Error`. Anything else
  becomes an unexplained 500 at the edge, which is correct but useless.
- Anything that can be cancelled takes a `context.Context` first.
- Background work that outlives a request uses `ctxkit.Detach`, not the
  request context. The request context is cancelled when the client hangs up,
  and the side effect silently never happens.
- No new dependency without a reason in the PR description. A framework's
  dependencies become every importer's dependencies.

## Tests

- `make ci` must pass. It runs with `-race`, and that is deliberate: the DI
  container, the event bus, the scheduler and the circuit breaker are all
  concurrent, and a race in any of them is a production defect no
  single-threaded test would find.
- A bug fix comes with a test that fails without the fix.
- Prefer a test through the real pipeline — `testkit.Mount` — over one with
  the HTTP layer mocked away. Most of what this framework does lives in that
  pipeline, and a mock of it quietly diverges.

## Changing behaviour

A change that leaves a document describing the old behaviour has not landed.
In the same pull request:

- update the package's doc comment if the reasoning changed,
- update `README.md` and anything under `docs/` that mentions it,
- add a `CHANGELOG.md` entry under **Unreleased**.

## Changing generated code

`internal/scaffold/templates.go` is the shape every new module in every
service that uses this framework will take. Treat a change there as a change
to all of them:

- The generated code must build and pass `icpt lint -strict`. CI checks this
  by scaffolding a module and compiling it.
- Prefer removing a line people delete anyway over adding one they will
  forget to add.

## Compatibility

Before 1.0 the API may change between minor versions, and the changelog says
how. After 1.0 it will not change within a major version.

An exported identifier is a promise. If you are unsure whether something
should be exported, do not export it — adding to the surface later is easy,
removing from it is not.

## Commits and pull requests

Conventional commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
`chore:`) — the release notes are generated from them. One logical change per
pull request; a formatting sweep bundled with a behaviour change hides the
behaviour change.

## Reporting security problems

Not here. See [SECURITY.md](SECURITY.md).
