// Package automation runs a person's automations: it finds their shared and
// private files, remembers what each check saw, and starts the commands and
// agents they chose. It has no terminal code and does no network I/O; the
// caller reads Jira and GitHub and hands it the assessments.
package automation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"workfile/internal/workfile"
)

// Set is what this computer runs for one workspace: the person's shared file
// and their private file. A file that cannot run says why, and what to do.
type Set struct {
	Person string // the person's name in people.yml, or ""
	Shared *workfile.AutomationFile
	// SharedProblem says why the shared file does not run, or "" when it does.
	SharedProblem string
	// Changed lists shared automations whose actions changed since the person
	// last approved them. NeverApproved is set when none were ever approved.
	Changed       []Change
	NeverApproved bool

	PrivatePath    string // where the private file is, or would be
	Private        *workfile.AutomationFile
	PrivateProblem string
	// Remotes are the repository's git remotes, normalised.
	Remotes []string
	// NoAgent is set when no agent command is configured, so automations
	// that ask an agent cannot run.
	NoAgent bool
}

// NoAgentProblem says what to do when an agent automation has no agent.
const NoAgentProblem = "needs an agent command"

// Entry is one automation and the file it came from.
type Entry struct {
	File       *workfile.AutomationFile
	Automation workfile.Automation
	// Blocked says why it cannot run now, or "".
	Blocked string
}

// Entries lists every automation in the set, shared first, in file order.
func (s *Set) Entries() []Entry {
	var entries []Entry
	for _, pair := range []struct {
		f       *workfile.AutomationFile
		problem string
	}{{s.Shared, s.SharedProblem}, {s.Private, s.PrivateProblem}} {
		if pair.f == nil {
			continue
		}
		for _, a := range pair.f.Automations {
			blocked := pair.problem
			if blocked == "" && a.Agent != "" && s.NoAgent {
				blocked = NoAgentProblem
			}
			entries = append(entries, Entry{File: pair.f, Automation: a, Blocked: blocked})
		}
	}
	return entries
}

// Find works out whose shared file this computer runs and whether the private
// file may run here. who is the person's identity per provider, the same one
// `wf status --me` uses. trust holds the approved shared actions.
func Find(ctx context.Context, w *workfile.Workspace, personal workfile.Personal, who map[string]string, trust Trust) *Set {
	s := &Set{NoAgent: len(personal.Agent) == 0}
	s.Person, s.SharedProblem = person(w, personal, who)
	if s.Person != "" {
		s.Shared = w.SharedFile(s.Person)
		if s.Shared != nil {
			s.Changed = trust.Changes(s.Shared, Root(w))
			s.NeverApproved = !trust.ApprovedAny(s.Shared)
			switch {
			case s.NeverApproved:
				s.SharedProblem = "You have not approved its actions on this computer yet. Check them, then run wf automations trust."
			case len(s.Changed) > 0:
				s.SharedProblem = "Its actions changed since you approved them. Check the changes, then run wf automations trust."
			}
		}
	}
	if w.Policy.Name == "" {
		return s
	}
	s.PrivatePath = workfile.PrivateAutomationsPath(w.Policy.Name)
	private, err := w.ReadPrivateAutomations(s.PrivatePath)
	s.Private = private
	switch {
	case err != nil:
		s.PrivateProblem = err.Error()
	case private == nil:
		return s
	}
	if problems := workfile.CheckUniqueIDs(s.Shared, s.Private); len(problems) > 0 && s.PrivateProblem == "" {
		s.PrivateProblem = strings.Join(problems, "\n")
	}
	remotes, err := Remotes(ctx, Root(w))
	s.Remotes = remotes
	if s.PrivateProblem == "" && s.Private != nil && !Linked(s.Private.Repos, remotes) {
		switch {
		case err != nil:
			s.PrivateProblem = "Cannot read this repository's git remotes, so the private file does not run here. Private files need a git repository."
		case len(remotes) == 0:
			s.PrivateProblem = "This folder has no git remotes, or is not a git repository, so the private file does not run here."
		default:
			s.PrivateProblem = fmt.Sprintf("This repository is not in the file's repos list, so it does not run here. To run it here, add %s to repos.", remotes[0])
		}
	}
	return s
}

// person finds the person's name in people.yml: me.person, or the one person
// whose accounts match the identity.
func person(w *workfile.Workspace, personal workfile.Personal, who map[string]string) (string, string) {
	if personal.Person != "" {
		if _, ok := w.People[personal.Person]; !ok {
			return "", fmt.Sprintf("me.person is %s, who is not in people.yml.", personal.Person)
		}
		return personal.Person, ""
	}
	var matches []string
	for _, name := range workfile.SortedKeys(w.People) {
		for instance, id := range w.People[name] {
			if instance != "name" && id != "" && who[instance] == id {
				matches = append(matches, name)
				break
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], ""
	case 0:
		return "", "Workfile cannot tell who you are in people.yml. Set me.person in " + workfile.PersonalPath("config.yml") + "."
	default:
		return "", "More than one person in people.yml matches your accounts. Set me.person in " + workfile.PersonalPath("config.yml") + "."
	}
}

// Root is the folder that holds .workfile/.
func Root(w *workfile.Workspace) string { return filepath.Dir(w.Dir) }

// StateDir is where Workfile records what fired, run logs and approvals for
// one workspace folder. Two clones of a repository keep separate records.
func StateDir(w *workfile.Workspace) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(w.Dir)))[:12]
	if w.Policy.Name != "" {
		name = w.Policy.Name + "-" + name
	}
	return filepath.Join(base, "workfile", name)
}

// Remotes lists the git remotes of the repository that holds dir, normalised.
// Remotes are set when a repository is cloned and are never part of its
// files, so a repository cannot claim to be another one.
func Remotes(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get-regexp", `^remote\..*\.url$`)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil // a repository with no remotes
	}
	if err != nil {
		return nil, err
	}
	var remotes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		_, value, _ := strings.Cut(line, " ")
		if remote := workfile.NormalizeRemote(value); remote != "" && !slices.Contains(remotes, remote) {
			remotes = append(remotes, remote)
		}
	}
	return remotes, nil
}

// Linked reports whether any of the file's repos is one of the remotes.
func Linked(repos, remotes []string) bool {
	for _, repo := range repos {
		if r := workfile.NormalizeRemote(repo); r != "" && slices.Contains(remotes, r) {
			return true
		}
	}
	return false
}
