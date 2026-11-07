// Package project loads icpt.json, the per-repository configuration for the
// icpt CLI.
//
// It exists because there is no one right layout. Some repositories keep
// tests beside the code, some mirror them under tests/; some call the data
// layer repository/, some repo/, some store/; some have one database, some
// have one per region. A tool that hard-codes one answer is a tool that has
// to be fought.
//
// So the conventions are configuration, and the defaults are the shape this
// framework recommends. A repository with no icpt.json gets those defaults
// and works; a repository with one gets its own.
//
// Unknown keys are PRESERVED on write. inceptools-cli's `dialect` and
// `databases` live in this same file, and a tool that rewrote the file
// without them would break migrations the first time somebody ran `icpt init`.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the configuration file this package reads.
const FileName = "icpt.json"

// DefaultTargetName is what the single-database form is called, matching
// inceptools-cli.
const DefaultTargetName = "default"

// Database is one migration target.
//
// These two fields are inceptools-cli's existing schema and are read, not
// invented: a repository already using that CLI needs no change to its file.
type Database struct {
	// EnvVariable names the environment variable holding the DSN. The DSN
	// itself is never stored in the file: a connection string holds a
	// password, and the repository is the wrong place for one.
	EnvVariable string `json:"env_variable"`
	// MigrationPath is the directory of .up.sql / .down.sql files, relative
	// to the repository root.
	MigrationPath string `json:"migration_path"`
	// Dialect overrides the top-level one for this target. A repository with
	// a Postgres primary and a SQLite cache, or two engines mid-migration,
	// sets it here.
	Dialect string `json:"dialect,omitempty"`
	// LedgerTable overrides the name of the applied-migrations table, for a
	// database that already has a schema_migrations belonging to something
	// else.
	LedgerTable string `json:"ledger_table,omitempty"`
	// NoLock skips the advisory lock. Setting it means two concurrent
	// deploys can both apply the same migration; only do it where the lock
	// genuinely cannot work.
	NoLock bool `json:"no_lock,omitempty"`
}

// DialectOf returns this target's dialect, falling back to the file's.
func (c *Config) DialectOf(db Database) string {
	if db.Dialect != "" {
		return db.Dialect
	}
	return c.Dialect
}

// Layout names the directories and files the tooling looks for.
type Layout struct {
	// Apps is where the domains live. Default "apps".
	Apps string `json:"apps,omitempty"`
	// Tests selects where generated tests go and where the linter expects
	// them: "colocated" (beside the code, the Go default) or "mirror" (under
	// TestsDir, mirroring the package path).
	Tests string `json:"tests,omitempty"`
	// TestsDir is the root for mirrored tests. Default "tests".
	TestsDir string `json:"tests_dir,omitempty"`
	// ModuleFile is the name of a module's declaration file. {module} is
	// replaced with the module's package name. Default "{module}_module.go".
	ModuleFile string `json:"module_file,omitempty"`
	// AppFile is the name of a domain's aggregator. {app} is replaced.
	// Default "{app}_app.go".
	AppFile string `json:"app_file,omitempty"`
	// Layers maps each logical layer to the directory names that count as
	// it. A repository that calls its data layer "store" adds that here and
	// the layering rules start applying to it.
	Layers map[string][]string `json:"layers,omitempty"`
}

// Generate configures the scaffolder.
type Generate struct {
	// Repository emits a repository layer. Turn it off for a service whose
	// modules talk to something other than a database.
	Repository *bool `json:"repository,omitempty"`
	// Tests emits a test file per module.
	Tests *bool `json:"tests,omitempty"`
	// Constants emits a constants package.
	Constants *bool `json:"constants,omitempty"`
}

