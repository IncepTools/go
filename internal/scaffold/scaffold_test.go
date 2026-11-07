package scaffold_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IncepTools/go/internal/scaffold"
)

func TestGeneratedModuleParsesAsGo(t *testing.T) {
	d := scaffold.NewData("example.com/svc", "apps", "catalog", "products")
	files, err := scaffold.Module(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files generated")
	}

	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		if _, err := parser.ParseFile(fset, f.Path, f.Content, parser.AllErrors); err != nil {
			t.Errorf("%s does not parse: %v", f.Path, err)
		}
		if strings.Contains(f.Content, "__BT__") {
			t.Errorf("%s still contains the backtick placeholder", f.Path)
		}
	}
}

func TestEntityNameIsSingularised(t *testing.T) {
	cases := map[string]string{
		"products":  "Product",
		"policies":  "Policy",
		"gateways":  "Gateway",
		"addresses": "Address",
		"status":    "Status",
	}
	for mod, want := range cases {
		if got := scaffold.NewData("m", "apps", "app", mod).Entity; got != want {
			t.Errorf("NewData(%q).Entity = %q, want %q", mod, got, want)
		}
	}
}

func TestImportPathsUseTheRealModule(t *testing.T) {
	d := scaffold.NewData("github.com/acme/billing", "apps", "invoices", "lines")
	if got := d.ImportBase(); got != "github.com/acme/billing/apps/invoices/lines" {
		t.Fatalf("ImportBase = %q", got)
	}

	files, err := scaffold.Module(d)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range files {
		if strings.Contains(f.Content, "github.com/acme/billing/apps/invoices/lines/service") {
			found = true
		}
	}
	if !found {
		t.Fatal("generated code does not import its own packages by the real module path")
	}
}

func TestWriteRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	files := []scaffold.File{{Path: "a/b.go", Content: "package b\n"}}

	if _, err := scaffold.Write(root, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.Write(root, files, false); err == nil {
		t.Fatal("the generator overwrote an existing file; there is no undo for that")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	files := []scaffold.File{{Path: "a/b.go", Content: "package b\n"}}

	written, err := scaffold.Write(root, files, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %v", written)
	}
	if _, err := os.Stat(filepath.Join(root, "a/b.go")); err == nil {
		t.Fatal("a dry run created a file")
	}
}

func TestUpdateDTOUsesPointers(t *testing.T) {
	// A plain field cannot express "set this to its zero value", so a PATCH
	// that clears a description silently does nothing.
	d := scaffold.NewData("m", "apps", "catalog", "products")
	files, err := scaffold.Module(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "dto") {
			if !strings.Contains(f.Content, "Name        *string") {
				t.Fatalf("the generated update DTO does not use pointers:\n%s", f.Content)
			}
			return
		}
	}
	t.Fatal("no DTO generated")
}
