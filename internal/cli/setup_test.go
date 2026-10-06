package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupFake struct{ peopleErr bool }

func (setupFake) Identity(context.Context, string) (string, error) { return "example-user", nil }
func (setupFake) Test(context.Context, string) (string, error)     { return "connected", nil }
func (setupFake) SetupProjects(context.Context) ([]provider.SetupProject, error) {
	return []provider.SetupProject{{Key: "APP", Name: "Example project"}}, nil
}
func (setupFake) SetupStatuses(context.Context, string) ([]string, error) {
	return []string{"To Do", "In Progress", "Done"}, nil
}
func (f setupFake) SetupPeople(context.Context, string) ([]provider.SetupPerson, error) {
	if f.peopleErr {
		return nil, errors.New("permission denied")
	}
	return []provider.SetupPerson{{AccountID: "example-alice", DisplayName: "Alice", Active: true}}, nil
}

func wizardForTest(t *testing.T, width int, answers string) (*setupWizard, *bytes.Buffer, string) {
	t.Helper()
	root := t.TempDir()
	var out bytes.Buffer
	s := &setupWizard{v: view{out: &out, width: width}, input: bufio.NewScanner(strings.NewReader(answers)), secret: func() (string, error) { return "example-token", nil }, open: func(string) error { return errors.New("no browser") }, credentials: filepath.Join(root, "private", "credentials"), connect: func(*workfile.Workspace, map[string]string) setupSource { return setupFake{} }}
	return s, &out, root
}
func TestSetupCreatesValidatedWorkspaceAndPrivateCredentials(t *testing.T) {
	for _, width := range []int{40, 60} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("WORKFILE_BACKGROUND", "off")
			s, out, root := wizardForTest(t, width, "n\n\nexample-team.atlassian.net\nalice@example.com\n\n\n\n\n")
			if err := s.v.setupIntro(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.run(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			w, err := workfile.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(w.Policy.States) != 3 || len(w.Policy.Gates) != 0 || len(w.People) != 1 {
				t.Fatalf("unexpected workspace: %+v", w)
			}
			if !w.Policy.Transitions["done"].End {
				t.Fatal("last stage must be terminal")
			}
			info, err := os.Stat(s.credentials)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatal("credentials not private")
			}
			c := workfile.Credentials{Path: s.credentials}
			token, err := c.Get(w.Providers["jira"].Auth["token"])
			if err != nil || token != "example-token" {
				t.Fatalf("credential lookup failed: %v", err)
			}
			for _, name := range []string{"providers.yml", "people.yml", "policy.yml"} {
				data, err := os.ReadFile(filepath.Join(root, ".workfile", name))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("example-token")) {
					t.Fatal("workspace contains secret")
				}
			}
			if strings.Contains(out.String(), "example-token") || strings.Contains(out.String(), "\x1b") {
				t.Fatal("unsafe plain output")
			}
			for _, line := range strings.Split(out.String(), "\n") {
				if runewidth.StringWidth(line) > width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
		})
	}
}
func TestSetupDeclinedSaveWritesNothing(t *testing.T) {
	s, _, root := wizardForTest(t, 60, "n\n\nexample-team.atlassian.net\nalice@example.com\n\n\n\nno\n")
	if err := s.run(context.Background(), root); err == nil {
		t.Fatal("expected cancellation")
	}
	for _, path := range []string{filepath.Join(root, ".workfile"), s.credentials} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected file %s", path)
		}
	}
}
func TestSetupDoesNotOverwriteWorkspace(t *testing.T) {
	s, _, root := wizardForTest(t, 60, "n\n\n")
	if err := os.Mkdir(filepath.Join(root, ".workfile"), 0755); err != nil {
		t.Fatal(err)
	}
	called := false
	s.connect = func(*workfile.Workspace, map[string]string) setupSource { called = true; return setupFake{} }
	if err := s.run(context.Background(), root); err == nil || called {
		t.Fatal("existing workspace not protected")
	}
}
func TestSetupInvalidEntriesRetryInPlace(t *testing.T) {
	s, out, root := wizardForTest(t, 40, "n\n\n\nnot-a-site\nhttps://example-team.atlassian.net/\n\nnot-an-email\nalice@example.com\n99\n1\n1 1 3\n1 2 3\ny\ny\n")
	secretReads := 0
	s.secret = func() (string, error) {
		secretReads++
		switch secretReads {
		case 1:
			return "", nil
		case 2:
			return "bad\nvalue", nil
		default:
			return "example-token", nil
		}
	}
	if err := s.run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if w.Providers["jira"].Site != "example-team.atlassian.net" || secretReads != 3 {
		t.Fatal("corrections were not retained")
	}
	for _, message := range []string{"A value is required", "enter a Jira Cloud hostname", "enter an email address", "Paste a non-empty token", "Choose a number", "valid listed numbers without duplicates"} {
		if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), message) {
			t.Fatalf("missing retry feedback %q", message)
		}
	}
	if strings.Contains(out.String(), "example-token") {
		t.Fatal("token was printed")
	}
}