// Config is the whole file.
//
// Two shapes are supported, because inceptools-cli supports both and a
// repository using it must keep working untouched:
//
//	{ "env_variable": "DATABASE_URL", "migration_path": "./migrations" }
//
// is the single-database form, and yields one target named "default". The
// other is the "databases" map below. Setting both is an error rather than a
// merge — it reads as though it configures two targets and does not.
type Config struct {
	// Module is the Go module path. Read from go.mod when absent.
	Module string `json:"module,omitempty"`
	// Dialect is the default SQL dialect for migrations.
	Dialect string `json:"dialect,omitempty"`

	// EnvVariable and MigrationPath are the single-database form.
	EnvVariable   string `json:"env_variable,omitempty"`
	MigrationPath string `json:"migration_path,omitempty"`

	// Databases are the migration targets, keyed by the name passed to -db.
	Databases map[string]Database `json:"databases,omitempty"`

	Layout   Layout   `json:"layout,omitempty"`
	Generate Generate `json:"generate,omitempty"`
	// Rules overrides a lint rule's severity: "error", "warn" or "off".
	Rules map[string]string `json:"rules,omitempty"`
	// Skip are path fragments the linter ignores.
	Skip []string `json:"skip,omitempty"`

	// Root is the directory the file was found in. Not serialised.
	Root string `json:"-"`
	// Found reports whether a file was actually read, as opposed to
	// defaults being returned.
	Found bool `json:"-"`

	// extra holds keys this version does not know about, so writing the file
	// back cannot drop another tool's configuration.
	extra map[string]json.RawMessage
}

// Dialects are the SQL engines the migrator supports, with the aliases
// inceptools-cli accepts. It is declared here, rather than imported from the
// migrator, so that validating a config file does not pull five database
// drivers into every build that reads one.
var Dialects = []string{
	"postgres", "pg",
	"mysql",
	"sqlite",
	"sqlserver", "mssql",
	"clickhouse",
}

// SupportedDialect reports whether name is one of Dialects, ignoring case.
func SupportedDialect(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, d := range Dialects {
		if d == name {
			return true
		}
	}
	return false
}

// Layer names used by the tooling.
const (
	LayerController = "controller"
	LayerService    = "service"
	LayerEntity     = "entity"
	LayerDTO        = "dto"
	LayerRepository = "repository"
	LayerConstants  = "constants"
)

// DefaultLayers is the recommended shape, plus the aliases seen most often.
func DefaultLayers() map[string][]string {
	return map[string][]string{
		LayerController: {"controller", "controllers", "handler", "handlers"},
		LayerService:    {"service", "services", "usecase", "usecases"},
		LayerEntity:     {"entity", "entities", "model", "models", "domain"},
		LayerDTO:        {"dto", "dtos", "types", "schema"},
		LayerRepository: {"repository", "repositories", "repo", "store", "storage"},
		LayerConstants:  {"constants", "const"},
	}
}

// Default returns the configuration used when there is no file.
func Default() *Config {
	yes := true
	return &Config{
		Dialect: "postgres",
		Layout: Layout{
			Apps:       "apps",
			Tests:      "colocated",
			TestsDir:   "tests",
			ModuleFile: "{module}_module.go",
			AppFile:    "{app}_app.go",
			Layers:     DefaultLayers(),
		},
		Generate: Generate{Repository: &yes, Tests: &yes, Constants: &yes},
		Root:     ".",
	}
}

