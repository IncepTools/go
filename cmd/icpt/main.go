// Command icpt is the inceptools CLI: scaffolding, structure rules and SQL
// migrations, all driven by icpt.json.
//
//	icpt init                       write icpt.json
//	icpt new <service>              scaffold a new service
//	icpt generate module <app/mod>  scaffold a resource module
//	icpt lint                       check the layering and layout rules
//	icpt routes                     print a service's route table
//	icpt doctor                     check this project's setup
//
//	icpt create <name>              new migration pair
//	icpt migrate                    apply pending migrations
//	icpt status                     what is applied, pending or drifted
//	icpt down -steps N              reverse the last N migrations
//
// Nothing destructive happens without either -y or a typed confirmation, and
// `-n` is a dry run everywhere it appears.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/internal/lint"
	"github.com/IncepTools/go/internal/project"
	"github.com/IncepTools/go/internal/scaffold"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "icpt: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command given")
	}
	switch args[0] {
	case "new":
		return cmdNew(args[1:])
	case "generate", "g":
		return cmdGenerate(args[1:])
	case "lint":
		return cmdLint(args[1:])
	case "migrate", "up":
		return cmdMigrate(args[1:])
	case "down":
		return cmdDown(args[1:])
	case "create":
		return cmdCreateMigration(args[1:])
	case "status":
		return cmdMigrateStatus(args[1:])
	case "init":
		return cmdInit(args[1:])
	case "routes":
		return cmdRoutes(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "version", "-v", "--version":
		printVersion()
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// Build information, set by the release build:
//
//	-ldflags "-X main.version=v1.2.3 -X main.commit=$(git rev-parse HEAD)"
//
// They default to "dev" so a `go install` from source is honest about being
// an untagged build rather than claiming a version it does not have.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func printVersion() {
	fmt.Printf("icpt %s\n", version)
	fmt.Printf("  framework %s\n", inceptools.Version)
	if commit != "" {
		fmt.Printf("  commit    %s\n", commit)
	}
	if date != "" {
		fmt.Printf("  built     %s\n", date)
	}
	fmt.Printf("  go        %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	// When ldflags were not set, the module's own build info still knows
	// what version `go install module@v1.2.3` fetched.
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			fmt.Printf("  module    %s\n", bi.Main.Version)
		}
	}
}

func usage() {
	fmt.Print(`icpt — the inceptools CLI

Project
  icpt init [-apps apps] [-tests colocated|mirror]
      Write icpt.json, preserving any keys another tool owns.

  icpt new <service> [-mod <go module path>]
      Scaffold a new service: main.go, config, Makefile, .env.example.

  icpt generate app <app>
  icpt generate module <app>/<module>
  icpt generate resource <app>/<module>      (alias for module)
      Scaffold a domain or a full CRUD module. Never overwrites.

  icpt lint [-strict]
      Check the layering rules: services free of HTTP, controllers free of
      SQL, DTOs free of the database, modules not reaching into each other.
      Directory names and rule severities come from icpt.json.

  icpt routes [-json]
      Print the route table a service exposes, read from a routes dump.

  icpt doctor
      Check this project's setup.

Migrations (configured under "databases" in icpt.json)
  icpt create <name> [-db <database>]
      Write the next <version>_<name>.up.sql / .down.sql pair.

  icpt migrate [-db <database>] [-to <version>] [-n] [-allow-drift]
      Apply every pending migration, in order, one transaction each, under
      an advisory lock.

  icpt status [-db <database>]
      What is applied, pending, drifted or missing.

  icpt down -steps <n> [-db <database>] [-n] [-y]
      Reverse the last n migrations, running the DOWN SCRIPT STORED IN THE
      LEDGER rather than the file on disk.

  icpt version

Common flags
  -n            dry run
  -root <dir>   project root (default ".")
  -db <name>    which database in icpt.json; required when there are several
`)
}

// reorder moves positional arguments after the flags.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `icpt generate module catalog/products -n` would silently ignore -n. Every CLI
// a developer has used accepts flags in either position, and a flag that is
// quietly dropped is worse than one that errors.
func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if n, v, ok := strings.Cut(name, "="); ok {
			_, _ = n, v
			continue // -flag=value carries its own value
		}
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		// A boolean flag takes no following value; anything else does.
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	modPath := fs.String("mod", "", "Go module path for the new service")
	dry := fs.Bool("n", false, "dry run")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: icpt new <service> [-mod <go module path>]")
	}
	service := fs.Arg(0)
	if *modPath == "" {
		*modPath = "example.com/" + service
	}

	d := scaffold.Data{GoModule: *modPath, AppsDir: "apps", Service: service}
	files, err := scaffold.Service(d)
	if err != nil {
		return err
	}
	if _, err := os.Stat(service); err == nil {
		return fmt.Errorf("%s already exists", service)
	}
	written, err := scaffold.Write(service, files, *dry)
	if err != nil {
		return err
	}
	report(service, written, *dry)
	if !*dry {
		fmt.Printf("\nNext:\n  cd %s\n  go mod init %s\n  go get github.com/IncepTools/go\n  icpt generate module <app>/<module>\n",
			service, *modPath)
	}
	return nil
}

