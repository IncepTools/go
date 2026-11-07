// Package migrate applies versioned SQL migrations, driven by icpt.json.
//
// It is the migration half of the icpt CLI, and it keeps the file and ledger
// conventions already in use so an existing repository needs no change:
//
//	migrations/000012_add_product_sku.up.sql
//	migrations/000012_add_product_sku.down.sql
//
// and a `schema_migrations` table the application checks for at boot.
//
// Four properties this implementation insists on, each because its absence
// has cost somebody a database:
//
//   - DUPLICATE VERSIONS ARE REFUSED. Given 000027_a and 000027_b a ledger
//     keyed on the version applies one of them and records the other's name.
//     Nothing warns, and the migration that did not run simply does not
//     exist. This refuses to start.
//   - ONE TRANSACTION PER MIGRATION, with the ledger row written inside it.
//     A migration that fails half way leaves neither schema change nor ledger
//     row, so a re-run is a clean retry rather than an archaeology exercise.
//   - AN ADVISORY LOCK around the whole run. Two deploys rolling at once
//     otherwise both see version 11 as the latest and both apply 12.
//   - THE DOWN SCRIPT IS STORED, and rollback runs the STORED one. The file
//     on disk belongs to whatever is checked out now; the database was built
//     by what ran then.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // database/sql driver: clickhouse
	_ "github.com/go-sql-driver/mysql"         // database/sql driver: mysql
	_ "github.com/jackc/pgx/v5/stdlib"         // database/sql driver: postgres
	_ "github.com/microsoft/go-mssqldb"        // database/sql driver: sqlserver
	_ "modernc.org/sqlite"                     // database/sql driver: sqlite (cgo-free)
)

// DefaultLedgerTable is the table recording what has been applied.
const DefaultLedgerTable = "schema_migrations"

// Ledger statuses.
//
// A reversed migration becomes StatusRolledBack; its row is KEPT, not
// deleted. Deleting it would throw away the up and down scripts that were
// actually run, which are the only record of what the database once was —
// and the only thing a later investigation has to go on.
const (
	// StatusSuccess marks an applied migration. A service checking whether
	// its schema is current counts rows with this status.
	StatusSuccess = "success"
	// StatusRolledBack marks a migration that was reversed. The version is
	// then pending again, and will be re-applied by the next run.
	StatusRolledBack = "rollback"
	// StatusFailed marks an attempt that did not complete.
	StatusFailed = "failed"
	// StatusRunning marks a migration in flight on an engine without
	// transactional DDL, so a crash leaves evidence rather than silence.
	StatusRunning = "running"
)

// lockKey is the advisory lock id held for the length of a run. Any constant
// works as long as every migrator uses the same one.
const lockKey int64 = 0x1C9700112

// File is one migration on disk.
type File struct {
	Version  string
	Name     string
	UpPath   string
	DownPath string
	Up       string
	Down     string
}

// Applied is one ledger row.
type Applied struct {
	Version   string
	Name      string
	Status    string
	Up        string
	Down      string
	AppliedAt time.Time
}

// Status is one migration's position.
type Status struct {
	Version string
	Name    string
	// Applied is true when a successful ledger row exists.
	Applied bool
	// Missing is true for a ledger row with no file: the migration ran once
	// and the file has since been deleted or renamed, so the database cannot
	// be rebuilt from this checkout.
	Missing bool
	// Drifted is true when the file's SQL differs from what was recorded.
	Drifted    bool
	AppliedAt  time.Time
	StatusText string
}

// Options configures one migrator.
type Options struct {
	// Name identifies the target in messages.
	Name string
	// DSN is the connection string. It comes from the environment, never
	// from a file in the repository.
	DSN string
	// Dir is the migration directory.
	Dir string
	// Dialect is "postgres", "mysql" or "sqlite". Empty means postgres.
	Dialect string
	// LedgerTable overrides the ledger's name, for a repository that
	// already has a table called schema_migrations belonging to something
	// else.
	LedgerTable string
	// NoLock skips the advisory lock. Only for a dialect or a deployment
	// where it cannot work; without it, two concurrent deploys can both
	// apply the same migration.
	NoLock bool
}

