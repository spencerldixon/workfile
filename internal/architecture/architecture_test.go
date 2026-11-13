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
// libraries use too, so they are allowed. The v1 libraries are not.

// allowed is the internal packages each internal package may import.
var allowed = map[string][]string{
	"internal/workfile":     {},
	"internal/provider":     {"internal/workfile"},
	"internal/board":        {"internal/workfile"},
	"internal/snapshot":     {"internal/workfile"},
	"internal/automation":   {"internal/workfile"},
	"internal/cli":          {"internal/workfile", "internal/provider", "internal/board", "internal/snapshot", "internal/automation"},
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
			switch {
			case strings.HasPrefix(imp, "github.com/charmbracelet/") && !strings.HasPrefix(imp, "github.com/charmbracelet/x/"):
				t.Errorf("%s imports %s: use the v2 packages under charm.land", name, imp)
			case strings.HasPrefix(imp, "charm.land/") && name != "internal/cli":
				t.Errorf("%s imports %s: only internal/cli may import Charm packages", name, imp)
			case imp == "net/http" && name != "internal/provider":
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
