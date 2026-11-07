// Package lint checks that a codebase follows the inceptools layering rules.
//
// A convention nothing enforces is a convention that decays. These are the
// rules this framework's shape depends on, each stated with the failure it
// prevents — because a lint rule whose reason nobody remembers gets disabled
// the first time it is inconvenient.
package lint

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/IncepTools/go/internal/project"
)

// Severity of a finding.
type Severity string

const (
	// Error breaks a rule the architecture depends on. Fail the build.
	Error Severity = "error"
	// Warn is a smell: probably wrong, occasionally justified.
	Warn Severity = "warn"
	// Off suppresses a rule. Set it in icpt.json under "rules".
	Off Severity = "off"
)

// Rules are the rule names, so a configuration file can be checked against
// them rather than silently ignoring a typo.
var Rules = []string{
	"service-imports-http",
	"controller-imports-db",
	"dto-imports-db",
	"entity-imports-layer",
	"cross-module-internals",
	"module-has-no-module-file",
	"app-has-no-root-file",
}

// Finding is one rule violation.
type Finding struct {
	Severity Severity
	Rule     string
	Path     string
	Line     int
	Message  string
	// Why explains the failure the rule prevents. Printed with the finding,
	// because "layering violation" teaches nobody anything.
	Why string
}

func (f Finding) String() string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	s := fmt.Sprintf("%-5s %-22s %s\n        %s", f.Severity, f.Rule, loc, f.Message)
	if f.Why != "" {
		s += "\n        why: " + f.Why
	}
	return s
}

// Options configures a run.
type Options struct {
	// Config carries the repository's conventions — which directories are
	// which layer, where the domains live, which rules are on. Loaded from
	// icpt.json; nil means the defaults.
	Config *project.Config
	// Root overrides the configuration's root.
	Root string
	// AppsDir overrides the configuration's domains directory.
	AppsDir string
	// Skip are extra path fragments to ignore.
	Skip []string
}

