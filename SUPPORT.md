# Support

## Where to go

| | |
|---|---|
| **How do I…?** | [Discussions → Q&A](https://github.com/IncepTools/go/discussions/categories/q-a) |
| **This behaves differently from the docs** | [Open an issue](https://github.com/IncepTools/go/issues/new?template=bug_report.yml) |
| **The framework should do X** | [Discussions → Ideas](https://github.com/IncepTools/go/discussions/categories/ideas), then an issue if it has legs |
| **A security problem** | **Not a public issue.** See [SECURITY.md](SECURITY.md) |

## Before you ask

Most questions are answered faster by one of these:

- [`docs/`](docs/) — one page per capability, and each explains the
  reasoning rather than only the API.
- `go doc github.com/IncepTools/go/src/data/repo` — every package's doc comment
  says what it is for and which failure it prevents. Often faster than the
  docs directory.
- [`examples/`](examples/) — four runnable services. If you are wondering
  how two pieces fit together, one of them probably does it.
- `icpt doctor` — checks whether the project is set up the way the tooling
  expects.

## Asking well

A failing test against this repository is the fastest possible bug report,
and usually faster to write than a paragraph. Failing that: what you
expected, what happened, and the shortest `main.go` or `icpt` command that
shows it. Include `icpt version` and `go version`.

## Response times

This is maintained by people with other work. There is no SLA.

Bugs with a reproduction are looked at first, because they can be. A report
without one may sit until somebody happens to hit the same thing.

Security reports are acknowledged within three working days — see
[SECURITY.md](SECURITY.md).

## Version support

Until 1.0, fixes land on the latest minor only. See [RELEASING.md](RELEASING.md).
