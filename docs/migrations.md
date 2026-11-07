# Migrations

Versioned SQL, two files per change, and a ledger table in the database:

```
migrations/
├── 000001_init.up.sql
├── 000001_init.down.sql
├── 000002_add_products_table.up.sql
└── 000002_add_products_table.down.sql
```

Nothing calls `AutoMigrate`. The schema is code you write and review.

## Commands

```bash
icpt create add products table   # writes the next numbered pair
icpt migrate                     # apply everything pending
icpt migrate -n                  # dry run
icpt migrate -to 000012          # stop after a version
icpt status                      # applied, pending, drifted, missing
icpt down -steps 1 -db primary   # reverse, after a typed confirmation
```

`-db <name>` picks the target. `icpt migrate` with no `-db` migrates **every**
configured database — applying is additive and idempotent, so fanning out is
safe. `icpt down` and `icpt create` **refuse** to guess: a rollback across
every database because a flag was omitted is unrecoverable.

## Configuration

```jsonc
{
  "dialect": "postgres",
  "databases": {
    "primary":   { "env_variable": "DATABASE_URL",  "migration_path": "./migrations" },
    "reporting": { "env_variable": "REPORTING_URL", "dialect": "clickhouse" }
  }
}
```

A single-database project can use the short form:

```jsonc
{ "env_variable": "DATABASE_URL", "migration_path": "./migrations", "dialect": "postgres" }
```

which yields one target named `default`.

| Option | |
|---|---|
| `env_variable` | The variable holding the DSN. Required. |
| `migration_path` | Directory of the SQL files. Defaults to `./migrations/<name>`. |
| `dialect` | Overrides the file's. |
| `ledger_table` | Default `schema_migrations`. |
| `no_lock` | Skip the advisory lock. Two deploys can then both apply the same migration. |

The DSN lives in the **environment** and never in the file, because a
connection string holds a password. For a one-off run, pass it as an
argument: `icpt migrate -db primary "postgres://…"`.

## Dialects

`postgres` (alias `pg`), `mysql`, `sqlite`, `sqlserver` (alias `mssql`) and
`clickhouse`.

### Transactional DDL is the difference that matters

| | Transactional DDL | Consequence |
|---|---|---|
| PostgreSQL, SQLite, SQL Server | yes | The migration and its ledger row are one transaction. A failure leaves nothing. |
| MySQL, ClickHouse | **no** | Every DDL statement commits implicitly. A failure part way leaves the earlier statements applied. |

Where it is unavailable, the migrator writes a `running` ledger row **before**
the work and settles it afterwards, so a crash leaves evidence instead of
silence. That is as far as compensation goes — it cannot make the change
atomic.

On MySQL and ClickHouse: keep a migration to **one statement** where you can,
and prefer forms that are safe to re-run (`CREATE TABLE IF NOT EXISTS`,
`ADD COLUMN IF NOT EXISTS`).

## What the migrator insists on

**Duplicate versions are refused.** Given `000027_a` and `000027_b`, a ledger
keyed on the version applies one of them and records the other's name.
Nothing warns. The migration that did not run simply does not exist, and the
stored scripts belong to a different file than the name beside them — which
is also what a rollback would run.

**Every migration has a down script.** A change you cannot reverse is one you
cannot deploy on a Friday.

**One transaction per migration, ledger row included**, where the engine
allows it. A half-applied migration with no record is an archaeology
exercise; this way a re-run is a clean retry.

**An advisory lock around the whole run.** Two deploys rolling at once
otherwise both see version 11 as current and both apply 12. PostgreSQL uses
`pg_advisory_lock`, MySQL `GET_LOCK`, SQL Server `sp_getapplock`, each on a
connection held for the length of the run. SQLite needs none — one writer at
a time, by construction. ClickHouse has none available, so run its migrations
from one place.

**Rollback runs the stored script.** The file on disk belongs to what is
checked out now; the database was built by what ran then. After a squash, a
rename or a revert, those are different things.

**Drift blocks.** An applied migration whose file has since been edited is
reported by `icpt status` and refused by `icpt migrate`:

```
migrate: these applied migrations differ from their files: 000012_add_price_index
An applied migration must never be edited — the database was built by what ran,
not by what is on disk now. Write a new migration, or pass -allow-drift if you
have reconciled the ledger deliberately
```

`-allow-drift` exists for the one case where it is correct: you reconciled
the ledger on purpose, as a squash does.

## The ledger

`schema_migrations`, created and owned by the migrator. Do not create it in a
migration, and do not drop it in a down script.

| Column | |
|---|---|
| `version`, `name` | From the file name. |
| `status` | `success`, `rollback`, `failed`, or `running` mid-apply. |
| `up_script`, `down_script` | What actually ran. The rollback source. |
| `applied_at`, `message`, `duration_ms`, `error` | |

**A rollback marks the row, it does not delete it.** The stored scripts are
the only record of what the database once was; erasing them leaves a later
investigation with nothing. A `rollback` row counts as pending, so the next
run re-applies it.

A service can refuse to boot against an unmigrated database:

```go
var applied int64
err := db.Raw(`SELECT count(*) FROM schema_migrations WHERE status = 'success'`).
	Scan(&applied).Error
if err != nil || applied == 0 {
	return errors.New("the database has no applied migrations; run icpt migrate")
}
```

## Writing one

```sql
-- 000002_add_products_table.up.sql
CREATE TABLE products (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL,
    name        TEXT NOT NULL,
    price       BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX products_account_id_idx ON products (account_id);
CREATE UNIQUE INDEX products_account_name_idx ON products (account_id, name)
    WHERE deleted_at IS NULL;
```

```sql
-- 000002_add_products_table.down.sql
DROP TABLE products;
```

### Conventions

**Index every column you filter by**, starting with `account_id`. A scoped
repository filters every query by it; without the index, every query is a
sequential scan.

**Keep the uniqueness constraint even when the service also checks.** The
check alone is a race — two concurrent creates both see nothing and both
insert. The check gives a good message; the index gives the guarantee.

**Never edit an applied migration.** Write a new one. `icpt status` will tell
you when somebody did.

**Two migrations in one pull request are usually one migration.** Each is a
separate transaction, so a failure between them leaves a state neither file
describes.

**Think about the deploy, not just the schema.** A column rename is three
deploys: add the new column and write both; backfill and switch reads; drop
the old one. A single `ALTER ... RENAME` breaks the replicas still running
the old code.

**Watch for locks on a large table.** `ALTER TABLE ... ADD COLUMN` with a
non-constant default rewrites it; `CREATE INDEX` without `CONCURRENTLY`
blocks writes. `CREATE INDEX CONCURRENTLY` cannot run inside a transaction,
so it belongs in a migration of its own, applied with the knowledge that it
is not atomic.

## Next

- [`icpt.json`](project-config.md) · [Database](database.md) · [CLI](cli.md)
