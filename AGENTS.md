# Working on inceptools

This repository is a Go application framework and CLI. Changes should preserve
the framework's central promise: a service's important failure modes are
visible at boot, in tests, or in icpt lint, rather than discovered by a
production request.

## Repository map

- inceptools.go — application factory and runtime lifecycle.
- src/ — public framework packages.
- tests/ — external tests mirroring src/ and internal/.
- internal/ — CLI-only implementation and generated project templates.
- cmd/icpt/ — the icpt command.
- examples/ — a separate Go module containing runnable services.
- docs/ — one guide per public capability.

## Required checks

Run the narrowest relevant checks while iterating, then run the full suite:

    GOCACHE=/tmp/inceptools-gocache go test ./...
    cd examples && GOCACHE=/tmp/inceptools-examples-gocache go test ./...
    cd .. && make ci

For changes to dependencies, security-sensitive code, or release files, also
run make lint, make vuln, and make release-dry.

## Change conventions

- Keep tests in the mirrored tests/ tree and use external test packages.
- Add or update a runnable example when a public feature needs a usage story.
- Update the relevant guide and CHANGELOG.md under Unreleased.
- Keep exported identifiers documented and run goimports when adding imports.
- Do not add a root dependency without explaining why it belongs in every
  consumer's dependency graph.
- Use Conventional Commit subjects: feat:, fix:, docs:, test:, refactor:, or
  chore:.

Read CONTRIBUTING.md for the complete architecture and review rules, and
RELEASING.md before changing versions.

## AI agents

The framework-specific agent instructions are available at
[`.agents/skills/inceptools/SKILL.md`](.agents/skills/inceptools/SKILL.md).
Agents working on an inceptools project should load that skill before creating
modules, changing framework features, or running release workflows.
