package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

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

type setupWizard struct {
	v           view
	input       *bufio.Scanner
	secret      func() (string, error)
	open        func(string) error
	connect     func(*workfile.Workspace, map[string]string) setupSource
	credentials string
}

func setup(ctx context.Context, v view, o options) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("setup needs an interactive terminal; run wf setup in your terminal")
	}
	state, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	s := setupWizard{v: v, input: bufio.NewScanner(os.Stdin), credentials: workfile.PersonalPath("credentials"), open: func(target string) error { return openSetupBrowser(ctx, target) }}
	s.secret = func() (string, error) {
		type reply struct {
			value string
			err   error
		}
		ready := make(chan reply, 1)
		go func() {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			ready <- reply{strings.TrimSpace(string(b)), err}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-ready:
			return r.value, r.err
		}
	}
	s.connect = func(w *workfile.Workspace, values map[string]string) setupSource {
		c := provider.New(w)
		c.Credentials.Set(values)
		return c
	}
	if err := v.setupIntro(ctx); err != nil {
		return err
	}
	return s.run(ctx, o.dir)
}

func (v view) setupIntro(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	v.heading("setup")
	v.block(muted, styled("From zero to your first report", "1"), []row{
		{plain("1 Connect  →  2 Discover  →  3 Save")},
		{plain("Read-only access. Your secrets stay on this computer.")},
	})
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

func (s *setupWizard) section(title, text string) {
	s.v.block(muted, styled(title, "1"), textRows(text, "", "", "", s.v.innerWidth()))
}
func (s *setupWizard) ask(ctx context.Context, label, def string) (string, error) {
	return s.askValid(ctx, label, def, func(value string) (string, error) { return value, nil })
}

func (s *setupWizard) askValid(ctx context.Context, label, def string, validate func(string) (string, error)) (string, error) {
	for {
		value, err := s.readAnswer(ctx, label, def)
		if err != nil {
			return "", err
		}
		if value == "" {
			s.v.wrap("A value is required. Please try again.", "  ")
			continue
		}
		value, err = validate(value)
		if err == nil {
			return value, nil
		}
		s.v.wrap("! "+err.Error(), "  ")
	}
}

func (s *setupWizard) readAnswer(ctx context.Context, label, def string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if def != "" {
		label += " [" + def + "]"
	}
	s.v.wrap(label, "  ")
	fmt.Fprint(s.v.out, "  > ")
	type reply struct {
		value string
		err   error
	}
	ready := make(chan reply, 1)
	go func() {
		if !s.input.Scan() {
			err := s.input.Err()
			if err == nil {
				err = errors.New("setup cancelled; no workspace was written")
			}
			ready <- reply{err: err}
			return
		}
		ready <- reply{value: strings.TrimSpace(s.input.Text())}
	}()
	var value string
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ready:
		if r.err != nil {
			return "", r.err
		}
		value = r.value
	}
	fmt.Fprintln(s.v.out)
	if value == "" {
		value = def
	}
	return value, ctx.Err()
}
func (s *setupWizard) yes(ctx context.Context, label string, def bool) (bool, error) {
	d := "n"
	if def {
		d = "y"
	}
	for {
		value, err := s.ask(ctx, label+" (y/n)", d)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		s.v.wrap("Please choose y or n.", "  ")
	}
}
func (s *setupWizard) token(ctx context.Context, label, target string) (string, error) {
	s.section(label, "Create a read-only token in your browser, then paste it below. Input is hidden. Never share this token.")
	s.v.wrap(target, "  ")
	if err := s.open(target); err != nil {
		s.v.wrap("Could not open the browser. Open the address above manually.", "  ")
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		s.v.wrap("Token (hidden):", "  ")
		fmt.Fprint(s.v.out, "  > ")
		value, err := s.secret()
		fmt.Fprintln(s.v.out)
		if err != nil {
			return "", err
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, "\r\n\x00\"'") {
			s.v.wrap("! Paste a non-empty token on one line, without quotes. Please try again.", "  ")
			continue
		}
		return value, nil
	}
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

func (s *setupWizard) run(ctx context.Context, defaultDir string) error {
	s.section("1 / Connect", "Jira holds your tickets and is required. GitHub is optional. We will test each connection before saving anything.")
	github, err := s.yes(ctx, "Connect GitHub too?", true)
	if err != nil {
		return err
	}
	dir, err := s.askValid(ctx, "Where should .workfile/ live? Choose its parent folder.", defaultDir, setupDirectory)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, ".workfile")
	site, err := s.askValid(ctx, "Jira Cloud hostname (for example example-team.atlassian.net)", "", setupSite)
	if err != nil {
		return err
	}
	email, err := s.askValid(ctx, "Jira account email", "", setupEmail)
	if err != nil {
		return err
	}
	s.v.wrap("Jira scopes: read:jira-work and read:jira-user. Include project and assignable-user read access for discovery.", "  ")
	token, err := s.token(ctx, "Connect Jira", "https://id.atlassian.com/manage-profile/security/api-tokens")
	if err != nil {
		return err
	}
	// Separate credentials for each setup folder, preserving other workspaces.
	key := fmt.Sprintf("WF_%X", time.Now().UnixNano())
	values := map[string]string{key + "_JIRA_EMAIL": email, key + "_JIRA_TOKEN": token}
	ref := func(suffix string) string { return "${" + key + suffix + "}" }
	w := &workfile.Workspace{Policy: workfile.Policy{Tracker: "jira"}, Providers: map[string]workfile.Provider{"jira": {Kind: "jira", Site: site, Auth: map[string]string{"email": ref("_JIRA_EMAIL"), "token": ref("_JIRA_TOKEN")}}}, People: map[string]map[string]string{}}
	client := s.connect(w, values)
	for {
		if _, err = client.Identity(ctx, "jira"); err == nil {
			break
		}
		s.v.block(red, styled("! Jira connection needs attention", "1"), textRows(err.Error(), "", "", "", s.v.innerWidth()))
		retry, e := s.yes(ctx, "Correct the Jira connection and try again?", true)
		if e != nil {
			return e
		}
		if !retry {
			return errors.New("setup cancelled; no files written")
		}
		site, e = s.askValid(ctx, "Jira Cloud hostname", site, setupSite)
		if e != nil {
			return e
		}
		email, e = s.askValid(ctx, "Jira account email", email, setupEmail)
		if e != nil {
			return e
		}
		p := w.Providers["jira"]
		p.Site = site
		w.Providers["jira"] = p
		values[key+"_JIRA_EMAIL"] = email
		token, e := s.token(ctx, "Connect Jira", "https://id.atlassian.com/manage-profile/security/api-tokens")
		if e != nil {
			return e
		}
		values[key+"_JIRA_TOKEN"] = token
		client = s.connect(w, values)
	}
	s.v.block(green, styled("✓ Jira connected", "1"), nil)
	if github {
		org, err := s.askValid(ctx, "GitHub organisation", "", setupOrg)
		if err != nil {
			return err
		}
		s.v.wrap("GitHub: grant read access to repositories, pull requests, labels, statuses, checks and organisation data. Authorise organisation SSO if needed.", "  ")
		token, err := s.token(ctx, "Connect GitHub", "https://github.com/settings/personal-access-tokens/new")
		if err != nil {
			return err
		}
		values[key+"_GITHUB_TOKEN"] = token
		w.Providers["github"] = workfile.Provider{Kind: "github", Org: org, Lookback: "90d", Auth: map[string]string{"token": ref("_GITHUB_TOKEN")}}
		for {
			if _, err = client.Test(ctx, "github"); err == nil {
				break
			}
			s.v.block(red, styled("! GitHub connection needs attention", "1"), textRows(err.Error(), "", "", "", s.v.innerWidth()))
			retry, e := s.yes(ctx, "Correct the GitHub connection and try again?", true)
			if e != nil {
				return e
			}
			if !retry {
				return errors.New("setup cancelled; no files written")
			}
			org, e = s.askValid(ctx, "GitHub organisation", org, setupOrg)
			if e != nil {
				return e
			}
			p := w.Providers["github"]
			p.Org = org
			w.Providers["github"] = p
			token, e := s.token(ctx, "Connect GitHub", "https://github.com/settings/personal-access-tokens/new")
			if e != nil {
				return e
			}
			values[key+"_GITHUB_TOKEN"] = token
			client = s.connect(w, values)
		}
		s.v.block(green, styled("✓ GitHub connected", "1"), nil)
	}
	s.section("2 / Discover", "Choose a Jira project. We will suggest a stage order from its status categories. Jira's full workflow moves and rules are not imported.")
	projects, err := client.SetupProjects(ctx)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return errors.New("no visible Jira projects; check token permissions")
	}
	for i, p := range projects {
		s.v.wrap(fmt.Sprintf("%d. %s — %s", i+1, clean(p.Key), clean(p.Name)), "  ")
	}
	var project string
	for {
		choice, err := s.ask(ctx, "Project number", "1")
		if err != nil {
			return err
		}
		n, e := strconv.Atoi(choice)
		if e == nil && n > 0 && n <= len(projects) {
			project = projects[n-1].Key
			break
		}
		s.v.wrap("Choose a number from the list.", "  ")
	}
	statuses, err := client.SetupStatuses(ctx, project)
	if err != nil {
		return err
	}
	for i, status := range statuses {
		s.v.wrap(fmt.Sprintf("%d. %s", i+1, clean(status)), "  ")
	}
	var order []int
	defaults := make([]string, len(statuses))
	for i := range statuses {
		defaults[i] = strconv.Itoa(i + 1)
	}
	for {
		answer, err := s.ask(ctx, "Choose the stages to include, first to last. Enter their numbers separated by spaces; leave unwanted stages out.", strings.Join(defaults, " "))
		if err != nil {
			return err
		}
		order, err = setupOrder(answer, len(statuses))
		if err == nil {
			break
		}
		s.v.wrap(err.Error(), "  ")
	}
	w.Policy.Gates = map[string][]workfile.Rule{}
	w.Policy.Transitions = map[string]workfile.Transition{}
	p := w.Providers["jira"]
	p.Scope = "project = " + strconv.Quote(project)
	if len(order) < len(statuses) {
		selected := make([]string, len(order))
		for i, n := range order {
			selected[i] = strconv.Quote(statuses[n])
		}
		p.Scope += " AND status in (" + strings.Join(selected, ", ") + ")"
		s.v.wrap("Only selected statuses will be included in the Jira search. Other statuses are outside this policy's coverage.", "  ")
	}
	p.Statuses = map[string]workfile.Names{}
	w.Policy.States = setupStates(statuses, order)
	for i, n := range order {
		state := w.Policy.States[i]
		p.Statuses[state] = workfile.Names{statuses[n]}
		if i == len(order)-1 {
			w.Policy.Transitions[state] = workfile.Transition{End: true}
		} else {
			w.Policy.Transitions[state] = workfile.Transition{To: []string{w.Policy.States[i+1]}}
		}
	}
	w.Providers["jira"] = p
	s.section("Starter workflow", "Stages: "+strings.Join(orderedStatuses(statuses, order), " → ")+". Suggested moves connect each stage to the next; the last is terminal. No gates are invented. Edit policy.yml to add your team's checks and branches.")
	ok, err := s.yes(ctx, "Use these starter moves?", true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("setup cancelled; no files written")
	}
	people, err := client.SetupPeople(ctx, project)
	if err != nil {
		s.v.wrap("! Could not import project people. You can add them later in people.yml.", "  ")
		ok, e := s.yes(ctx, "Continue with an empty people file?", true)
		if e != nil {
			return e
		}
		if !ok {
			return errors.New("setup cancelled; no files written")
		}
	} else {
		names := make([]string, len(people))
		indices := make([]int, len(people))
		for i, person := range people {
			names[i] = clean(person.DisplayName)
			if strings.TrimSpace(names[i]) == "" {
				names[i] = fmt.Sprintf("person_%d", i+1)
			}
			indices[i] = i
		}
		aliases := setupStates(names, indices)
		for i, person := range people {
			w.People[aliases[i]] = map[string]string{"name": clean(person.DisplayName), "jira": person.AccountID}
		}
	}
	s.section("3 / Save", fmt.Sprintf("Create %s with policy.yml, providers.yml and people.yml. Imported %d visible project people; GitHub identities are not guessed. Save tokens privately in %s. No existing workspace will be overwritten.", clean(target), len(w.People), clean(s.credentials)))
	ok, err = s.yes(ctx, "Save and finish?", true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("setup cancelled; no files written")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = saveSetup(dir, w, s.credentials, values); err != nil {
		return err
	}
	s.v.block(green, styled("✓ Your workfile is ready", "1"), textRows("Next: cd "+strconv.Quote(dir)+" and run wf status. This starter has no gates yet, so ready means no configured requirements. Add your team's checks in .workfile/policy.yml. Guide: docs/getting-started.md.", "", "", "", s.v.innerWidth()))
	return nil
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
func setupOrder(answer string, count int) ([]int, error) {
	fields := strings.Fields(answer)
	seen := map[int]bool{}
	var order []int
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > count || seen[n] {
			return nil, errors.New("enter valid listed numbers without duplicates")
		}
		seen[n] = true
		order = append(order, n-1)
	}
	if len(order) == 0 {
		return nil, errors.New("select at least one listed status")
	}
	return order, nil
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
