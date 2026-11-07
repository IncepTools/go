package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IncepTools/go/internal/migrate"
)

func pair(t *testing.T, dir, stem, up, down string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stem+".up.sql"), []byte(up), 0o644); err != nil {
		t.Fatal(err)
	}
	if down != "" {
		if err := os.WriteFile(filepath.Join(dir, stem+".down.sql"), []byte(down), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadSortsByVersion(t *testing.T) {
	dir := t.TempDir()
	pair(t, dir, "000002_second", "SELECT 2;", "SELECT -2;")
	pair(t, dir, "000001_first", "SELECT 1;", "SELECT -1;")
	pair(t, dir, "000010_tenth", "SELECT 10;", "SELECT -10;")

	files, err := migrate.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{files[0].Version, files[1].Version, files[2].Version}
	want := []string{"000001", "000002", "000010"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if files[0].Name != "first" {
		t.Fatalf("name = %q", files[0].Name)
	}
}

func TestDuplicateVersionIsRefused(t *testing.T) {
	// The failure: the ledger is keyed on the version, so one of these never
	// runs and the other is recorded under the wrong name. Nothing warns.
	dir := t.TempDir()
	pair(t, dir, "000027_add_index", "SELECT 1;", "SELECT -1;")
	pair(t, dir, "000027_add_column", "SELECT 2;", "SELECT -2;")

	_, err := migrate.Load(dir)
	if err == nil {
		t.Fatal("two migrations sharing a version were accepted")
	}
	if !strings.Contains(err.Error(), "000027") {
		t.Fatalf("the error does not name the version: %v", err)
	}
}

func TestMissingDownScriptIsRefused(t *testing.T) {
	dir := t.TempDir()
	pair(t, dir, "000001_init", "SELECT 1;", "")

	if _, err := migrate.Load(dir); err == nil {
		t.Fatal("a migration with no reverse was accepted")
	}
}

func TestBadFileNameIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oops.up.sql"), []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Load(dir); err == nil {
		t.Fatal("a file not named <version>_<name>.up.sql was accepted")
	}
}

func TestEmptyDirectoryIsAnError(t *testing.T) {
	if _, err := migrate.Load(t.TempDir()); err == nil {
		t.Fatal("an empty migration directory passed silently")
	}
}

func TestCreateUsesTheNextVersionAndKeepsTheWidth(t *testing.T) {
	dir := t.TempDir()
	pair(t, dir, "000001_init", "SELECT 1;", "SELECT -1;")
	pair(t, dir, "000009_nine", "SELECT 9;", "SELECT -9;")

	up, down, err := migrate.Create(dir, "Add product SKU!")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(up) != "000010_add_product_sku.up.sql" {
		t.Fatalf("up = %s", filepath.Base(up))
	}
	if filepath.Base(down) != "000010_add_product_sku.down.sql" {
		t.Fatalf("down = %s", filepath.Base(down))
	}

	// Both files must exist, or Load refuses the directory next time.
	files, err := migrate.Load(dir)
	if err != nil {
		t.Fatalf("the directory is unloadable after Create: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %d, want 3", len(files))
	}
}

func TestCreateInAnEmptyDirectoryStartsAtOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "migrations")
	up, _, err := migrate.Create(dir, "init")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(up) != "000001_init.up.sql" {
		t.Fatalf("up = %s", filepath.Base(up))
	}
}

func TestCreateNeedsAName(t *testing.T) {
	if _, _, err := migrate.Create(t.TempDir(), "   "); err == nil {
		t.Fatal("a nameless migration was created")
	}
}
