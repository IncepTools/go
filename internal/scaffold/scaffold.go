// Package scaffold writes the files for a new module, app or service.
//
// Generated code here is a starting point that already does the things people
// forget: the repository is tenant-scoped, the list endpoint is paginated with
// an allow-listed sort, the create route carries a guard, the update DTO uses
// pointers so a PATCH can set a field to false, and the service returns typed
// errors rather than strings. Deleting a line you do not need is cheaper than
// remembering a line you did.
package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/IncepTools/go/internal/project"
)

// Data drives the templates.
type Data struct {
	// GoModule is the importing module path, e.g. "github.com/acme/billing".
	GoModule string
	// AppsDir is where domains live, e.g. "apps".
	AppsDir string
	// App is the domain, e.g. "catalog".
	App string
	// Module is the resource, e.g. "products".
	Module string
	// Package is Module as a Go identifier, e.g. "products".
	Package string
	// Entity is the singular Go type, e.g. "Product".
	Entity string
	// Receiver is a short receiver name, e.g. "s".
	Receiver string
	// Service is the service name for a scaffolded service.
	Service string

	// The fields below come from icpt.json via WithConfig. They exist so the
	// generator writes files where THIS repository keeps them, rather than
	// where the framework would prefer.
	moduleFile  string
	appFile     string
	mirrorTests bool
	testsDir    string
	withRepo    bool
	withTests   bool
	withConst   bool
}

// WithConfig applies the repository's conventions from icpt.json.
func (d Data) WithConfig(c *project.Config) Data {
	if c == nil {
		return d
	}
	if d.AppsDir == "" {
		d.AppsDir = c.Layout.Apps
	}
	d.moduleFile = c.ModuleFileName(d.Package)
	d.appFile = c.AppFileName(d.App)
	d.mirrorTests = c.MirrorTests()
	d.testsDir = c.Layout.TestsDir
	d.withRepo = c.Generate.Repository == nil || *c.Generate.Repository
	d.withTests = c.Generate.Tests == nil || *c.Generate.Tests
	d.withConst = c.Generate.Constants == nil || *c.Generate.Constants
	return d
}

// ModuleFile is the module declaration file name.
func (d Data) ModuleFile() string {
	if d.moduleFile != "" {
		return d.moduleFile
	}
	return d.Package + "_module.go"
}

// AppFile is the domain aggregator file name.
func (d Data) AppFile() string {
	if d.appFile != "" {
		return d.appFile
	}
	return d.App + "_app.go"
}

// NewData derives the template data from an app/module pair.
func NewData(goModule, appsDir, app, mod string) Data {
	pkg := identifier(mod)
	return Data{
		GoModule:  goModule,
		AppsDir:   appsDir,
		App:       app,
		Module:    mod,
		Package:   pkg,
		Entity:    exported(singular(mod)),
		Receiver:  string([]rune(pkg)[0]),
		testsDir:  "tests",
		withRepo:  true,
		withTests: true,
		withConst: true,
	}
}

// ImportBase is the import path of the module directory.
func (d Data) ImportBase() string {
	return strings.Join([]string{d.GoModule, d.AppsDir, d.App, d.Package}, "/")
}

// File is one generated file.
type File struct {
	Path    string
	Content string
}

// Module renders every file of a new module, honouring the repository's
// layout from icpt.json.
func Module(d Data) ([]File, error) {
	base := filepath.Join(d.AppsDir, d.App, d.Package)
	lower := strings.ToLower(d.Entity)

	type spec struct {
		path string
		tmpl string
	}
	specs := []spec{
		{filepath.Join(base, "entity", lower+".go"), entityTmpl},
		{filepath.Join(base, "dto", lower+"_dto.go"), dtoTmpl},
		{filepath.Join(base, "service", lower+"_service.go"), serviceTmpl},
		{filepath.Join(base, "controller", lower+"_controller.go"), controllerTmpl},
		{filepath.Join(base, d.ModuleFile()), moduleTmpl},
	}
	if d.withConst {
		specs = append([]spec{{filepath.Join(base, "constants", "constants.go"), constantsTmpl}}, specs...)
	}
	if d.withRepo {
		specs = append(specs, spec{filepath.Join(base, "repository", lower+"_repository.go"), repositoryTmpl})
	}
	if d.withTests {
		// A mirrored layout puts the test under tests/, at the same path as
		// the package it covers — which means it is an external test package
		// either way, so the generated file compiles in both.
		testPath := filepath.Join(base, d.Package+"_module_test.go")
		if d.mirrorTests {
			testPath = filepath.Join(d.testsDir, d.App, d.Package, d.Package+"_module_test.go")
		}
		specs = append(specs, spec{testPath, moduleTestTmpl})
	}

	out := make([]File, 0, len(specs))
	for _, s := range specs {
		content, err := render(s.path, s.tmpl, d)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: s.path, Content: content})
	}
	return out, nil
}

// AppFile renders the app aggregator.
func AppFile(d Data) (File, error) {
	path := filepath.Join(d.AppsDir, d.App, d.AppFile())
	content, err := render(path, appTmpl, d)
	if err != nil {
		return File{}, err
	}
	return File{Path: path, Content: content}, nil
}

// Service renders a new service skeleton.
func Service(d Data) ([]File, error) {
	specs := []struct {
		path string
		tmpl string
	}{
		{"main.go", mainTmpl},
		{filepath.Join("internal", "appconfig", "config.go"), serviceConfigTmpl},
		{".env.example", envTmpl},
		{"Makefile", makefileTmpl},
		{"README.md", serviceReadmeTmpl},
	}
	out := make([]File, 0, len(specs))
	for _, s := range specs {
		content, err := render(s.path, s.tmpl, d)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: s.path, Content: content})
	}
	return out, nil
}

// btToken stands in for a backtick inside the templates below. Go raw string
// literals cannot contain one, and the templates are full of struct tags.
const btToken = "__BT__"

func render(name, tmpl string, d Data) (string, error) {
	tmpl = strings.ReplaceAll(tmpl, btToken, "\x60")
	t, err := template.New(name).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("scaffold: parsing template for %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("scaffold: rendering %s: %w", name, err)
	}
	return buf.String(), nil
}

// Write creates the files under root.
//
// It never overwrites. A generator that clobbers an existing file destroys
// work with no undo, and the one time it happens costs more than every
// keystroke the generator ever saved.
func Write(root string, files []File, dryRun bool) ([]string, error) {
	var written []string
	for _, f := range files {
		full := filepath.Join(root, f.Path)
		if _, err := os.Stat(full); err == nil {
			return written, fmt.Errorf("scaffold: %s already exists; refusing to overwrite", f.Path)
		}
	}
	for _, f := range files {
		full := filepath.Join(root, f.Path)
		if dryRun {
			written = append(written, f.Path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return written, err
		}
		written = append(written, f.Path)
	}
	return written, nil
}

func identifier(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	out := b.String()
	if out == "" {
		return "module"
	}
	if unicode.IsDigit(rune(out[0])) {
		return "m" + out
	}
	return out
}

func exported(s string) string {
	if s == "" {
		return "Entity"
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// singular is a deliberately small heuristic: it handles the plurals that
// appear in resource names and leaves anything surprising alone, because a
// wrong guess here renames a type in generated code and is more annoying than
// no guess at all.
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "us"), strings.HasSuffix(s, "is"), strings.HasSuffix(s, "ss"):
		// status, analysis, access — the trailing s is part of the word.
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	default:
		return s
	}
}
