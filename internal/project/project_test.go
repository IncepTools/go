package project_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IncepTools/go/internal/project"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, project.FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMissingFileYieldsWorkingDefaults(t *testing.T) {
	cfg, err := project.Load(t.TempDir())
	if err != nil {
		t.Fatalf("a missing %s must not be an error: %v", project.FileName, err)
	}
	if cfg.Found {
		t.Fatal("Found is true with no file")
	}
	if cfg.Layout.Apps != "apps" || cfg.Layout.Tests != "colocated" {
		t.Fatalf("defaults = %+v", cfg.Layout)
	}
	if cfg.LayerOf("service") != project.LayerService {
		t.Fatal("the default layer names are not applied")
	}
}

// The existing inceptools-cli file must keep working untouched.
func TestExistingMigrationConfigIsRead(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{
	  "dialect": "postgres",
	  "databases": {
	    "reporting": { "env_variable": "REPORTING_DATABASE_URL", "migration_path": "./migrations" }
	  }
	}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	target, err := cfg.SelectOne("")
	if err != nil {
		t.Fatal(err)
	}
	if target.Name != "reporting" || target.EnvVariable != "REPORTING_DATABASE_URL" {
		t.Fatalf("target = %+v", target)
	}
	if cfg.Layout.Apps != "apps" {
		t.Fatal("the layout defaults were not applied to a migration-only file")
	}
}

func TestSavePreservesUnknownKeys(t *testing.T) {
	// inceptools-cli owns "dialect" and "databases"; a future tool may own
	// more. Rewriting the file must not drop them.
	dir := t.TempDir()
	path := writeConfig(t, dir, `{
	  "dialect": "postgres",
	  "databases": { "main": { "env_variable": "DATABASE_URL", "migration_path": "./migrations" } },
	  "someOtherTool": { "keep": "me" }
	}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Layout.Apps = "internal/apps"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["someOtherTool"]; !ok {
		t.Fatalf("an unknown key was dropped on save:\n%s", raw)
	}
	if _, ok := out["databases"]; !ok {
		t.Fatalf("the migration config was dropped on save:\n%s", raw)
	}
	if !strings.Contains(string(raw), "internal/apps") {
		t.Fatalf("the change was not written:\n%s", raw)
	}
}

func TestCustomLayerNamesMergeWithTheDefaults(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"layout": {"layers": {"repository": ["gateway"]}}}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LayerOf("gateway") != project.LayerRepository {
		t.Fatal("the custom name was not added")
	}
	if cfg.LayerOf("repository") != project.LayerRepository {
		t.Fatal("naming one alias switched off the default; layers must merge, not replace")
	}
	if cfg.LayerOf("service") != project.LayerService {
		t.Fatal("an unrelated layer was lost")
	}
}

func TestSeveralDatabasesRequireAName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"databases": {
	  "global": {"env_variable":"A","migration_path":"./m"},
	  "reporting":  {"env_variable":"B","migration_path":"./n"}
	}}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.SelectOne(""); err == nil {
		t.Fatal("SelectOne guessed between two databases; a rollback would hit the wrong one")
	}
	if _, err := cfg.SelectOne("reporting"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.SelectOne("nope"); err == nil {
		t.Fatal("an unknown database name was accepted")
	}
	// migrate with no -db fans out on purpose: applying is additive and
	// idempotent, so skipping a database silently is the worse failure.
	all, err := cfg.SelectTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("SelectTargets(\"\") returned %d targets, want both", len(all))
	}
}

func TestInvalidConfigIsRejected(t *testing.T) {
	for _, bad := range []string{
		`{"layout": {"tests": "somewhere"}}`,
		`{"dialect": "oracle"}`,
		`{"databases": {"a": {"env_variable":"A","migration_path":"./m","dialect":"oracle"}}}`,
		`{"rules": {"service-imports-http": "maybe"}}`,
		`{"databases": {"a": {"migration_path": "./m"}}}`,
		`{"env_variable":"A","migration_path":"./m","databases":{"b":{"env_variable":"B"}}}`,
	} {
		dir := t.TempDir()
		writeConfig(t, dir, bad)
		if _, err := project.Load(dir); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
}

func TestMultipleDialectsAreAllowed(t *testing.T) {
	// One repository, two engines: a Postgres primary and a SQLite cache, or
	// two databases mid-migration between engines.
	dir := t.TempDir()
	writeConfig(t, dir, `{
	  "dialect": "postgres",
	  "databases": {
	    "main":  {"env_variable":"DATABASE_URL","migration_path":"./migrations"},
	    "cache": {"env_variable":"CACHE_URL","migration_path":"./migrations/cache",
	              "dialect":"sqlite","ledger_table":"icpt_migrations","no_lock":true}
	  }
	}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	main, err := cfg.SelectOne("main")
	if err != nil {
		t.Fatal(err)
	}
	if main.Dialect != "postgres" {
		t.Fatalf("main dialect = %q; it should inherit the file's", main.Dialect)
	}
	cache, err := cfg.SelectOne("cache")
	if err != nil {
		t.Fatal(err)
	}
	if cache.Dialect != "sqlite" {
		t.Fatalf("cache dialect = %q; the per-database override was ignored", cache.Dialect)
	}
	if cache.LedgerTable != "icpt_migrations" || !cache.NoLock {
		t.Fatalf("per-database options were lost: %+v", cache)
	}
}

func TestMalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"layout":`)
	if _, err := project.Load(dir); err == nil {
		t.Fatal("a malformed file was silently replaced by defaults")
	}
}

func TestFindWalksUpButStopsAtGoMod(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"layout":{"apps":"domains"}}`)
	deep := filepath.Join(root, "apps", "catalog", "products")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := project.Load(deep)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Layout.Apps != "domains" {
		t.Fatal("the repository config was not found from a subdirectory")
	}

	// A nested module is a different repository; do not reach past it.
	nested := filepath.Join(root, "tools", "thing")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found := project.Find(nested); found {
		t.Fatal("Find walked past a go.mod into the parent repository")
	}
}

