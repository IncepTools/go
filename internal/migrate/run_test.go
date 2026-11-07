package migrate_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/IncepTools/go/internal/migrate"
)

// These exercise the whole apply/record/reverse path against a real engine.
// SQLite is used because it needs no server, and because it has
// transactional DDL — the same property the Postgres path relies on.

func newMigrator(t *testing.T, dir string) (*migrate.Migrator, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	m, err := migrate.Open(context.Background(), migrate.Options{
		Name: "test", DSN: dbPath, Dir: dir, Dialect: "sqlite",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, dbPath
}

func seedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pair(t, dir, "000001_create_notes",
		"CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT NOT NULL);",
		"DROP TABLE notes;")
	pair(t, dir, "000002_add_body",
		"ALTER TABLE notes ADD COLUMN body TEXT;",
		"ALTER TABLE notes DROP COLUMN body;")
	return dir
}

func TestUpAppliesEverythingAndIsIdempotent(t *testing.T) {
	dir := seedDir(t)
	m, dbPath := newMigrator(t, dir)
	ctx := context.Background()

	n, err := m.Up(ctx, migrate.UpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("applied = %d, want 2", n)
	}

	// A second run must apply nothing. A migrator that reapplies is worse
	// than one that does not run at all.
	again, err := m.Up(ctx, migrate.UpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second run applied %d migrations", again)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO notes (title, body) VALUES ('a','b')`); err != nil {
		t.Fatalf("the schema is not what the migrations describe: %v", err)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	dir := seedDir(t)
	m, dbPath := newMigrator(t, dir)
	ctx := context.Background()

	n, err := m.Up(ctx, migrate.UpOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("dry run reported %d", n)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	var count int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='notes'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("a dry run created the table")
	}
}

func TestToStopsAtAVersion(t *testing.T) {
	dir := seedDir(t)
	m, _ := newMigrator(t, dir)

	n, err := m.Up(context.Background(), migrate.UpOptions{To: "000001"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
}

func TestFailedMigrationLeavesNoSchemaChangeAndNoSuccessRow(t *testing.T) {
	dir := t.TempDir()
	pair(t, dir, "000001_ok",
		"CREATE TABLE a (id INTEGER PRIMARY KEY);", "DROP TABLE a;")
	pair(t, dir, "000002_broken",
		"CREATE TABLE b (id INTEGER PRIMARY KEY); THIS IS NOT SQL;", "DROP TABLE b;")

	m, dbPath := newMigrator(t, dir)
	ctx := context.Background()

	n, err := m.Up(ctx, migrate.UpOptions{})
	if err == nil {
		t.Fatal("the broken migration reported success")
	}
	if n != 1 {
		t.Fatalf("applied = %d, want 1 (the good one)", n)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()

	var tables int
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='b'`).Scan(&tables)
	if tables != 0 {
		t.Fatal("the failed migration left its table behind; it was not rolled back")
	}

	var ok int
	_ = db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='000002' AND status='success'`).Scan(&ok)
	if ok != 0 {
		t.Fatal("a failed migration was recorded as successful")
	}

	// There should still be evidence of the attempt.
	var failed int
	_ = db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='000002' AND status='failed'`).Scan(&failed)
	if failed == 0 {
		t.Fatal("the failed attempt left no record at all")
	}
}

func TestStatusReportsPendingAppliedAndDrift(t *testing.T) {
	dir := seedDir(t)
	m, _ := newMigrator(t, dir)
	ctx := context.Background()

	if _, err := m.Up(ctx, migrate.UpOptions{To: "000001"}); err != nil {
		t.Fatal(err)
	}

	rows, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if !rows[0].Applied || rows[1].Applied {
		t.Fatalf("status = %+v", rows)
	}

	// Edit an applied migration. This is the thing that makes a ledger stop
	// describing a rebuildable database.
	upPath := filepath.Join(dir, "000001_create_notes.up.sql")
	if err := os.WriteFile(upPath,
		[]byte("CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT, extra TEXT);"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err = m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Drifted {
		t.Fatal("an edited applied migration was not reported as drifted")
	}

	if _, err := m.Up(ctx, migrate.UpOptions{}); err == nil {
		t.Fatal("migrate proceeded over drift without -allow-drift")
	} else if !strings.Contains(err.Error(), "allow-drift") {
		t.Fatalf("the error does not say how to proceed deliberately: %v", err)
	}

	if _, err := m.Up(ctx, migrate.UpOptions{AllowDrift: true}); err != nil {
		t.Fatalf("-allow-drift did not work: %v", err)
	}
}

func TestStatusReportsAMissingFile(t *testing.T) {
	dir := seedDir(t)
	m, _ := newMigrator(t, dir)
	ctx := context.Background()

	if _, err := m.Up(ctx, migrate.UpOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "000002_add_body.up.sql")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "000002_add_body.down.sql")); err != nil {
		t.Fatal(err)
	}

	rows, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var missing bool
	for _, r := range rows {
		if r.Version == "000002" && r.Missing {
			missing = true
		}
	}
	if !missing {
		t.Fatalf("a deleted applied migration was not reported: %+v", rows)
	}
}

func TestDownRunsTheStoredScript(t *testing.T) {
	dir := seedDir(t)
	m, dbPath := newMigrator(t, dir)
	ctx := context.Background()

	if _, err := m.Up(ctx, migrate.UpOptions{}); err != nil {
		t.Fatal(err)
	}

	// Replace the file's down script with something that would fail. The
	// rollback must use what was STORED when the migration ran, not what is
	// checked out now.
	if err := os.WriteFile(filepath.Join(dir, "000002_add_body.down.sql"),
		[]byte("DROP TABLE does_not_exist;"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := m.Down(ctx, migrate.DownOptions{Steps: 1})
	if err != nil {
		t.Fatalf("rollback used the file rather than the ledger: %v", err)
	}
	if n != 1 {
		t.Fatalf("reversed = %d", n)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO notes (title, body) VALUES ('a','b')`); err == nil {
		t.Fatal("the column is still there; the rollback did nothing")
	}

	var left int
	_ = db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE status='success'`).Scan(&left)
	if left != 1 {
		t.Fatalf("ledger rows = %d, want 1", left)
	}
}

func TestDownRequiresSteps(t *testing.T) {
	m, _ := newMigrator(t, seedDir(t))
	if _, err := m.Down(context.Background(), migrate.DownOptions{Steps: 0}); err == nil {
		t.Fatal("down with no steps was accepted")
	}
}

func TestCustomLedgerTable(t *testing.T) {
	dir := seedDir(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	m, err := migrate.Open(context.Background(), migrate.Options{
		Name: "test", DSN: dbPath, Dir: dir, Dialect: "sqlite", LedgerTable: "icpt_migrations",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if _, err := m.Up(context.Background(), migrate.UpOptions{}); err != nil {
		t.Fatal(err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM icpt_migrations WHERE status='success'`).Scan(&n); err != nil {
		t.Fatalf("the custom ledger table was not used: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

func TestUnsupportedDialectIsRefused(t *testing.T) {
	_, err := migrate.Open(context.Background(), migrate.Options{
		Name: "x", DSN: "whatever", Dir: t.TempDir(), Dialect: "oracle",
	})
	if err == nil {
		t.Fatal("an unsupported dialect was accepted")
	}
}

func TestRollbackKeepsTheRowAndMakesTheVersionPendingAgain(t *testing.T) {
	// The ledger row is the only record of what the database once was.
	// Deleting it on rollback would leave a later investigation with
	// nothing, so the row is marked instead.
	dir := seedDir(t)
	m, dbPath := newMigrator(t, dir)
	ctx := context.Background()

	if _, err := m.Up(ctx, migrate.UpOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Down(ctx, migrate.DownOptions{Steps: 1}); err != nil {
		t.Fatal(err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()

	var status, down string
	err := db.QueryRow(
		`SELECT status, down_script FROM schema_migrations WHERE version = '000002'`).
		Scan(&status, &down)
	if err != nil {
		t.Fatalf("the rolled-back row was deleted: %v", err)
	}
	if status != migrate.StatusRolledBack {
		t.Fatalf("status = %q, want %q", status, migrate.StatusRolledBack)
	}
	if down == "" {
		t.Fatal("the stored down script was discarded")
	}

	// A rolled-back version is pending again.
	rows, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Version == "000002" && r.Applied {
			t.Fatal("a rolled-back migration still reports as applied")
		}
	}

	n, err := m.Up(ctx, migrate.UpOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("re-applied %d, want 1 — a rolled-back migration must be re-appliable", n)
	}
}

func TestEveryDialectBuildsItsLedger(t *testing.T) {
	// Only sqlite can be exercised without a server, but every dialect's
	// DDL should at least be well-formed and name the configured table.
	for _, name := range migrate.DialectNames() {
		d, err := migrate.DialectFor(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		stmts := d.LedgerDDL("icpt_migrations")
		if len(stmts) == 0 {
			t.Errorf("%s: no ledger DDL", name)
			continue
		}
		for _, s := range stmts {
			if !strings.Contains(s, "icpt_migrations") {
				t.Errorf("%s: a statement ignores the configured table name: %s", name, s)
			}
		}
		if d.Placeholder(1) == "" {
			t.Errorf("%s: empty placeholder", name)
		}
	}
}

func TestDialectAliases(t *testing.T) {
	for alias, want := range map[string]string{
		"pg": "postgres", "PG": "postgres", "mssql": "sqlserver", "Postgres": "postgres",
	} {
		d, err := migrate.DialectFor(alias)
		if err != nil {
			t.Errorf("%s: %v", alias, err)
			continue
		}
		if d.Name() != want {
			t.Errorf("%s resolved to %s, want %s", alias, d.Name(), want)
		}
	}
}
