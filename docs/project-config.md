# `icpt.json`

The per-repository configuration for the `icpt` CLI. It is optional: with no
file you get the defaults below and everything works.

`icpt` searches upward from the working directory, stopping at a `go.mod`, so
a command run from `apps/catalog/products` finds the repository's file.

## Everything, with defaults

```jsonc
{
  // Read from go.mod when absent.
  "module": "github.com/acme/shop",

  "layout": {
    "apps":        "apps",                   // where domains live
    "tests":       "colocated",              // or "mirror"
    "tests_dir":   "tests",                  // root for mirrored tests
    "module_file": "{module}_module.go",
    "app_file":    "{app}_app.go",
    "layers": {
      // MERGED with the defaults, never replacing them.
      "repository": ["gateway"]
    }
  },

  "generate": {
    "repository": true,
    "tests":      true,
    "constants":  true
  },

  "rules": {
    "service-imports-http":      "error",
    "controller-imports-db":     "error",
    "dto-imports-db":            "error",
    "entity-imports-layer":      "warn",
    "cross-module-internals":    "warn",
    "module-has-no-module-file": "error",
    "app-has-no-root-file":      "warn"
  },

  "skip": ["apps/legacy"],

  // Migration targets — see migrations.md
  "dialect": "postgres",
  "databases": {
    "primary": { "env_variable": "DATABASE_URL", "migration_path": "./migrations" }
  }
}
```

## Layout

**`apps`** — a repository keeping domains under `internal/apps` sets it here,
and both the linter and the generator follow.

**`tests`** — `"colocated"` puts a generated module test beside the module,
the Go default. `"mirror"` puts it at `tests/<app>/<module>/`. Both produce
an external test package, so the generated file compiles either way.

**`module_file` / `app_file`** — `{module}` and `{app}` are substituted. A
repository that prefers `module.go` over `products_module.go` says so here.

**`layers`** — which directory names count as which logical layer. The
defaults already cover the common aliases:

| Layer | Default names |
|---|---|
| `controller` | controller, controllers, handler, handlers |
| `service` | service, services, usecase, usecases |
| `entity` | entity, entities, model, models, domain |
| `dto` | dto, dtos, types, schema |
| `repository` | repository, repositories, repo, store, storage |
| `constants` | constants, const |

Names you add **merge** with these. Naming one extra alias must not switch
off every rule for every other layer, which is what replacing would quietly
do.

## Generate

Turn `repository` off for a service whose modules talk to something other
than a database — a gateway, a worker. The generated service then calls its
dependency directly.

## Rules

Each is `"error"`, `"warn"` or `"off"`. `icpt lint` exits non-zero on any
error, and on any warning with `-strict`.

Turning a rule off is a legitimate decision. Record why in a comment next to
it, or the next person turns it back on and reverts a week of work.

## Skip

Path fragments the linter ignores. Useful for a directory being migrated
into the conventions rather than one exempt from them.

## Databases

```jsonc
"databases": {
  "primary":   { "env_variable": "DATABASE_URL",  "migration_path": "./migrations" },
  "reporting": { "env_variable": "REPORTING_URL", "dialect": "clickhouse" },
  "cache":     { "env_variable": "CACHE_URL", "dialect": "sqlite",
                 "ledger_table": "icpt_migrations", "no_lock": true }
}
```

| Option | |
|---|---|
| `env_variable` | The variable holding the DSN. Required. |
| `migration_path` | Defaults to `./migrations/<name>`. |
| `dialect` | Overrides the top-level one. |
| `ledger_table` | Default `schema_migrations`. |
| `no_lock` | Skip the advisory lock. |

A single-database project can use the short form instead of the map:

```jsonc
{ "env_variable": "DATABASE_URL", "migration_path": "./migrations", "dialect": "postgres" }
```

That yields one target named `default`. Setting **both** forms is an error
rather than a merge: it reads as though it configures two targets and does
not.

The DSN is never in this file. See [Migrations](migrations.md).

## Validation

A malformed file is an error, not a silent fallback to the defaults — quietly
ignoring it would apply conventions the author did not ask for and did not
notice. `icpt doctor` reports the same problems with a hint each.

```
icpt.json: layout.tests is "somewhere"; expected "colocated" or "mirror";
database "reporting" has no env_variable
```

## Keys from other tools

`icpt` **preserves keys it does not recognise** when it writes the file, so
another tool's section survives `icpt init -force`.

## Examples

A repository with domains under `internal`, mirrored tests and no repository
layer:

```jsonc
{
  "layout": { "apps": "internal/apps", "tests": "mirror", "tests_dir": "test" },
  "generate": { "repository": false }
}
```

A repository adopting the conventions gradually:

```jsonc
{
  "skip": ["apps/legacy"],
  "rules": {
    "cross-module-internals": "off",
    "app-has-no-root-file": "off"
  }
}
```

Turn them back on one at a time as the tree is cleaned up.

## Next

- [CLI](cli.md) · [Migrations](migrations.md) · [Architecture](architecture.md)