func TestSetupExistingDestinationCanBeCorrected(t *testing.T) {
	s, _, root := wizardForTest(t, 60, "")
	if err := os.Mkdir(filepath.Join(root, ".workfile"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, ".workfile", "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	s.input = bufio.NewScanner(strings.NewReader("n\n\n" + other + "\nexample-team.atlassian.net\nalice@example.com\n\n\n\n\n"))
	if err := s.run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := workfile.Load(other); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep" {
		t.Fatal("existing workspace changed")
	}
}

type setupTwoPeopleFake struct{ setupFake }

func (setupTwoPeopleFake) SetupPeople(context.Context, string) ([]provider.SetupPerson, error) {
	return []provider.SetupPerson{{AccountID: "example-alice", DisplayName: "Alice", Active: true}, {AccountID: "example-bob", DisplayName: "Bob", Active: true}}, nil
}

func TestSetupSelectedStatusesAndYAMLFormatting(t *testing.T) {
	s, out, root := wizardForTest(t, 60, "n\n\nexample-team.atlassian.net\nalice@example.com\n\n1 3\n\n\n")
	s.connect = func(*workfile.Workspace, map[string]string) setupSource { return setupTwoPeopleFake{} }
	if err := s.run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(w.Policy.States, " ") != "to_do done" {
		t.Fatalf("unexpected states: %v", w.Policy.States)
	}
	if len(w.Providers["jira"].Statuses) != 2 || w.Providers["jira"].Scope != `project = "APP" AND status in ("To Do", "Done")` {
		t.Fatalf("unexpected Jira config: %+v", w.Providers["jira"])
	}
	if !strings.Contains(out.String(), "Other statuses are outside") {
		t.Fatal("missing coverage explanation")
	}
	policy, err := os.ReadFile(filepath.Join(root, ".workfile", "policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.Split(strings.TrimSpace(string(policy)), "\n\n")
	if len(sections) != 4 {
		t.Fatalf("expected four spaced sections: %s", policy)
	}
	for i, key := range []string{"tracker:", "states:", "transitions:", "gates:"} {
		if !strings.HasPrefix(sections[i], key) {
			t.Fatalf("section %d is not %s: %s", i, key, sections[i])
		}
	}
	people, err := os.ReadFile(filepath.Join(root, ".workfile", "people.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(people), "alice:\n") || !strings.Contains(string(people), "\n\nbob:\n") || len(w.People) != 2 {
		t.Fatalf("people not spaced or round-trippable: %s", people)
	}
}

func TestSetupOrganisationRetry(t *testing.T) {
	s, _, _ := wizardForTest(t, 60, "\nhttps://github.com/example-team\nexample-team\n")
	org, err := s.askValid(context.Background(), "GitHub organisation", "", setupOrg)
	if err != nil || org != "example-team" {
		t.Fatalf("organisation retry failed: %q %v", org, err)
	}
}

func TestSetupOrder(t *testing.T) {
	for _, answer := range []string{"", "1 1 3", "0 2 3", "1 2 4", "one two three"} {
		if _, err := setupOrder(answer, 3); err == nil {
			t.Fatalf("accepted %q", answer)
		}
	}
	for _, answer := range []string{"2 1 3", "2 1", "2"} {
		order, err := setupOrder(answer, 3)
		if err != nil || order[0] != 1 {
			t.Fatalf("valid selection %q rejected", answer)
		}
	}
}
func TestSetupFlags(t *testing.T) {
	for _, args := range [][]string{{"setup", "--json"}, {"setup", "--me"}, {"setup", "APP-1"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := parse([]string{"setup", "--dir", "/tmp/example"}); err != nil {
		t.Fatal(err)
	}
}
func TestSetupGitHubAndPeopleFallback(t *testing.T) {
	s, out, root := wizardForTest(t, 60, "y\n\nexample-team.atlassian.net\nalice@example.com\nexample-team\n\n\n\n\n\n")
	s.connect = func(*workfile.Workspace, map[string]string) setupSource { return setupFake{peopleErr: true} }
	if err := s.run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Providers) != 2 || len(w.People) != 0 {
		t.Fatal("wrong optional-provider or fallback configuration")
	}
	if !strings.Contains(out.String(), "✓ GitHub connected") || !strings.Contains(out.String(), "Could not import project people") {
		t.Fatal("missing connection or fallback feedback")
	}
}

type setupRetryFake struct {
	setupFake
	calls int
}

func (f *setupRetryFake) Identity(context.Context, string) (string, error) {
	f.calls++
	if f.calls == 1 {
		return "", errors.New("provider returned HTTP 401; check the token")
	}
	return "example-user", nil
}
func TestSetupRetriesTokenBeforeSaving(t *testing.T) {
	s, _, root := wizardForTest(t, 60, "n\n\nexample-team.atlassian.net\nalice@example.com\ny\nexample-other.atlassian.net\nbob@example.com\n\n\n\n\n")
	fake := &setupRetryFake{}
	s.connect = func(*workfile.Workspace, map[string]string) setupSource { return fake }
	reads := 0
	s.secret = func() (string, error) { reads++; return "example-token-" + strconv.Itoa(reads), nil }
	if err := s.run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	w, err := workfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	c := workfile.Credentials{Path: s.credentials}
	got, err := c.Get(w.Providers["jira"].Auth["token"])
	if err != nil || got != "example-token-2" || fake.calls != 2 {
		t.Fatalf("retry did not save the replacement: %q %v", got, err)
	}
	email, err := c.Get(w.Providers["jira"].Auth["email"])
	if err != nil || email != "bob@example.com" || w.Providers["jira"].Site != "example-other.atlassian.net" {
		t.Fatal("retry did not save corrected site and email")
	}
}
func TestSetupCredentialFailureRemovesWorkspace(t *testing.T) {
	s, _, root := wizardForTest(t, 60, "n\n\nexample-team.atlassian.net\nalice@example.com\n\n\n\n\n")
	if err := os.MkdirAll(s.credentials, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.run(context.Background(), root); err == nil {
		t.Fatal("expected credential write failure")
	}
	if _, err := os.Stat(filepath.Join(root, ".workfile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed save left a partial workspace")
	}
}
func TestSetupStatesResolveCollisions(t *testing.T) {
	states := setupStates([]string{"In Review", "In-Review", "Done"}, []int{0, 1, 2})
	if strings.Join(states, " ") != "in_review in_review_2 done" {
		t.Fatalf("unexpected aliases: %v", states)
	}
}

func TestSetupPlainIntro(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	v := newView(&out)
	if err := v.setupIntro(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\r") {
		t.Fatal("terminal escapes in plain output")
	}
}

func TestSetupColorIntroHasNoAnimation(t *testing.T) {
	t.Setenv("WORKFILE_BACKGROUND", "off")
	var out bytes.Buffer
	v := view{out: &out, width: 60, color: true}
	if err := v.setupIntro(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\r") || strings.Contains(out.String(), "\x1b[2K") {
		t.Fatal("startup animation controls in output")
	}
}
