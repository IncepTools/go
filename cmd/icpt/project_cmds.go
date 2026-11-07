package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/IncepTools/go/internal/project"
	"github.com/IncepTools/go/src/web/router"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	root := fs.String("root", ".", "project root")
	apps := fs.String("apps", "", "directory holding the domains")
	tests := fs.String("tests", "", `where tests live: "colocated" or "mirror"`)
	force := fs.Bool("force", false, "rewrite an existing "+project.FileName)
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	path := filepath.Join(*root, project.FileName)
	cfg, err := project.Load(*root)
	if err != nil {
		return err
	}
	if cfg.Found && cfg.HadComments() && *force {
		return fmt.Errorf(
			"%s carries comments, and rewriting it would discard them.\n"+
				"Edit it by hand, or remove the comments first", path)
	}
	if cfg.Found && !*force {
		// Rewriting would reformat a file another tool also writes. Say what
		// is there and stop.
		fmt.Printf("%s already exists.\n", path)
		fmt.Printf("  apps:      %s\n  tests:     %s\n  databases: %s\n",
			cfg.Layout.Apps, cfg.Layout.Tests, strings.Join(cfg.DatabaseNames(), ", "))
		fmt.Println("\nPass -force to rewrite it. Keys this version does not know are preserved.")
		return nil
	}

	if *apps != "" {
		cfg.Layout.Apps = *apps
	}
	if *tests != "" {
		cfg.Layout.Tests = *tests
	}
	if cfg.Module == "" {
		if mod, err := readGoModule(*root); err == nil {
			cfg.Module = mod
		}
	}
	// Only the layer names that differ from the defaults are worth writing;
	// a file listing every default is noise that goes stale.
	cfg.Layout.Layers = nil

	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(path); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	fmt.Println("\nAdd a migration target under \"databases\" to use icpt migrate:")
	fmt.Println(`  "databases": { "main": { "env_variable": "DATABASE_URL", "migration_path": "./migrations" } }`)
	return nil
}

// routesDump is the shape Application.WriteRoutes produces.
type routesDump struct {
	Service string           `json:"service"`
	Routes  []router.Mounted `json:"routes"`
}

func cmdRoutes(args []string) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	file := fs.String("file", "routes.json", "route dump written by Application.WriteRoutes")
	asJSON := fs.Bool("json", false, "print the dump as JSON")
	unguarded := fs.Bool("unguarded", false, "list only mutating routes with no guard")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("%w\n\nWrite one from the service:\n"+
			"    app.WriteRoutes(\"routes.json\")", err)
	}
	var dump routesDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		return fmt.Errorf("%s is not a route dump: %w", *file, err)
	}
	if len(dump.Routes) == 0 {
		return errors.New("the dump contains no routes")
	}

	rows := dump.Routes
	if *unguarded {
		var filtered []router.Mounted
		for _, r := range rows {
			switch r.Method {
			case "POST", "PUT", "PATCH", "DELETE":
				if len(r.GuardNames) == 0 && !r.Public {
					filtered = append(filtered, r)
				}
			}
		}
		rows = filtered
		if len(rows) == 0 {
			fmt.Println("every mutating route carries a guard or is marked public")
			return nil
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].FullPath != rows[j].FullPath {
			return rows[i].FullPath < rows[j].FullPath
		}
		return rows[i].Method < rows[j].Method
	})

	if *asJSON {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "METHOD\tPATH\tMODULE\tGUARDS\tSUMMARY\n")
	for _, r := range rows {
		guards := strings.Join(r.GuardNames, ",")
		if guards == "" {
			guards = "-"
			if r.Public {
				guards = "(public)"
			}
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Method, r.FullPath, r.Module, guards, r.Doc.Summary)
	}
	_ = w.Flush()
	fmt.Printf("\n%d route(s) in %s\n", len(rows), dump.Service)
	if *unguarded {
		return errors.New("unguarded mutating routes found")
	}
	return nil
}
