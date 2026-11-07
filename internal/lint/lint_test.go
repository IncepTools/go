package lint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IncepTools/go/internal/lint"
)

func write(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root string) []lint.Finding {
	t.Helper()
	f, err := lint.Run(lint.Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func has(findings []lint.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestCleanTreeHasNoFindings(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/catalog/catalog_app.go", "package catalog\n")
	write(t, root, "apps/catalog/products/products_module.go", "package products\n")
	write(t, root, "apps/catalog/products/service/s.go", "package service\n\nimport \"context\"\n\nvar _ = context.Background\n")
	write(t, root, "apps/catalog/products/controller/c.go", "package controller\n\nimport \"github.com/gofiber/fiber/v3\"\n\nvar _ fiber.Ctx\n")

	if f := run(t, root); len(f) != 0 {
		t.Fatalf("findings on a clean tree: %v", f)
	}
}

func TestServiceImportingHTTPIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/catalog/catalog_app.go", "package catalog\n")
	write(t, root, "apps/catalog/products/products_module.go", "package products\n")
	write(t, root, "apps/catalog/products/service/s.go",
		"package service\n\nimport \"github.com/gofiber/fiber/v3\"\n\nvar _ fiber.Ctx\n")

	findings := run(t, root)
	if !has(findings, "service-imports-http") {
		t.Fatalf("not caught: %v", findings)
	}
	for _, f := range findings {
		if f.Rule == "service-imports-http" {
			if f.Severity != lint.Error {
				t.Errorf("severity = %s, want error", f.Severity)
			}
			if f.Why == "" {
				t.Error("the finding does not say what failure it prevents")
			}
		}
	}
}

func TestControllerImportingGORMIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/catalog/catalog_app.go", "package catalog\n")
	write(t, root, "apps/catalog/products/products_module.go", "package products\n")
	write(t, root, "apps/catalog/products/controller/c.go",
		"package controller\n\nimport \"gorm.io/gorm\"\n\nvar _ *gorm.DB\n")

	if !has(run(t, root), "controller-imports-db") {
		t.Fatal("a controller reaching for the database was not caught")
	}
}

func TestModuleWithNoModuleFileIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/catalog/catalog_app.go", "package catalog\n")
	write(t, root, "apps/catalog/products/service/s.go", "package service\n")

	if !has(run(t, root), "module-has-no-module-file") {
		t.Fatal("a module directory with nothing declaring it was not caught")
	}
}

func TestCrossModuleInternalsIsAWarning(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/catalog/catalog_app.go", "package catalog\n")
	write(t, root, "apps/catalog/products/products_module.go", "package products\n")
	write(t, root, "apps/catalog/products/service/s.go",
		"package service\n\nimport \"example.com/x/apps/billing/invoices/service\"\n\nvar _ = service.X\n")

	findings := run(t, root)
	if !has(findings, "cross-module-internals") {
		t.Fatalf("not caught: %v", findings)
	}
}

func TestMissingAppsDirectoryIsAnError(t *testing.T) {
	if _, err := lint.Run(lint.Options{Root: t.TempDir()}); err == nil {
		t.Fatal("linting a tree with no apps/ succeeded silently")
	}
}
