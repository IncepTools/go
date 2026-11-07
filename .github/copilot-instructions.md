# inceptools instructions

Read `AGENTS.md` and `.agents/skills/inceptools/SKILL.md` before changing this
repository. Use the `icpt` CLI and existing framework packages, preserve the
controller -> service -> repository -> database boundaries, mirror tests under
`tests/`, and remember `examples/` is a separate Go module. Run focused tests,
the relevant root/examples checks, and `git diff --check`. Never add secrets or
perform destructive release, tag, migration, or publishing actions without
explicit user authorization.

Use `src/core/telemetry` for OpenTelemetry instrumentation and keep spans
flowing through HTTP, schedulers, queues, events, and arbitrary functions.
New services use `/livez` and `/readyz`; `/healthz` is only a compatibility
alias. Runtime reloads use `Application.OnReload` and `Application.Reload`, so
the same code works with SIGHUP in a server and a control-plane callback in
serverless.
