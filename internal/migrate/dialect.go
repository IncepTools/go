package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Dialect is everything about a database engine that migration has to know.
//
// There is more variation here than "the placeholder character". The one that
// changes the DESIGN rather than the SQL is transactional DDL: PostgreSQL can
// run a schema change and record it in one transaction, so a failure leaves
// nothing behind. MySQL cannot — every DDL statement commits implicitly — so
// the same code there would leave a half-applied migration with no ledger
// row. Pretending otherwise is how a framework corrupts a MySQL database and
// blames the user.
type Dialect interface {
	// Name is the value written in icpt.json.
	Name() string
	// Driver is the database/sql driver to open with.
	Driver() string
	// Placeholder renders the nth bind parameter, 1-based.
	Placeholder(n int) string
	// LedgerDDL returns the statements that create or upgrade the ledger.
	// Each must be safe to run on every invocation.
	LedgerDDL(table string) []string
	// TransactionalDDL reports whether a schema change can be rolled back.
	TransactionalDDL() bool
	// Lock takes a cross-process advisory lock, returning a release
	// function. A dialect without one returns a no-op release and no error;
	// see the note on each implementation for what that costs.
	Lock(ctx context.Context, conn *sql.Conn) (release func(context.Context), err error)
	// QuoteIdent quotes an identifier.
	QuoteIdent(s string) string
}

var dialects = map[string]Dialect{}

func register(d Dialect) { dialects[d.Name()] = d }

func init() {
	register(postgresDialect{})
	register(mysqlDialect{})
	register(sqliteDialect{})
	register(sqlserverDialect{})
	register(clickhouseDialect{})
}

// aliases are the spellings accepted for a dialect name.
var aliases = map[string]string{
	"pg":    "postgres",
	"mssql": "sqlserver",
}

