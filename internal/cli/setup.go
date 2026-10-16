package cli

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"go.yaml.in/yaml/v3"
	"golang.org/x/term"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupSource interface {
	Identity(context.Context, string) (string, error)
	Test(context.Context, string) (string, error)
	SetupProjects(context.Context) ([]provider.SetupProject, error)
	SetupStatuses(context.Context, string) ([]string, error)
	SetupPeople(context.Context, string) ([]provider.SetupPerson, error)
}

// setup runs the interactive setup screens. The terminal's background is read
// first, because Bubble Tea takes over the terminal once it starts.
func setup(ctx context.Context, v view, o options) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("setup needs an interactive terminal; run wf setup in your terminal")
	}
	m := newSetupModel(ctx, v, setupDeps{
		dir:         o.dir,
		background:  v.laneBackground(),
		credentials: workfile.PersonalPath("credentials"),
		open:        func(target string) error { return openSetupBrowser(ctx, target) },
		connect: func(w *workfile.Workspace, values map[string]string) setupSource {
			c := provider.New(w)
			c.Credentials.Set(values)
			return c
		},
	})
	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(v.out)).Run()
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("setup cancelled; no files written")
		}
		return err
	}
	if fm, ok := final.(*setupModel); ok {
		if fm.fatal != nil {
			return fm.fatal
		}
		if !fm.saved {
			return errors.New("setup cancelled; no files written")
		}
	}
	return nil
}

func openSetupBrowser(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{target}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	default:
		command = "xdg-open"
		args = []string{target}
	}
	return exec.CommandContext(ctx, command, args...).Run()
}

func setupDirectory(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("choose a valid folder path")
	}
	dir, err := filepath.Abs(value)
	if err != nil {
		return "", errors.New("choose a valid folder path")
	}
	if _, err := os.Lstat(filepath.Join(dir, ".workfile")); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("destination .workfile already exists or cannot be accessed; choose another folder")
	}
	for parent := dir; ; parent = filepath.Dir(parent) {
		info, err := os.Stat(parent)
		if err == nil {
			if !info.IsDir() {
				return "", errors.New("choose a folder, not a file")
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(parent) == parent {
			return "", errors.New("folder cannot be accessed; choose another folder")
		}
	}
	return dir, nil
}

var setupSitePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.atlassian\.net$`)
var setupOrgPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func setupSite(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(value, "https://"), "/"))
	if !setupSitePattern.MatchString(value) {
		return "", errors.New("enter a Jira Cloud hostname ending in .atlassian.net")
	}
	return value, nil
}
func setupEmail(value string) (string, error) {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || strings.ContainsAny(value, "\r\n\x00\"'") {
		return "", errors.New("enter an email address, such as alice@example.com")
	}
	return value, nil
}
func setupOrg(value string) (string, error) {
	if !setupOrgPattern.MatchString(value) {
		return "", errors.New("enter the organisation name, not a GitHub URL")
	}
	return value, nil
}

func setupStates(statuses []string, order []int) []string {
	var states []string
	seen := map[string]bool{}
	for i, n := range order {
		base := strings.Trim(strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, strings.ToLower(statuses[n])), "_")
		if base == "" {
			base = fmt.Sprintf("stage_%d", i+1)
		}
		state := base
		for suffix := 2; seen[state]; suffix++ {
			state = fmt.Sprintf("%s_%d", base, suffix)
		}
		seen[state] = true
		states = append(states, state)
	}
	return states
}

func orderedStatuses(statuses []string, order []int) []string {
	r := make([]string, len(order))
	for i, n := range order {
		r[i] = clean(statuses[n])
	}
	return r
}

// Marshal each top-level entry separately to keep explicit order and spacing.
func setupYAMLSections(keys []string, values map[string]any) ([]byte, error) {
	if len(keys) == 0 {
		return []byte("{}\n"), nil
	}
	sections := make([]string, 0, len(keys))
	for _, key := range keys {
		data, err := yaml.Marshal(map[string]any{key: values[key]})
		if err != nil {
			return nil, err
		}
		sections = append(sections, strings.TrimSuffix(string(data), "\n"))
	}
	return []byte(strings.Join(sections, "\n\n") + "\n"), nil
}

func saveSetup(dir string, w *workfile.Workspace, credentials string, values map[string]string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(dir, ".workfile-setup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	transitions := map[string]any{}
	for state, move := range w.Policy.Transitions {
		if move.End {
			transitions[state] = map[string]any{"end": true}
		} else {
			transitions[state] = map[string]any{"to": move.To}
		}
	}
	policy := map[string]any{"tracker": w.Policy.Tracker, "states": w.Policy.States, "transitions": transitions, "gates": w.Policy.Gates}
	policyData, err := setupYAMLSections([]string{"tracker", "states", "transitions", "gates"}, policy)
	if err != nil {
		return err
	}
	people := map[string]any{}
	for name, identities := range w.People {
		people[name] = identities
	}
	peopleData, err := setupYAMLSections(workfile.SortedKeys(people), people)
	if err != nil {
		return err
	}
	providerData, err := yaml.Marshal(w.Providers)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"policy.yml": policyData, "providers.yml": providerData, "people.yml": peopleData} {
		if err = os.WriteFile(filepath.Join(staging, name), data, 0644); err != nil {
			return err
		}
	}
	// Validate exactly what will be installed before persisting credentials.
	testRoot, err := os.MkdirTemp(dir, ".workfile-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(testRoot)
	if err = os.Rename(staging, filepath.Join(testRoot, ".workfile")); err != nil {
		return err
	}
	if _, err = workfile.Load(testRoot); err != nil {
		return err
	}
	target := filepath.Join(dir, ".workfile")
	// Reserve the destination exclusively; concurrent setup cannot overwrite it.
	if err = os.Mkdir(target, 0755); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(target)
		}
	}()
	if err = workfile.SaveCredentials(credentials, values); err != nil {
		return err
	}
	for _, name := range []string{"policy.yml", "providers.yml", "people.yml"} {
		if err = os.Rename(filepath.Join(testRoot, ".workfile", name), filepath.Join(target, name)); err != nil {
			return err
		}
	}
	success = true
	return nil
}
