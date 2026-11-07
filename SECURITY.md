# Security Policy

## Supported versions

Until 1.0, only the latest minor release receives fixes. After 1.0, the
current and previous minor releases will.

| Version | Supported |
| ------- | --------- |
| 0.1.x   | yes       |

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private reporting — *Security* →
*Report a vulnerability* on this repository — or email
**security@inceptools.dev**.

Please include what the issue is, which package and version, how to reproduce
it, and what an attacker gets out of it.

What to expect:

- Acknowledgement within 3 working days.
- An assessment, and a fix or a plan, within 14 days.
- Credit in the release notes, unless you would rather not be named.

We ask for 90 days before public disclosure, or less once a fix is released.

## What counts

This is a library and a CLI, so most of its security surface is what it does
on your behalf. These are in scope:

- Anything that lets one tenant read or write another's data — in particular
  `data/repo`'s account scoping and `core/ctxkit`.
- Anything that leaks internal detail to a client: `web/response`'s sanitising
  funnel and `web/filter`'s handling of unclassified errors.
- Authorization that can be bypassed: `web/guard`, and the route mounting in
  `web/router` that decides when guards run relative to the handler.
- Credential handling: `core/config`'s secret masking, and the migrator's rule
  that a DSN comes from the environment and never from `icpt.json`.
- `reliability/idempotency`, where a flaw means a duplicated charge or a
  duplicated resource.
- Injection in anything that builds SQL: `web/pagination`'s sort allow-list
  and `data/gormx`.

These are out of scope:

- Denial of service from a configuration you chose — an unbounded rate limit,
  a disabled timeout.
- Vulnerabilities in a dependency, unless this project's use of it is what
  makes it exploitable. Report those upstream; tell us too and we will bump.
- The example under `examples/`, which authenticates from a request header and
  says so in a comment. It is an example of the framework, not of
  authentication.