// Migrator runs migrations against one database.
type Migrator struct {
	db      *sql.DB
	dialect Dialect
	dir     string
	name    string
	table   string
	noLock  bool
	// Out receives progress lines. Defaults to discarding them.
	Out func(format string, args ...any)
}

// Open connects to a database and prepares a migrator for it.
func Open(ctx context.Context, opt Options) (*Migrator, error) {
	if strings.TrimSpace(opt.DSN) == "" {
		return nil, fmt.Errorf("migrate: the DSN for %q is empty", opt.Name)
	}
	d, err := DialectFor(opt.Dialect)
	if err != nil {
		return nil, err
	}
	table := opt.LedgerTable
	if table == "" {
		table = DefaultLedgerTable
	}
	if !validLedgerTable(table) {
		return nil, fmt.Errorf("migrate: invalid ledger table %q; use letters, digits and underscores", table)
	}

	db, err := sql.Open(d.Driver(), opt.DSN)
	if err != nil {
		return nil, fmt.Errorf("migrate: opening %q: %w", opt.Name, err)
	}
	// Two connections: one holds the advisory lock for the whole run, the
	// other does the work. A session-scoped lock released when its
	// connection returns to the pool would protect nothing.
	db.SetMaxOpenConns(2)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %q is unreachable: %w", opt.Name, err)
	}
	return &Migrator{
		db: db, dialect: d, dir: opt.Dir, name: opt.Name,
		table: table, noLock: opt.NoLock,
		Out: func(string, ...any) {},
	}, nil
}

// Dialect returns the engine this migrator targets.
func (m *Migrator) Dialect() Dialect { return m.dialect }

// exec runs a statement written with ? placeholders.
func (m *Migrator) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return m.db.ExecContext(ctx, rebind(m.dialect, q), args...)
}

// Close releases the connection.
func (m *Migrator) Close() error { return m.db.Close() }

func (m *Migrator) logf(format string, args ...any) {
	if m.Out != nil {
		m.Out(format, args...)
	}
}

// Load reads the migration directory, sorted by version.
//
// It refuses a directory containing two migrations with one version, and a
// migration with no down script — a migration you cannot reverse is one you
// cannot deploy on a Friday.
func Load(dir string) ([]File, error) {
	ups, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	if len(ups) == 0 {
		return nil, fmt.Errorf("migrate: no *.up.sql files in %s", dir)
	}
	sort.Strings(ups)

	seen := map[string]string{}
	out := make([]File, 0, len(ups))
	for _, up := range ups {
		base := filepath.Base(up)
		stem := strings.TrimSuffix(base, ".up.sql")
		version, name, ok := strings.Cut(stem, "_")
		if !ok || version == "" {
			return nil, fmt.Errorf("migrate: %s is not named <version>_<name>.up.sql", base)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf(
				"migrate: version %s is claimed by both %s and %s — "+
					"the ledger is keyed on the version, so one of them would never run and "+
					"the other would be recorded under the wrong name", version, prev, base)
		}
		seen[version] = base

		upSQL, err := os.ReadFile(up)
		if err != nil {
			return nil, err
		}
		downPath := filepath.Join(dir, stem+".down.sql")
		downSQL, err := os.ReadFile(downPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("migrate: %s has no matching %s; "+
					"every migration needs a reverse", base, filepath.Base(downPath))
			}
			return nil, err
		}

		out = append(out, File{
			Version: version, Name: name,
			UpPath: up, DownPath: downPath,
			Up: string(upSQL), Down: string(downSQL),
		})
	}
	return out, nil
}

// ensureLedger creates the ledger table if it is absent, and adds the
// columns a newer icpt records.
//
// ADD COLUMN IF NOT EXISTS rather than a fresh CREATE: the table may already
// have been created by an older tool, and dropping or recreating it would
// erase the record of everything ever applied.
func (m *Migrator) ensureLedger(ctx context.Context) error {
	for _, stmt := range m.dialect.LedgerDDL(m.table) {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: preparing the ledger: %w", err)
		}
	}
	m.syncIDSequence(ctx)
	return nil
}

