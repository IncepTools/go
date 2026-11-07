package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IncepTools/go/internal/migrate"
	"github.com/IncepTools/go/internal/project"
)

// openTarget reads a target's DSN from the environment and connects.
//
// cliDSN, when given, overrides the environment — the one case where a DSN
// may be typed in: a one-off run against a database that has no variable.
// The configuration file never holds one, because a connection string holds
// a password.
func openTarget(ctx context.Context, t project.Target, cliDSN string) (*migrate.Migrator, error) {
	dsn := cliDSN
	if dsn == "" {
		dsn = os.Getenv(t.EnvVariable)
	}
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf(
			"no database URL for %q: set %s, or pass the URL as an argument",
			t.Name, t.EnvVariable)
	}
	return migrate.Open(ctx, migrate.Options{
		Name:        t.Name,
		DSN:         dsn,
		Dir:         t.MigrationPath,
		Dialect:     t.Dialect,
		LedgerTable: t.LedgerTable,
		NoLock:      t.NoLock,
	})
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbName := fs.String("db", "", "which database in "+project.FileName+" (default: all of them)")
	to := fs.String("to", "", "stop after this version")
	dry := fs.Bool("n", false, "dry run: print what would be applied")
	allowDrift := fs.Bool("allow-drift", false, "proceed although an applied migration's file has changed")
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	// With no -db, every configured database is migrated in turn. Migrating
	// is additive and idempotent, so fanning out is safe; rolling back is
	// neither, which is why `down` refuses to.
	targets, err := cfg.SelectTargets(*dbName)
	if err != nil {
		return err
	}

	cliDSN := ""
	if fs.NArg() >= 1 {
		cliDSN = fs.Arg(0)
	}
	if cliDSN != "" && len(targets) > 1 {
		return fmt.Errorf("a database URL is ambiguous with %d databases configured; "+
			"name one with -db", len(targets))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	for _, t := range targets {
		if len(targets) > 1 || t.Name != project.DefaultTargetName {
			fmt.Printf("\n== %s (%s)\n", t.Name, dialectLabel(t))
		}
		if err := migrateOne(ctx, t, cliDSN, *to, *dry, *allowDrift); err != nil {
			return err
		}
	}
	return nil
}

func migrateOne(ctx context.Context, t project.Target, cliDSN, to string, dry, allowDrift bool) error {
	m, err := openTarget(ctx, t, cliDSN)
	if err != nil {
		return err
	}
	defer m.Close() //nolint:errcheck
	m.Out = func(format string, a ...any) { fmt.Printf("  "+format+"\n", a...) }

	applied, err := m.Up(ctx, migrate.UpOptions{To: to, DryRun: dry, AllowDrift: allowDrift})
	if err != nil {
		return err
	}
	switch {
	case applied == 0:
		fmt.Printf("  %s: already up to date\n", t.Name)
	case dry:
		fmt.Printf("  %s: %d migration(s) would be applied\n", t.Name, applied)
	default:
		fmt.Printf("  %s: %d migration(s) applied\n", t.Name, applied)
	}
	return nil
}

func dialectLabel(t project.Target) string {
	if t.Dialect == "" {
		return "postgres"
	}
	return t.Dialect
}

func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	dbName := fs.String("db", "", "which database in "+project.FileName)
	steps := fs.Int("steps", 1, "how many migrations to reverse")
	dry := fs.Bool("n", false, "dry run: print what would be reversed")
	yes := fs.Bool("y", false, "do not ask for confirmation")
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	// Never fan out. A rollback across every database because -db was
	// omitted is unrecoverable.
	target, err := cfg.SelectOne(*dbName)
	if err != nil {
		return err
	}
	name := target.Name

	cliDSN := ""
	if fs.NArg() >= 1 {
		cliDSN = fs.Arg(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	m, err := openTarget(ctx, target, cliDSN)
	if err != nil {
		return err
	}
	defer m.Close() //nolint:errcheck
	m.Out = func(format string, a ...any) { fmt.Printf(format+"\n", a...) }

	// A rollback drops data. Confirming is cheap; `icpt down` typed in the
	// wrong terminal is not.
	if !*dry && !*yes {
		fmt.Printf("Reverse %d migration(s) on %q? This can destroy data. Type yes to continue: ", *steps, name)
		var answer string
		_, _ = fmt.Scanln(&answer)
		if strings.TrimSpace(strings.ToLower(answer)) != "yes" {
			return errors.New("cancelled")
		}
	}

	n, err := m.Down(ctx, migrate.DownOptions{Steps: *steps, DryRun: *dry})
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d migration(s) reversed\n", name, n)
	return nil
}

func cmdMigrateStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dbName := fs.String("db", "", "which database in "+project.FileName)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	target, err := cfg.SelectOne(*dbName)
	if err != nil {
		return err
	}
	name := target.Name

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	m, err := openTarget(ctx, target, "")
	if err != nil {
		return err
	}
	defer m.Close() //nolint:errcheck

	rows, err := m.Status(ctx)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "VERSION\tNAME\tSTATE\tAPPLIED\n")
	pending, problems := 0, 0
	for _, r := range rows {
		state := "pending"
		switch {
		case r.Missing:
			state = "MISSING FILE"
			problems++
		case r.Drifted:
			state = "DRIFTED"
			problems++
		case r.Applied:
			state = "applied"
		default:
			pending++
		}
		when := ""
		if r.Applied {
			when = r.AppliedAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Version, r.Name, state, when)
	}
	_ = w.Flush()

	fmt.Printf("\n%s: %d pending, %d problem(s)\n", name, pending, problems)
	if problems > 0 {
		fmt.Println(
			"\nDRIFTED means an applied migration's file was edited afterwards, and\n" +
				"MISSING FILE means one was applied and then deleted. Either way the ledger\n" +
				"no longer describes a database this checkout can rebuild.")
	}
	return nil
}

func cmdCreateMigration(args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	dbName := fs.String("db", "", "which database in "+project.FileName)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	target, err := cfg.SelectOne(*dbName)
	if err != nil {
		return err
	}

	// The name may be typed as arguments or prompted for, so that both
	// `icpt create add user email` and a bare `icpt create` work.
	name := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(name) == "" {
		fmt.Print("Migration name (e.g. add user email): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		name = strings.TrimSpace(line)
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("a migration needs a name")
	}

	up, down, err := migrate.Create(target.MigrationPath, name)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\ncreated %s\n", rel(up), rel(down))
	return nil
}

func rel(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}