// Find walks up from dir looking for icpt.json, stopping at the filesystem
// root or at a directory holding go.mod.
//
// Walking up means a command run from apps/catalog/products finds the repository's
// configuration, which is what anybody would expect and what every other
// tool does.
func Find(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(abs, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		// go.mod is the boundary: above it is somebody else's repository.
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return "", false
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

// Load reads the configuration for dir, falling back to the defaults.
//
// A missing file is NOT an error: the defaults are a working configuration,
// and requiring a config file to run a linter is friction with no benefit.
// A malformed file IS an error — silently ignoring it would apply defaults
// the author did not ask for and did not notice.
func Load(dir string) (*Config, error) {
	cfg := Default()

	path, ok := Find(dir)
	if !ok {
		abs, err := filepath.Abs(dir)
		if err == nil {
			cfg.Root = abs
		}
		return cfg, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("project: reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("project: %s is not valid JSON: %w", path, err)
	}
	if err := json.Unmarshal(raw, &cfg.extra); err != nil {
		return nil, fmt.Errorf("project: %s is not a JSON object: %w", path, err)
	}
	cfg.Root = filepath.Dir(path)
	cfg.Found = true
	cfg.applyDefaults()
	return cfg, cfg.Validate()
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.Dialect == "" {
		c.Dialect = d.Dialect
	}
	if c.Layout.Apps == "" {
		c.Layout.Apps = d.Layout.Apps
	}
	if c.Layout.Tests == "" {
		c.Layout.Tests = d.Layout.Tests
	}
	if c.Layout.TestsDir == "" {
		c.Layout.TestsDir = d.Layout.TestsDir
	}
	if c.Layout.ModuleFile == "" {
		c.Layout.ModuleFile = d.Layout.ModuleFile
	}
	if c.Layout.AppFile == "" {
		c.Layout.AppFile = d.Layout.AppFile
	}
	// Layers MERGE with the defaults rather than replacing them. Naming one
	// extra alias should not switch off every rule for every other layer,
	// which is what a replace would quietly do.
	merged := DefaultLayers()
	for layer, names := range c.Layout.Layers {
		merged[layer] = union(merged[layer], names)
	}
	c.Layout.Layers = merged

	if c.Generate.Repository == nil {
		c.Generate.Repository = d.Generate.Repository
	}
	if c.Generate.Tests == nil {
		c.Generate.Tests = d.Generate.Tests
	}
	if c.Generate.Constants == nil {
		c.Generate.Constants = d.Generate.Constants
	}
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Validate reports configuration that cannot work.
func (c *Config) Validate() error {
	var problems []string
	switch c.Layout.Tests {
	case "colocated", "mirror":
	default:
		problems = append(problems, fmt.Sprintf("layout.tests is %q; expected \"colocated\" or \"mirror\"", c.Layout.Tests))
	}
	if c.Dialect != "" && !SupportedDialect(c.Dialect) {
		problems = append(problems, fmt.Sprintf("dialect %q is not supported; have: %s",
			c.Dialect, strings.Join(Dialects, ", ")))
	}
	if len(c.Databases) > 0 && (c.EnvVariable != "" || c.MigrationPath != "") {
		problems = append(problems,
			"both the single-database form (env_variable/migration_path) and the "+
				"\"databases\" map are set; the top-level pair would be ignored, so remove it")
	}
	for name, db := range c.Databases {
		if db.EnvVariable == "" {
			problems = append(problems, fmt.Sprintf("database %q has no env_variable", name))
		}
		if db.Dialect != "" && !SupportedDialect(db.Dialect) {
			problems = append(problems, fmt.Sprintf("database %q has dialect %q; have: %s",
				name, db.Dialect, strings.Join(Dialects, ", ")))
		}
	}
	for rule, severity := range c.Rules {
		switch severity {
		case "error", "warn", "off":
		default:
			problems = append(problems, fmt.Sprintf("rules.%s is %q; expected \"error\", \"warn\" or \"off\"", rule, severity))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s: %s", FileName, strings.Join(problems, "; "))
	}
	return nil
}

// LayerOf returns the logical layer a directory name belongs to, or "".
func (c *Config) LayerOf(dirName string) string {
	for layer, names := range c.Layout.Layers {
		for _, n := range names {
			if n == dirName {
				return layer
			}
		}
	}
	return ""
}

// IsLayerDir reports whether a directory is a layer rather than a module.
func (c *Config) IsLayerDir(dirName string) bool { return c.LayerOf(dirName) != "" }

// AppsPath is the absolute path of the domains directory.
func (c *Config) AppsPath() string { return filepath.Join(c.Root, c.Layout.Apps) }

// ModuleFileName renders the module declaration file name.
func (c *Config) ModuleFileName(pkg string) string {
	return strings.ReplaceAll(c.Layout.ModuleFile, "{module}", pkg)
}

// AppFileName renders the domain aggregator file name.
func (c *Config) AppFileName(app string) string {
	return strings.ReplaceAll(c.Layout.AppFile, "{app}", app)
}

// MirrorTests reports whether tests live under TestsDir rather than beside
// the code.
func (c *Config) MirrorTests() bool { return c.Layout.Tests == "mirror" }

// Target is a fully resolved migration target.
type Target struct {
	Name string
	// EnvVariable holds the DSN. Never the DSN itself.
	EnvVariable string
	// MigrationPath is absolute, resolved against the repository root.
	MigrationPath string
	Dialect       string
	LedgerTable   string
	NoLock        bool
}

// Targets resolves every configured database into a deterministic,
// name-sorted list.
//
// The single-database form yields one target called "default", and a
// database entry with no migration_path defaults to ./migrations/<name> —
// both to match inceptools-cli, so a repository already using it resolves to
// the same directories here.
func (c *Config) Targets() []Target {
	if len(c.Databases) == 0 {
		env := c.EnvVariable
		if env == "" {
			env = "DATABASE_URL"
		}
		path := c.MigrationPath
		if path == "" {
			path = "./migrations"
		}
		return []Target{{
			Name:          DefaultTargetName,
			EnvVariable:   env,
			MigrationPath: c.resolve(path),
			Dialect:       c.Dialect,
		}}
	}

	names := make([]string, 0, len(c.Databases))
	for name := range c.Databases {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Target, 0, len(names))
	for _, name := range names {
		db := c.Databases[name]
		path := db.MigrationPath
		if path == "" {
			path = "./migrations/" + name
		}
		out = append(out, Target{
			Name:          name,
			EnvVariable:   db.EnvVariable,
			MigrationPath: c.resolve(path),
			Dialect:       c.DialectOf(db),
			LedgerTable:   db.LedgerTable,
			NoLock:        db.NoLock,
		})
	}
	return out
}

func (c *Config) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Root, path)
}

// DatabaseNames lists the configured migration targets, sorted.
func (c *Config) DatabaseNames() []string {
	targets := c.Targets()
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Name)
	}
	return out
}

// ErrNoSuchDatabase is returned when -db names something not configured.
var ErrNoSuchDatabase = errors.New("no such database in " + FileName)

// SelectTargets filters Targets by name.
//
// An empty name returns EVERY target. That is what `inceptools migrate` with
// no -db does, and changing it would silently stop migrating a database in
// a repository that relied on it. Callers for which fanning out is wrong —
// a rollback, creating a file — use SelectOne instead.
func (c *Config) SelectTargets(name string) ([]Target, error) {
	targets := c.Targets()
	if name == "" {
		return targets, nil
	}
	for _, t := range targets {
		if t.Name == name {
			return []Target{t}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q (configured: %s)",
		ErrNoSuchDatabase, name, strings.Join(c.DatabaseNames(), ", "))
}

// SelectOne resolves exactly one target, and refuses to guess between
// several.
//
// A rollback that fanned out across every database because -db was omitted
// is unrecoverable, and "it picked the first one" is the same mistake with
// better odds.
func (c *Config) SelectOne(name string) (Target, error) {
	targets, err := c.SelectTargets(name)
	if err != nil {
		return Target{}, err
	}
	if len(targets) == 1 {
		return targets[0], nil
	}
	return Target{}, fmt.Errorf(
		"%s configures %d databases (%s); name one with -db",
		FileName, len(targets), strings.Join(c.DatabaseNames(), ", "))
}

// Save writes the configuration, preserving any keys this version does not
// know about.
func (c *Config) Save(path string) error {
	merged := map[string]json.RawMessage{}
	for k, v := range c.extra {
		merged[k] = v
	}

	own, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var ownMap map[string]json.RawMessage
	if err := json.Unmarshal(own, &ownMap); err != nil {
		return err
	}
	for k, v := range ownMap {
		merged[k] = v
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