// validLedgerTable accepts only a simple SQL identifier. LedgerTable is
// configuration, but validating it here keeps identifier interpolation safe
// even when configuration is supplied by an untrusted deployment environment.
func validLedgerTable(table string) bool {
	if table == "" || (table[0] < 'A' || table[0] > 'Z') &&
		(table[0] < 'a' || table[0] > 'z') && table[0] != '_' {
		return false
	}
	for i := 1; i < len(table); i++ {
		c := table[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') &&
			(c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// ledgerSQL substitutes the validated, dialect-quoted ledger identifier into
// a query template. SQL parameters cannot represent identifiers, so this is
// the one intentionally dynamic boundary; values remain ? parameters.
func (m *Migrator) ledgerSQL(query string) string {
	return strings.ReplaceAll(query, "__LEDGER_TABLE__", m.dialect.QuoteIdent(m.table)) // NOSONAR: table is validated by validLedgerTable.
}

// syncIDSequence resets the ledger's id sequence to MAX(id) on PostgreSQL.
//
// A pg_dump custom-format restore copies the rows without advancing the
// sequence, so the next insert collides on the primary key — which surfaces
// as a migration failing for a reason that has nothing to do with its SQL.
// Idempotent, cheap, and a no-op elsewhere or when id has no owned sequence.
func (m *Migrator) syncIDSequence(ctx context.Context) {
	if m.dialect.Name() != "postgres" {
		return
	}
	_, err := m.db.ExecContext(ctx, m.ledgerSQL(`
		SELECT setval(
		    pg_get_serial_sequence($1, 'id'),
		    GREATEST(COALESCE((SELECT MAX(id) FROM __LEDGER_TABLE__), 0), 1),
		    (SELECT COUNT(*) > 0 FROM __LEDGER_TABLE__)
		)
		WHERE pg_get_serial_sequence($1, 'id') IS NOT NULL`), m.table)
	if err != nil {
		m.logf("note: could not sync the %s id sequence: %v", m.table, err)
	}
}

// appliedRows reads the successful ledger rows, keyed by version.
func (m *Migrator) appliedRows(ctx context.Context) (map[string]Applied, error) {
	rows, err := m.db.QueryContext(ctx,
		m.ledgerSQL(`SELECT version, name, status, COALESCE(up_script,''), COALESCE(down_script,'')
		   FROM __LEDGER_TABLE__
		  WHERE status = 'success'
		  ORDER BY version`))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]Applied{}
	for rows.Next() {
		var a Applied
		if err := rows.Scan(&a.Version, &a.Name, &a.Status, &a.Up, &a.Down); err != nil {
			return nil, err
		}
		out[a.Version] = a
	}
	return out, rows.Err()
}

// Status reports every migration's position, on disk and in the ledger.
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	if err := m.ensureLedger(ctx); err != nil {
		return nil, err
	}
	files, err := Load(m.dir)
	if err != nil {
		return nil, err
	}
	applied, err := m.appliedRows(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Status, 0, len(files))
	onDisk := map[string]bool{}
	for _, f := range files {
		onDisk[f.Version] = true
		s := Status{Version: f.Version, Name: f.Name}
		if a, ok := applied[f.Version]; ok {
			s.Applied, s.StatusText = true, a.Status
			// Comparing the stored script with the file is the only way to
			// notice that an applied migration was edited afterwards — a
			// ledger that no longer describes a rebuildable database.
			s.Drifted = normalise(a.Up) != normalise(f.Up)
		}
		out = append(out, s)
	}
	for version, a := range applied {
		if !onDisk[version] {
			out = append(out, Status{
				Version: version, Name: a.Name, Applied: true, Missing: true,
				StatusText: a.Status,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func normalise(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }

// lock takes the dialect's advisory lock for the run, on a dedicated
// connection held until release.
func (m *Migrator) lock(ctx context.Context) (release func(), err error) {
	if m.noLock {
		return func() {}, nil
	}
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	unlock, err := m.dialect.Lock(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate: taking the advisory lock: %w", err)
	}
	return func() {
		// Unlock on a fresh context: the run's context may be cancelled, and
		// a lock left held blocks the next deploy until the session ends.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
		defer cancel()
		unlock(uctx)
		_ = conn.Close()
	}, nil
}

// UpOptions configures Up.
type UpOptions struct {
	// To stops after this version. Empty applies everything pending.
	To string
	// DryRun prints what would run and changes nothing.
	DryRun bool
	// AllowDrift proceeds although an applied migration's file has changed.
	// Off by default: drift usually means somebody edited an applied
	// migration, which leaves the ledger unable to rebuild the database.
	AllowDrift bool
}

// Up applies every pending migration in version order.
func (m *Migrator) Up(ctx context.Context, opt UpOptions) (int, error) {
	if err := m.ensureLedger(ctx); err != nil {
		return 0, err
	}
	files, err := Load(m.dir)
	if err != nil {
		return 0, err
	}

	release, err := m.lock(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	// Read the ledger AFTER taking the lock: between an unlocked read and
	// the first apply, another migrator could have applied everything.
	applied, err := m.appliedRows(ctx)
	if err != nil {
		return 0, err
	}

	if !opt.AllowDrift {
		var drifted []string
		for _, f := range files {
			if a, ok := applied[f.Version]; ok && normalise(a.Up) != normalise(f.Up) {
				drifted = append(drifted, f.Version+"_"+f.Name)
			}
		}
		if len(drifted) > 0 {
			return 0, fmt.Errorf(
				"migrate: these applied migrations differ from their files: %s\n"+
					"An applied migration must never be edited — the database was built by what ran, "+
					"not by what is on disk now. Write a new migration, or pass -allow-drift if you "+
					"have reconciled the ledger deliberately",
				strings.Join(drifted, ", "))
		}
	}

	count := 0
	for _, f := range files {
		if _, done := applied[f.Version]; done {
			continue
		}
		if opt.To != "" && f.Version > opt.To {
			break
		}
		if opt.DryRun {
			m.logf("would apply %s_%s", f.Version, f.Name)
			count++
			continue
		}
		if err := m.applyOne(ctx, f); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (m *Migrator) applyOne(ctx context.Context, f File) error {
	if m.dialect.TransactionalDDL() {
		return m.applyInTx(ctx, f)
	}
	return m.applyWithoutTx(ctx, f)
}

// applyInTx runs the migration and records it in ONE transaction, so a
// failure leaves neither the schema change nor the ledger row and a re-run is
// a clean retry.
func (m *Migrator) applyInTx(ctx context.Context, f File) error {
	start := time.Now()
	m.logf("applying %s_%s", f.Version, f.Name)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, f.Up); err != nil {
		_ = tx.Rollback()
		m.recordFailure(ctx, f, err, time.Since(start))
		return fmt.Errorf("migrate: %s_%s failed (rolled back): %w", f.Version, f.Name, err)
	}
	// Clear any prior row for this version — a previous 'rollback' or
	// 'failed' attempt — so the success row is the one a reader finds.
	clear := rebind(m.dialect, m.ledgerSQL(`DELETE FROM __LEDGER_TABLE__ WHERE version = ?`))
	if _, err := tx.ExecContext(ctx, clear, f.Version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate: clearing the prior log for %s_%s: %w", f.Version, f.Name, err)
	}
	insert := rebind(m.dialect, m.ledgerSQL(`INSERT INTO __LEDGER_TABLE__
			 (version, name, status, up_script, down_script, message, duration_ms)
			  VALUES (?, ?, 'success', ?, ?, 'Applied successfully', ?)`))
	if _, err := tx.ExecContext(ctx, insert,
		f.Version, f.Name, f.Up, f.Down, time.Since(start).Milliseconds()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate: recording %s_%s: %w", f.Version, f.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: committing %s_%s: %w", f.Version, f.Name, err)
	}
	m.logf("applied  %s_%s (%dms)", f.Version, f.Name, time.Since(start).Milliseconds())
	return nil
}

// applyWithoutTx is the MySQL path, where DDL commits implicitly and a
// migration cannot be rolled back.
//
// The compensation is a "running" row written BEFORE the work. If the process
// dies mid-migration, that row survives and the next run reports a
// half-applied migration instead of silently trying it again on a schema that
// has already half-changed. It is evidence, not atomicity — there is no
// atomicity to be had here.
func (m *Migrator) applyWithoutTx(ctx context.Context, f File) error {
	start := time.Now()
	m.logf("applying %s_%s (no transactional DDL on %s; this cannot be rolled back)",
		f.Version, f.Name, m.dialect.Name())

	if _, err := m.exec(ctx, m.ledgerSQL(`DELETE FROM __LEDGER_TABLE__ WHERE version = ?`), f.Version); err != nil {
		return fmt.Errorf("migrate: clearing the prior log for %s_%s: %w", f.Version, f.Name, err)
	}
	res, err := m.exec(ctx, m.ledgerSQL(`INSERT INTO __LEDGER_TABLE__
			 (version, name, status, up_script, down_script)
			  VALUES (?, ?, 'running', ?, ?)`),
		f.Version, f.Name, f.Up, f.Down)
	if err != nil {
		return fmt.Errorf("migrate: claiming %s_%s: %w", f.Version, f.Name, err)
	}
	id, _ := res.LastInsertId()

	if _, err := m.db.ExecContext(ctx, f.Up); err != nil {
		_, _ = m.exec(context.WithoutCancel(ctx), m.ledgerSQL(`UPDATE __LEDGER_TABLE__
				 SET status = 'failed', error = ?, message = ?, duration_ms = ?
				  WHERE id = ?`),
			err.Error(), "Failed; schema may be partly changed",
			time.Since(start).Milliseconds(), id)
		return fmt.Errorf("migrate: %s_%s failed and CANNOT be rolled back on %s — "+
			"inspect the schema before retrying: %w", f.Version, f.Name, m.dialect.Name(), err)
	}

	if _, err := m.exec(ctx, m.ledgerSQL(`UPDATE __LEDGER_TABLE__
			 SET status = 'success', message = 'Applied successfully', duration_ms = ?
			  WHERE id = ?`),
		time.Since(start).Milliseconds(), id); err != nil {
		return fmt.Errorf("migrate: %s_%s applied but could not be recorded — "+
			"the ledger now understates the schema: %w", f.Version, f.Name, err)
	}
	m.logf("applied  %s_%s (%dms)", f.Version, f.Name, time.Since(start).Milliseconds())
	return nil
}

// recordFailure writes a failed row OUTSIDE the rolled-back transaction, so
// there is evidence of the attempt. A failure to record a failure is itself
// only logged: the real error is the one worth returning.
func (m *Migrator) recordFailure(ctx context.Context, f File, cause error, took time.Duration) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_, err := m.exec(rctx, m.ledgerSQL(`INSERT INTO __LEDGER_TABLE__
			 (version, name, status, up_script, down_script, message, duration_ms, error)
			  VALUES (?, ?, 'failed', ?, ?, 'Rolled back', ?, ?)`),
		f.Version, f.Name, f.Up, f.Down, took.Milliseconds(), cause.Error())
	if err != nil {
		m.logf("note: the failure of %s_%s could not be recorded: %v", f.Version, f.Name, err)
	}
}

// DownOptions configures Down.
type DownOptions struct {
	// Steps is how many migrations to reverse, most recent first.
	Steps int
	// DryRun prints what would run and changes nothing.
	DryRun bool
}

// Down reverses the most recently applied migrations.
//
// It runs the down script STORED IN THE LEDGER, not the file on disk. The
// database was built by what ran at the time; the checked-out file may belong
// to a later version of that migration, or to a squash, and running it would
// reverse something that was never applied.
func (m *Migrator) Down(ctx context.Context, opt DownOptions) (int, error) {
	if opt.Steps <= 0 {
		return 0, errors.New("migrate: -steps must be at least 1")
	}
	if err := m.ensureLedger(ctx); err != nil {
		return 0, err
	}

	release, err := m.lock(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	steps, err := m.lastApplied(ctx, opt.Steps)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, s := range steps {
		if strings.TrimSpace(s.down) == "" {
			return count, fmt.Errorf(
				"migrate: %s_%s has no stored down script; refusing to guess", s.version, s.name)
		}
		if opt.DryRun {
			m.logf("would reverse %s_%s", s.version, s.name)
			count++
			continue
		}
		m.logf("reversing %s_%s", s.version, s.name)

		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return count, err
		}
		if _, err := tx.ExecContext(ctx, s.down); err != nil {
			_ = tx.Rollback()
			return count, fmt.Errorf("migrate: reversing %s_%s failed (rolled back): %w",
				s.version, s.name, err)
		}
		// Mark, do not delete. The stored up and down scripts are the only
		// record of what this database once was; a rollback that erases them
		// leaves a later investigation with nothing. The version counts as
		// pending again, so the next run re-applies it.
		if _, err := tx.ExecContext(ctx, rebind(m.dialect, m.ledgerSQL(`UPDATE __LEDGER_TABLE__
				 SET status = 'rollback', message = 'Rolled back'
				  WHERE version = ? AND status = 'success'`)), s.version); err != nil {
			_ = tx.Rollback()
			return count, err
		}
		if err := tx.Commit(); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// step is one migration to reverse, as recorded in the ledger.
type step struct{ version, name, down string }

// lastApplied reads the most recently applied migrations, newest first.
func (m *Migrator) lastApplied(ctx context.Context, limit int) ([]step, error) {
	rows, err := m.db.QueryContext(ctx, rebind(m.dialect, m.ledgerSQL(`SELECT version, name, COALESCE(down_script,'')
		   FROM __LEDGER_TABLE__
		  WHERE status = 'success'
		  ORDER BY version DESC
		  LIMIT ?`)), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []step
	for rows.Next() {
		var s step
		if err := rows.Scan(&s.version, &s.name, &s.down); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Create writes an empty up/down pair with the next version number.
func Create(dir, name string) (up, down string, err error) {
	if strings.TrimSpace(name) == "" {
		return "", "", errors.New("migrate: a migration needs a name")
	}
	slug := slugify(name)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	next, err := nextVersion(dir)
	if err != nil {
		return "", "", err
	}

	up = filepath.Join(dir, next+"_"+slug+".up.sql")
	down = filepath.Join(dir, next+"_"+slug+".down.sql")

	header := func(direction string) string {
		return fmt.Sprintf("-- %s_%s.%s.sql\n--\n"+
			"-- Write the %s here. It runs in ONE transaction, so either all of it\n"+
			"-- lands or none of it does.\n--\n"+
			"-- Once this has been applied anywhere, do not edit it: the ledger stores\n"+
			"-- what ran, and a file that no longer matches cannot rebuild the database.\n\n",
			next, slug, direction, direction)
	}
	if err := os.WriteFile(up, []byte(header("up")), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(down, []byte(header("down")), 0o644); err != nil {
		return "", "", err
	}
	return up, down, nil
}

// nextVersion returns the highest version in dir plus one, zero-padded to six
// digits to match the existing files.
func nextVersion(dir string) (string, error) {
	ups, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return "", err
	}
	highest := 0
	width := 6
	for _, up := range ups {
		stem := strings.TrimSuffix(filepath.Base(up), ".up.sql")
		version, _, ok := strings.Cut(stem, "_")
		if !ok {
			continue
		}
		width = max(width, len(version))
		n := 0
		if _, err := fmt.Sscanf(version, "%d", &n); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%0*d", width, highest+1), nil
}

func slugify(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