// DialectNames lists the supported dialects, sorted.
func DialectNames() []string {
	out := make([]string, 0, len(dialects))
	for name := range dialects {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// DialectFor resolves a dialect by name.
func DialectFor(name string) (Dialect, error) {
	if name == "" {
		name = "postgres"
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	d, ok := dialects[name]
	if !ok {
		return nil, fmt.Errorf("migrate: unsupported dialect %q (have: %s)",
			name, strings.Join(DialectNames(), ", "))
	}
	return d, nil
}

// ---------------------------------------------------------------- postgres

type postgresDialect struct{}

func (postgresDialect) Name() string             { return "postgres" }
func (postgresDialect) Driver() string           { return "pgx" }
func (postgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (postgresDialect) TransactionalDDL() bool   { return true }
func (postgresDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (postgresDialect) LedgerDDL(table string) []string {
	t := postgresDialect{}.QuoteIdent(table)
	return []string{
		`CREATE TABLE IF NOT EXISTS ` + t + ` (
			id          BIGSERIAL PRIMARY KEY,
			version     TEXT NOT NULL,
			name        TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'success',
			up_script   TEXT,
			down_script TEXT
		)`,
		`ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS applied_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS message TEXT`,
		`ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS duration_ms BIGINT`,
		`ALTER TABLE ` + t + ` ADD COLUMN IF NOT EXISTS error TEXT`,
	}
}

func (postgresDialect) Lock(ctx context.Context, conn *sql.Conn) (func(context.Context), error) {
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return nil, err
	}
	return func(c context.Context) {
		_, _ = conn.ExecContext(c, `SELECT pg_advisory_unlock($1)`, lockKey)
	}, nil
}

// ------------------------------------------------------------------- mysql

type mysqlDialect struct{}

func (mysqlDialect) Name() string               { return "mysql" }
func (mysqlDialect) Driver() string             { return "mysql" }
func (mysqlDialect) Placeholder(int) string     { return "?" }
func (mysqlDialect) QuoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// TransactionalDDL is false, and this is the single most important fact about
// migrating MySQL.
//
// Every DDL statement commits implicitly, so a migration that fails on its
// third statement leaves the first two applied with no way to roll them back.
// The migrator compensates by recording a "running" row BEFORE the work and
// settling it afterwards, which at least leaves evidence instead of silence —
// but it cannot make the schema change atomic. Keep MySQL migrations to one
// statement where you can.
func (mysqlDialect) TransactionalDDL() bool { return false }

func (mysqlDialect) LedgerDDL(table string) []string {
	t := mysqlDialect{}.QuoteIdent(table)
	return []string{
		`CREATE TABLE IF NOT EXISTS ` + t + ` (
			id          BIGINT AUTO_INCREMENT PRIMARY KEY,
			version     VARCHAR(64) NOT NULL,
			name        VARCHAR(255) NOT NULL,
			status      VARCHAR(32) NOT NULL DEFAULT 'success',
			up_script   LONGTEXT,
			down_script LONGTEXT,
			applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			message     LONGTEXT NULL,
			duration_ms BIGINT NULL,
			error       LONGTEXT NULL,
			KEY version_idx (version)
		)`,
	}
}

// Lock uses GET_LOCK, which is session-scoped and released when the
// connection closes — which is why the migrator holds one dedicated *sql.Conn
// for the whole run rather than borrowing from the pool.
func (mysqlDialect) Lock(ctx context.Context, conn *sql.Conn) (func(context.Context), error) {
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, ?)`, "icpt_migrate", 30).Scan(&got); err != nil {
		return nil, err
	}
	if !got.Valid || got.Int64 != 1 {
		return nil, fmt.Errorf("migrate: another migration is already running (GET_LOCK timed out)")
	}
	return func(c context.Context) {
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK(?)`, "icpt_migrate")
	}, nil
}

// ------------------------------------------------------------------ sqlite

type sqliteDialect struct{}

func (sqliteDialect) Name() string           { return "sqlite" }
func (sqliteDialect) Driver() string         { return "sqlite" }
func (sqliteDialect) Placeholder(int) string { return "?" }
func (sqliteDialect) TransactionalDDL() bool { return true }
func (sqliteDialect) QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (sqliteDialect) LedgerDDL(table string) []string {
	t := sqliteDialect{}.QuoteIdent(table)
	return []string{
		`CREATE TABLE IF NOT EXISTS ` + t + ` (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			version     TEXT NOT NULL,
			name        TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'success',
			up_script   TEXT,
			down_script TEXT,
			applied_at  TEXT NOT NULL DEFAULT (datetime('now')),
			message     TEXT,
			duration_ms INTEGER,
			error       TEXT
		)`,
	}
}

// Lock is a no-op: SQLite allows one writer at a time by construction, and
// two migrators against one file is not a situation worth coordinating — the
// second simply blocks on the file lock.
func (sqliteDialect) Lock(context.Context, *sql.Conn) (func(context.Context), error) {
	return func(context.Context) {}, nil
}

// rebind rewrites a query written with ? placeholders into the dialect's own
// form, so each statement is written once.
func rebind(d Dialect, query string) string {
	if d.Placeholder(1) == "?" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString(d.Placeholder(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// unlockTimeout bounds releasing a lock on the way out.
const unlockTimeout = 10 * time.Second

// --------------------------------------------------------------- sqlserver

type sqlserverDialect struct{}

func (sqlserverDialect) Name() string             { return "sqlserver" }
func (sqlserverDialect) Driver() string           { return "sqlserver" }
func (sqlserverDialect) Placeholder(n int) string { return fmt.Sprintf("@p%d", n) }
func (sqlserverDialect) TransactionalDDL() bool   { return true }
func (sqlserverDialect) QuoteIdent(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

func (sqlserverDialect) LedgerDDL(table string) []string {
	t := sqlserverDialect{}.QuoteIdent(table)
	lit := strings.ReplaceAll(table, "'", "''")
	return []string{
		`IF OBJECT_ID('` + lit + `', 'U') IS NULL
		CREATE TABLE ` + t + ` (
			id          BIGINT IDENTITY(1,1) PRIMARY KEY,
			version     NVARCHAR(64) NOT NULL,
			name        NVARCHAR(255) NOT NULL,
			status      NVARCHAR(32) NOT NULL DEFAULT 'success',
			up_script   NVARCHAR(MAX),
			down_script NVARCHAR(MAX),
			applied_at  DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
			message     NVARCHAR(MAX),
			duration_ms BIGINT NULL,
			error       NVARCHAR(MAX) NULL
		)`,
	}
}

// Lock uses an application lock scoped to the session, which is released
// when the connection closes.
func (sqlserverDialect) Lock(ctx context.Context, conn *sql.Conn) (func(context.Context), error) {
	_, err := conn.ExecContext(ctx,
		`EXEC sp_getapplock @Resource = 'icpt_migrate', @LockMode = 'Exclusive',
		 @LockOwner = 'Session', @LockTimeout = 30000`)
	if err != nil {
		return nil, err
	}
	return func(c context.Context) {
		_, _ = conn.ExecContext(c,
			`EXEC sp_releaseapplock @Resource = 'icpt_migrate', @LockOwner = 'Session'`)
	}, nil
}

// -------------------------------------------------------------- clickhouse

type clickhouseDialect struct{}

func (clickhouseDialect) Name() string           { return "clickhouse" }
func (clickhouseDialect) Driver() string         { return "clickhouse" }
func (clickhouseDialect) Placeholder(int) string { return "?" }
func (clickhouseDialect) QuoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// TransactionalDDL is false. ClickHouse has no transactions in the sense the
// other engines do: a multi-statement migration that fails part way leaves
// the earlier statements applied, and nothing can undo them.
//
// Write one statement per migration here, and prefer changes that are
// idempotent — CREATE TABLE IF NOT EXISTS, ADD COLUMN IF NOT EXISTS — so a
// re-run after a failure is safe.
func (clickhouseDialect) TransactionalDDL() bool { return false }

func (clickhouseDialect) LedgerDDL(table string) []string {
	t := clickhouseDialect{}.QuoteIdent(table)
	return []string{
		// ReplacingMergeTree keyed on version: re-recording a version
		// replaces the row rather than duplicating it, which is the closest
		// ClickHouse gets to the unique constraint the other engines use.
		`CREATE TABLE IF NOT EXISTS ` + t + ` (
			id          UInt64 DEFAULT 0,
			version     String,
			name        String,
			status      String DEFAULT 'success',
			up_script   String,
			down_script String,
			applied_at  DateTime DEFAULT now(),
			message     String DEFAULT '',
			duration_ms Int64 DEFAULT 0,
			error       String DEFAULT ''
		) ENGINE = ReplacingMergeTree(applied_at) ORDER BY version`,
	}
}

// Lock is a no-op: ClickHouse has no advisory lock. Two migrators against one
// cluster are not coordinated, so run migrations from one place.
func (clickhouseDialect) Lock(context.Context, *sql.Conn) (func(context.Context), error) {
	return func(context.Context) {}, nil
}
