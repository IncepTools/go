 # Releasing

Two things ship from this repository and they ship together:

- **The library**, `github.com/IncepTools/go`. Go modules have no registry
  to upload to — pushing a tag *is* the release, and
  `go get github.com/IncepTools/go@v1.2.3` resolves straight from here.
- **The CLI**, `icpt`. Built for five platforms, signed, with an SBOM, and
  published to the Homebrew tap.

## Versioning

[Semantic Versioning](https://semver.org). Before 1.0 the exported API may
change between minor versions, and every such change is listed in
[CHANGELOG.md](CHANGELOG.md) under **Changed** with what to do about it.

After 1.0, within a major version:

- No exported identifier is removed or changed incompatibly.
- No behaviour a documented contract promises is changed.
- New exported identifiers, new optional fields and new interface
  **implementations** are minor releases.

A new method on an exported **interface** is breaking, because it breaks
every implementation outside this repository. Add a second interface
instead, and check for it with a type assertion — which is how every
optional capability here already works (`lifecycle.Shutdowner`,
`module.EntityOwner`, `binding.Normalizer`).

Go's own rule applies at the top: a v2 is a different module path,
`github.com/IncepTools/go/v2`. That is expensive for importers, so it is a
last resort rather than a release cadence.

## Supported versions

Until 1.0, the latest minor only. After 1.0, the current and previous
minor. See [SECURITY.md](SECURITY.md).

## Before tagging

```bash
make ci            # tidy, fmt, vet, test -race
make lint          # golangci-lint
make examples      # the separate examples module
make vuln          # govulncheck
make apidiff       # what changed in the exported API since the last tag
make release-dry   # a full goreleaser build, nothing published
```

`make apidiff` is the one people skip and should not. It prints every
exported-API change since the last tag, and it is the difference between
choosing a version number and guessing one.

Then:

1. Move everything under **Unreleased** in `CHANGELOG.md` into a new
   version heading with today's date, and add the link at the bottom.
2. Check the README's **Status** section still describes reality.
3. Commit: `docs: release vX.Y.Z`.

## Tagging

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

The `release` workflow then runs the tests again — a release is the one
build nobody can re-run — and, only if they pass, builds, signs and
publishes.

Pre-releases work as you would expect: `v0.3.0-rc.1` is marked as a
pre-release automatically, and `go get` will not select it unless asked for
by name.

## Verifying a release

```bash
cosign verify-blob checksums.txt \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/IncepTools/go/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Signing is keyless: the identity is the release workflow's own OIDC token,
so there is no private key to lose and the signature proves the artifact
came from this repository rather than from somebody's laptop.

## If a release is bad

**Never delete or move a tag.** The module proxy has already cached it, and
re-pointing a tag means two different people get different code for one
version — which is far worse than a bad release.

Retract it instead:

```go
// go.mod
retract (
    v0.2.0 // Published with a broken migration ledger; use v0.2.1.
)
```

Release the retraction as `v0.2.1` along with the fix. `go get` then warns
anybody on `v0.2.0` and will not select it for a new install. The comment
is shown to users, so say what is wrong and what to use.

For a security problem, follow [SECURITY.md](SECURITY.md) first; the
retraction is the last step, not the first.

## Secrets the workflow needs

| | |
|---|---|
| `GITHUB_TOKEN` | Provided automatically. |
| `HOMEBREW_TAP_TOKEN` | A token with write access to `IncepTools/homebrew-tap`. Without it the Homebrew step is skipped and everything else still publishes. |

## Release checklist

- [ ] `make ci`, `make lint`, `make examples`, `make vuln` all pass
- [ ] `make apidiff` reviewed, and the version number matches what it says
- [ ] `CHANGELOG.md` has a dated section and a comparison link
- [ ] README **Status** is accurate
- [ ] `make release-dry` succeeds
- [ ] Tag pushed, workflow green
- [ ] `go install github.com/IncepTools/go/cmd/icpt@vX.Y.Z` works from a clean machine
- [ ] pkg.go.dev shows the new version
