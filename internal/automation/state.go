package automation

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"workfile/internal/workfile"
)

// Trust holds the approved actions of the person's shared file, by
// automation id. Someone else could change the shared file in a pull request,
// so its actions run only once the person approves them on their computer.
type Trust map[string]Approved

// Approved is the part of an automation that decides what runs.
type Approved struct {
	Run   []string `json:"run,omitempty"`
	Agent string   `json:"agent,omitempty"`
	Mode  string   `json:"mode"`
	Scope string   `json:"scope"`
	// Scripts holds a fingerprint of each repository script that run names,
	// so a pull request that changes a script needs approving again.
	Scripts map[string]string `json:"scripts,omitempty"`
}

func approvedOf(a workfile.Automation, root string) Approved {
	return Approved{Run: a.Run, Agent: a.Agent, Mode: a.EffectiveMode(), Scope: a.Scope, Scripts: Scripts(root, a.Run)}
}

// Scripts fingerprints the files in root that run names by a relative path,
// such as ./scripts/announce.sh. Programs found on PATH are the person's own.
func Scripts(root string, run []string) map[string]string {
	var scripts map[string]string
	for _, arg := range run {
		if strings.Contains(arg, "{") || !strings.ContainsRune(arg, '/') || filepath.IsAbs(arg) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, arg))
		if err != nil {
			continue // not a file, such as an owner/repo argument
		}
		if scripts == nil {
			scripts = map[string]string{}
		}
		scripts[arg] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return scripts
}

// ChangedScripts lists the scripts added, removed or edited since approval.
func ChangedScripts(before, after map[string]string) []string {
	var changed []string
	for path, sum := range after {
		if before[path] != sum {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

// Change is one shared automation whose actions differ from the approved ones.
// Before is nil for an automation that was never approved.
type Change struct {
	ID     string
	Before *Approved
	After  Approved
}

// Changes lists the file's automations that are new or changed since approval.
func (t Trust) Changes(f *workfile.AutomationFile, root string) []Change {
	var changes []Change
	for _, a := range f.Automations {
		after := approvedOf(a, root)
		before, ok := t[a.ID]
		if !ok {
			changes = append(changes, Change{ID: a.ID, After: after})
			continue
		}
		if !slices.Equal(before.Run, after.Run) || before.Agent != after.Agent || before.Mode != after.Mode || before.Scope != after.Scope || len(ChangedScripts(before.Scripts, after.Scripts)) > 0 {
			changes = append(changes, Change{ID: a.ID, Before: &before, After: after})
		}
	}
	return changes
}

// ApprovedAny reports whether any of the file's automations was approved.
func (t Trust) ApprovedAny(f *workfile.AutomationFile) bool {
	for _, a := range f.Automations {
		if _, ok := t[a.ID]; ok {
			return true
		}
	}
	return false
}

// Approve records the file's current actions as approved.
func (t Trust) Approve(f *workfile.AutomationFile, root string) Trust {
	approved := Trust{}
	for _, a := range f.Automations {
		approved[a.ID] = approvedOf(a, root)
	}
	return approved
}

// State is what `wf watch` remembers between checks.
type State struct {
	// Facts holds each automation's trigger facts from the last check.
	Facts map[string]FactSet `json:"facts"`
	// Queue holds actions waiting their turn: those over the limit for one
	// check, and questions that nobody has answered yet.
	Queue []Event `json:"queue"`
	// History is the newest events, oldest first.
	History []Event `json:"history"`
}

// FactSet is one automation's facts and the fingerprint they were made with.
type FactSet struct {
	Fingerprint string            `json:"fingerprint"`
	Values      map[string]string `json:"values"`
}

const historyLimit = 200

// Record adds a finished event to the history.
func (s *State) Record(e Event) {
	s.History = append(s.History, e)
	if len(s.History) > historyLimit {
		s.History = slices.Clone(s.History[len(s.History)-historyLimit:])
	}
}

// Last is the newest event for an automation, if any.
func (s *State) Last(id string) (Event, bool) {
	for i := len(s.History) - 1; i >= 0; i-- {
		if s.History[i].Automation == id {
			return s.History[i], true
		}
	}
	return Event{}, false
}

// Store reads and writes the state folder for one workspace folder.
type Store struct{ Dir string }

func (s Store) LogDir() string { return filepath.Join(s.Dir, "logs") }

func (s Store) State() (*State, error) {
	st := &State{}
	if err := s.read("state.json", st); err != nil {
		return nil, err
	}
	if st.Facts == nil {
		st.Facts = map[string]FactSet{}
	}
	return st, nil
}

func (s Store) SaveState(st *State) error { return s.write("state.json", st) }

func (s Store) Trust() (Trust, error) {
	t := Trust{}
	if err := s.read("trust.json", &t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s Store) SaveTrust(t Trust) error { return s.write("trust.json", t) }

func (s Store) read(name string, target any) error {
	path := filepath.Join(s.Dir, name)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s is damaged; delete it to start from a fresh starting point", path)
	}
	return nil
}

// write replaces the file in one step, so a crash never leaves half a file.
func (s Store) write(name string, value any) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.Dir, name))
}

const logLife = 14 * 24 * time.Hour

// PruneLogs removes run logs older than 14 days.
func (s Store) PruneLogs(now time.Time) {
	entries, _ := os.ReadDir(s.LogDir())
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && now.Sub(info.ModTime()) > logLife {
			os.Remove(filepath.Join(s.LogDir(), entry.Name()))
		}
	}
}

// safe keeps a value usable in a file name.
func safe(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, text)
}