func cmdGenerate(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: icpt generate <app|module|resource> <name>")
	}
	kind := args[0]

	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	modPath := fs.String("mod", "", "Go module path (read from go.mod or icpt.json when omitted)")
	appsDirFlag := fs.String("apps", "", "directory holding the domains")
	root := fs.String("root", ".", "project root")
	dry := fs.Bool("n", false, "dry run")
	if err := fs.Parse(reorder(fs, args[1:])); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: icpt generate %s <name>", kind)
	}
	target := fs.Arg(0)

	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	appsDir := &cfg.Layout.Apps
	if *appsDirFlag != "" {
		appsDir = appsDirFlag
	}

	goModule := *modPath
	if goModule == "" {
		goModule = cfg.Module
	}
	if goModule == "" {
		goModule, err = readGoModule(*root)
		if err != nil {
			return err
		}
	}

	switch kind {
	case "app":
		d := scaffold.NewData(goModule, *appsDir, target, target).WithConfig(cfg)
		f, err := scaffold.AppFile(d)
		if err != nil {
			return err
		}
		written, err := scaffold.Write(cfg.Root, []scaffold.File{f}, *dry)
		if err != nil {
			return err
		}
		report(cfg.Root, written, *dry)
		return nil

	case "module", "resource":
		app, mod, ok := strings.Cut(target, "/")
		if !ok || app == "" || mod == "" {
			return fmt.Errorf("expected <app>/<module>, got %q", target)
		}
		d := scaffold.NewData(goModule, *appsDir, app, mod).WithConfig(cfg)
		files, err := scaffold.Module(d)
		if err != nil {
			return err
		}
		// Create the app aggregator too, when the domain is new. A module
		// with nothing to mount it is dead code that compiles.
		appFile := filepath.Join(*appsDir, app, cfg.AppFileName(app))
		if _, err := os.Stat(filepath.Join(cfg.Root, appFile)); errors.Is(err, os.ErrNotExist) {
			f, aerr := scaffold.AppFile(d)
			if aerr != nil {
				return aerr
			}
			files = append(files, f)
		}
		written, err := scaffold.Write(cfg.Root, files, *dry)
		if err != nil {
			return err
		}
		report(cfg.Root, written, *dry)
		if !*dry {
			fmt.Printf("\nNext:\n  add %s.New(s) to %s\n  write the migration for the %s table\n",
				d.Package, appFile, d.Package)
		}
		return nil

	default:
		return fmt.Errorf("unknown generator %q; expected app, module or resource", kind)
	}
}

func cmdLint(args []string) error {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	root := fs.String("root", ".", "project root")
	appsDir := fs.String("apps", "", "directory holding the domains")
	warnAsError := fs.Bool("strict", false, "treat warnings as errors")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	findings, err := lint.Run(lint.Options{Config: cfg, AppsDir: *appsDir})
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		fmt.Println("icpt lint: no findings")
		return nil
	}

	var errorCount, warnCount int
	for _, f := range findings {
		fmt.Println(f.String())
		fmt.Println()
		if f.Severity == lint.Error {
			errorCount++
		} else {
			warnCount++
		}
	}
	fmt.Printf("%d error(s), %d warning(s)\n", errorCount, warnCount)
	if errorCount > 0 || (*warnAsError && warnCount > 0) {
		return errors.New("lint failed")
	}
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ok := true
	check := func(label string, pass bool, hint string) {
		mark := "ok  "
		if !pass {
			mark, ok = "FAIL", false
		}
		fmt.Printf("%s  %s\n", mark, label)
		if !pass && hint != "" {
			fmt.Printf("      %s\n", hint)
		}
	}

	mod, err := readGoModule(*root)
	check("go.mod found", err == nil, "run this from the service root")
	if err == nil {
		fmt.Printf("      module %s\n", mod)
	}

	cfg, cerr := project.Load(*root)
	check(project.FileName+" is valid", cerr == nil, errText(cerr))
	if cerr == nil {
		if cfg.Found {
			dbs := "none"
			if cfg.DeclaresDatabases() {
				dbs = strings.Join(cfg.DatabaseNames(), " ")
			}
			fmt.Printf("      apps=%s tests=%s databases=[%s]\n",
				cfg.Layout.Apps, cfg.Layout.Tests, dbs)
		} else {
			fmt.Printf("      not present; using defaults (run: icpt init)\n")
		}
		// Only report on databases the project actually declares. Targets
		// always yields a synthetic "default" so that migrate works with
		// no configuration at all, and complaining about that one would
		// mean every library repository fails doctor.
		if cfg.DeclaresDatabases() {
			for _, t := range cfg.Targets() {
				_, set := os.LookupEnv(t.EnvVariable)
				check("DSN for "+t.Name+" ("+t.EnvVariable+")", set,
					"export "+t.EnvVariable+" before running icpt migrate")
				_, derr := os.Stat(t.MigrationPath)
				check("migrations for "+t.Name+" ("+t.Dialect+")", derr == nil,
					"expected "+t.MigrationPath+"; run: icpt create <name>")
			}
		} else {
			fmt.Printf("      no migration targets configured\n")
		}
		appsPath := cfg.AppsPath()
		_, statErr := os.Stat(appsPath)
		check(cfg.Layout.Apps+"/ exists", statErr == nil,
			"domains live in "+cfg.Layout.Apps+"/<app>/<module>; run: icpt generate module <app>/<module>")
	}

	raw, _ := os.ReadFile(filepath.Join(*root, "go.mod"))
	check("depends on inceptools", strings.Contains(string(raw), "github.com/IncepTools/go"),
		"run: go get github.com/IncepTools/go")

	_, gerr := exec.LookPath("go")
	check("go toolchain on PATH", gerr == nil, "install Go")

	if !ok {
		return errors.New("doctor found problems")
	}
	return nil
}

func report(root string, written []string, dry bool) {
	verb := "created"
	if dry {
		verb = "would create"
	}
	for _, w := range written {
		fmt.Printf("%s %s\n", verb, filepath.Join(root, w))
	}
}

// readGoModule reads the module path out of go.mod, so generated imports are
// right without the caller having to retype the path.
func readGoModule(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("no go.mod in %s: %w", root, err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if after, found := strings.CutPrefix(line, "module "); found {
			return strings.TrimSpace(after), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}