// Run checks the tree and returns findings, most severe first.
func Run(opt Options) ([]Finding, error) {
	cfg := opt.Config
	if cfg == nil {
		cfg = project.Default()
		if opt.Root != "" {
			cfg.Root = opt.Root
		}
	}
	if opt.Root == "" {
		opt.Root = cfg.Root
	}
	if opt.Root == "" {
		opt.Root = "."
	}
	if opt.AppsDir == "" {
		opt.AppsDir = cfg.Layout.Apps
	}
	opt.Skip = append(append([]string(nil), opt.Skip...), cfg.Skip...)
	appsPath := filepath.Join(opt.Root, opt.AppsDir)
	if _, err := os.Stat(appsPath); err != nil {
		return nil, fmt.Errorf("lint: %s does not exist; run this from the service root or pass -apps", appsPath)
	}

	var findings []Finding
	fset := token.NewFileSet()

	err := filepath.WalkDir(appsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip(path, opt.Skip) || d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			// Report it and keep walking rather than aborting the run: one
			// unparsable file must not hide every finding in the rest of the
			// tree, which is exactly when you most want the other findings.
			findings = append(findings, Finding{
				Severity: Error, Rule: "parse", Path: rel(opt.Root, path),
				Message: perr.Error(),
			})
			return nil //nolint:nilerr // reported as a finding, not as a walk failure
		}

		layer := cfg.LayerOf(filepath.Base(filepath.Dir(path)))
		for _, imp := range file.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			line := fset.Position(imp.Pos()).Line
			findings = append(findings, checkImport(opt.Root, opt.AppsDir, path, layer, p, line)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	findings = append(findings, checkModuleLayout(cfg, opt, appsPath)...)

	// Apply the repository's severity overrides, dropping anything set off.
	kept := findings[:0]
	for _, f := range findings {
		switch Severity(cfg.Rules[f.Rule]) {
		case Off:
			continue
		case Error:
			f.Severity = Error
		case Warn:
			f.Severity = Warn
		}
		kept = append(kept, f)
	}
	findings = kept

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity == Error
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

func checkImport(root, appsDir, path, layer, imported string, line int) []Finding {
	var out []Finding
	p := rel(root, path)

	isHTTP := strings.Contains(imported, "gofiber/fiber") ||
		strings.HasSuffix(imported, "/web/response") ||
		strings.HasSuffix(imported, "/web/binding") ||
		strings.HasSuffix(imported, "/web/pipe")
	isDB := strings.HasPrefix(imported, "gorm.io/") || strings.Contains(imported, "database/sql")

	switch layer {
	case project.LayerService:
		if isHTTP {
			out = append(out, Finding{
				Severity: Error, Rule: "service-imports-http", Path: p, Line: line,
				Message: "a service imports " + imported,
				Why: "a service that speaks HTTP cannot be called from a scheduled job, a CLI or a " +
					"queue consumer without faking a request. Return an *errs.Error and let the " +
					"controller decide the status.",
			})
		}
	case project.LayerController:
		if isDB {
			out = append(out, Finding{
				Severity: Error, Rule: "controller-imports-db", Path: p, Line: line,
				Message: "a controller imports " + imported,
				Why: "a query in a controller is a query no other caller can reuse and no test can " +
					"exercise without HTTP. It is also where tenant scoping gets forgotten.",
			})
		}
	case project.LayerEntity:
		if isHTTP || isDB && strings.Contains(imported, "database/sql") {
			out = append(out, Finding{
				Severity: Warn, Rule: "entity-imports-layer", Path: p, Line: line,
				Message: "an entity imports " + imported,
				Why:     "entities are data. Importing a transport or a driver makes them unusable from anywhere else.",
			})
		}
	case project.LayerDTO:
		if isDB {
			out = append(out, Finding{
				Severity: Error, Rule: "dto-imports-db", Path: p, Line: line,
				Message: "a DTO imports " + imported,
				Why: "a DTO that knows about the database is an entity with a different name, and " +
					"returning it leaks the schema into the public API.",
			})
		}
	}

	// A module reaching into another module's internals.
	if from, ok := moduleOf(path, appsDir); ok {
		if to, ok := moduleOfImport(imported, appsDir); ok && to != from && isInternalLayer(imported) {
			out = append(out, Finding{
				Severity: Warn, Rule: "cross-module-internals", Path: p, Line: line,
				Message: fmt.Sprintf("module %s imports %s's internals (%s)", from, to, imported),
				Why: "two modules coupled through each other's service or entity packages cannot be " +
					"changed or moved independently. Depend on an interface the other module publishes, " +
					"or move the shared piece up.",
			})
		}
	}
	return out
}

func isInternalLayer(imported string) bool {
	for _, l := range []string{"/service", "/entity", "/controller", "/repository"} {
		if strings.Contains(imported, l) {
			return true
		}
	}
	return false
}

// moduleOf returns "app/module" for a file under <appsDir>/<app>/<module>/...
func moduleOf(path, appsDir string) (string, bool) {
	return segmentAfter(strings.Split(filepath.ToSlash(path), "/"), appsDir)
}

func moduleOfImport(imported, appsDir string) (string, bool) {
	return segmentAfter(strings.Split(imported, "/"), appsDir)
}

func segmentAfter(parts []string, appsDir string) (string, bool) {
	appsDir = strings.Trim(filepath.ToSlash(appsDir), "/")
	for i, p := range parts {
		if p == appsDir && i+2 < len(parts) {
			return parts[i+1] + "/" + parts[i+2], true
		}
	}
	return "", false
}

// checkModuleLayout verifies each module directory declares itself.
func checkModuleLayout(cfg *project.Config, opt Options, appsPath string) []Finding {
	var out []Finding

	apps, err := os.ReadDir(appsPath)
	if err != nil {
		return out
	}
	for _, app := range apps {
		if !app.IsDir() || skip(filepath.Join(appsPath, app.Name()), opt.Skip) {
			continue
		}
		appDir := filepath.Join(appsPath, app.Name())

		if !hasGoFile(appDir) {
			out = append(out, Finding{
				Severity: Warn, Rule: "app-has-no-root-file", Path: rel(opt.Root, appDir),
				Message: "no Go file at the app root",
				Why: "an app should declare its modules in one place (an <app>_app.go with NewApp), " +
					"so the domain's surface is readable without opening every subdirectory.",
			})
		}

		modules, err := os.ReadDir(appDir)
		if err != nil {
			continue
		}
		for _, m := range modules {
			if !m.IsDir() || skip(filepath.Join(appDir, m.Name()), opt.Skip) {
				continue
			}
			modDir := filepath.Join(appDir, m.Name())
			// A layer directory at the module level belongs to the app
			// itself, not to a missing module.
			if cfg.IsLayerDir(m.Name()) {
				continue
			}
			if !hasGoFile(modDir) {
				out = append(out, Finding{
					Severity: Error, Rule: "module-has-no-module-file", Path: rel(opt.Root, modDir),
					Message: "no Go file at the module root",
					Why: "every module needs a module file declaring its name, controllers and " +
						"entities. Without one nothing mounts it and nothing knows it owns its tables.",
				})
			}
		}
	}
	return out
}

func hasGoFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

func skip(path string, fragments []string) bool {
	for _, f := range fragments {
		if f != "" && strings.Contains(filepath.ToSlash(path), f) {
			return true
		}
	}
	return false
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