func TestSingleDatabaseShape(t *testing.T) {
	// The form with no "databases" map yields one target called "default".
	dir := t.TempDir()
	writeConfig(t, dir, `{
	  "env_variable": "DATABASE_URL",
	  "migration_path": "./migrations",
	  "dialect": "postgres"
	}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	targets := cfg.Targets()
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].Name != project.DefaultTargetName {
		t.Fatalf("name = %q, want %q", targets[0].Name, project.DefaultTargetName)
	}
	if targets[0].EnvVariable != "DATABASE_URL" {
		t.Fatalf("env = %q", targets[0].EnvVariable)
	}
	if !filepath.IsAbs(targets[0].MigrationPath) {
		t.Fatalf("migration path %q is not resolved against the root", targets[0].MigrationPath)
	}
}

func TestEmptyFileStillYieldsADefaultTarget(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	targets := cfg.Targets()
	if len(targets) != 1 || targets[0].EnvVariable != "DATABASE_URL" {
		t.Fatalf("targets = %+v; an empty file should still describe one usable target", targets)
	}
}

func TestMigrationPathDefaultsToTheDatabaseName(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"databases": {"reporting": {"env_variable": "REPORTING_URL"}}}`)

	cfg, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	target, err := cfg.SelectOne("reporting")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(target.MigrationPath) != "reporting" {
		t.Fatalf("migration path = %q, want it to default to ./migrations/reporting", target.MigrationPath)
	}
}

func TestDialectAliasesAreAccepted(t *testing.T) {
	for _, d := range []string{"pg", "mssql", "sqlserver", "clickhouse", "MySQL"} {
		dir := t.TempDir()
		writeConfig(t, dir, `{"dialect": "`+d+`"}`)
		if _, err := project.Load(dir); err != nil {
			t.Errorf("dialect %q was rejected: %v", d, err)
		}
	}
}
