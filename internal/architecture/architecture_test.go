// Package architecture checks the import rules in the Phase 1 spec.
package architecture

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "workfile/"

// Packages under github.com/charmbracelet/x/ are shared helpers that the v2
// libraries use too. Harmonica is a spring helper, allowed only in the CLI.
// The v1 UI libraries are not allowed.
func charmImportProblem(pkg, imp string) string {
	const harmonica = "github.com/charmbracelet/harmonica"
	if strings.HasPrefix(imp, "github.com/charmbracelet/") &&
		!strings.HasPrefix(imp, "github.com/charmbracelet/x/") && imp != harmonica {
		return "use the v2 packages under charm.land"
	}
	if (strings.HasPrefix(imp, "charm.land/") || imp == harmonica) && pkg != "internal/cli" {
		return "only internal/cli may import Charm packages"
	}
	return ""
}

func TestCharmImportRules(t *testing.T) {
	for _, tt := range []struct {
		pkg, imp string
		allowed  bool
	}{
		{"internal/cli", "github.com/charmbracelet/harmonica", true},
		{"internal/workfile", "github.com/charmbracelet/harmonica", false},
		{"internal/cli", "github.com/charmbracelet/harmonica/other", false},
		{"internal/cli", "github.com/charmbracelet/bubbletea", false},
		{"internal/cli", "github.com/charmbracelet/bubbles/list", false},
		{"internal/cli", "github.com/charmbracelet/lipgloss", false},
		{"internal/cli", "charm.land/bubbletea/v2", true},
		{"internal/cli", "charm.land/bubbles/v2/list", true},
		{"internal/cli", "charm.land/lipgloss/v2", true},
		{"internal/workfile", "charm.land/bubbletea/v2", false},
		{"internal/cli", "github.com/charmbracelet/x/ansi", true},
		{"internal/workfile", "fmt", true},
	} {
		t.Run(tt.pkg+"/"+tt.imp, func(t *testing.T) {
			if problem := charmImportProblem(tt.pkg, tt.imp); (problem == "") != tt.allowed {
				t.Fatalf("allowed = %t, want %t; problem: %s", problem == "", tt.allowed, problem)
			}
		})
	}
}

// allowed is the internal packages each internal package may import.
var allowed = map[string][]string{
	"internal/workfile":     {},
	"internal/provider":     {"internal/workfile"},
	"internal/board":        {"internal/workfile"},
	"internal/snapshot":     {"internal/workfile"},
	"internal/run":          {},
	"internal/cli":          {"internal/workfile", "internal/provider", "internal/board", "internal/snapshot", "internal/run"},
	"internal/architecture": {},
}

type pkg struct {
	ImportPath  string
	Imports     []string
	TestImports []string
}

func list(t *testing.T) []pkg {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = "../.."
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list: %v: %s", err, stderr.String())
	}
	var pkgs []pkg
	dec := json.NewDecoder(&out)
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func TestImportRules(t *testing.T) {
	for _, p := range list(t) {
		name := strings.TrimPrefix(p.ImportPath, module)
		imports := slices.Concat(p.Imports, p.TestImports)
		for _, imp := range imports {
			if problem := charmImportProblem(name, imp); problem != "" {
				t.Errorf("%s imports %s: %s", name, imp, problem)
				continue
			}
			switch {
			case imp == "net/http" && name != "internal/provider" && slices.Contains(p.Imports, imp): // tests may fake a transport
				t.Errorf("%s imports net/http: only internal/provider may", name)
			case strings.HasPrefix(imp, module+"internal/"):
				rules, known := allowed[name]
				if !strings.HasPrefix(name, "internal/") {
					continue // cmd/wf may import anything
				}
				if !known {
					t.Errorf("%s is not in the architecture table: add it", name)
					continue
				}
				if dep := strings.TrimPrefix(imp, module); dep != name && !slices.Contains(rules, dep) {
					t.Errorf("%s may not import %s", name, dep)
				}
			}
		}
	}
}
