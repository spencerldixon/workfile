// Package cli provides the three wf commands and their terminal presentation.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"workfile/internal/provider"
	"workfile/internal/workfile"
)

const help = `workfile — clear expectations for how work moves

Usage: wf <command> [options]

  health [KEY ...]       Summarise policy health by gate and stage
  status [KEY ...]       Show all stages, or the full picture for named tickets
  test [PROVIDER ...]    Test all provider connections, or named instances

Filters for health and status:
  --limit N             Check up to N tickets (default 25; health checks every ticket unless set)
  --all                 Check every ticket in scope
  --me                  Tickets assigned to you, or PRs you opened or must review
  --user NAME           The same filter for someone in people.yml
  --failing             Show only violations, blocked tickets, and errors

Filters for health only (list the failing tickets):
  --gate NAME           Tickets failing this gate
  --stage NAME          Tickets in this stage that are out of policy

Options:
  --dir DIR             Find .workfile/ from DIR or its parents (default .)
  --version             Print the installed version
  -h, --help            Show this help

Examples:
  wf health
  wf health --gate code_review --stage done
  wf status --me
  wf status APP-42
  wf test github

Output is always for people. Set NO_COLOR to disable colour.
`

type options struct {
	command, dir, user, gate, stage string
	keys                            []string
	limit                           int
	all, me, failing, help, version bool
	limitSet                        bool
}

// listing is true when health names tickets instead of summarising.
func (o options) listing() bool { return len(o.keys) > 0 || o.gate != "" || o.stage != "" }

// limited is false when every ticket in scope is checked.
func (o options) limited() bool {
	return !o.all && len(o.keys) == 0 && (o.command != "health" || o.limitSet)
}

type source interface {
	Tickets(context.Context, []string, int) ([]workfile.Ticket, error)
	Records(context.Context, []workfile.Ticket) ([]workfile.Ticket, []string, error)
	Identity(context.Context, string) (string, error)
	Test(context.Context, string) (string, error)
}

func Run(ctx context.Context, args []string, out, errOut io.Writer, version string) int {
	return run(ctx, args, out, errOut, version, func(w *workfile.Workspace) source { return provider.New(w) })
}

func run(ctx context.Context, args []string, out, errOut io.Writer, version string, connect func(*workfile.Workspace) source) int {
	view := newView(out)
	fail := func(err error) int {
		for i, line := range strings.Split(err.Error(), "\n") {
			prefix := "wf: "
			if i > 0 {
				prefix = "    "
			}
			fmt.Fprintln(errOut, prefix+clean(strings.TrimSpace(line)))
		}
		return 2
	}
	o, err := parse(args)
	if err != nil {
		return fail(fmt.Errorf("%w; run wf --help", err))
	}
	if o.version {
		fmt.Fprintln(out, "wf "+version)
		return 0
	}
	if o.help || o.command == "" {
		fmt.Fprint(out, help)
		return 0
	}
	w, err := workfile.Load(o.dir)
	if err != nil {
		return fail(err)
	}
	client := connect(w)
	if o.command == "test" {
		return testConnections(ctx, view, client, w, o.keys)
	}
	var who map[string]string
	if o.user != "" {
		person, ok := w.People[o.user]
		if !ok {
			return fail(fmt.Errorf("%s is not in people.yml", o.user))
		}
		who = map[string]string{}
		for name := range w.Providers {
			if person[name] != "" {
				who[name] = person[name]
			}
		}
		if len(who) == 0 {
			return fail(errors.New("that person has no provider identities in people.yml"))
		}
	} else if o.me {
		overrides, err := workfile.Me()
		if err != nil {
			return fail(err)
		}
		who = map[string]string{}
		for _, name := range workfile.SortedKeys(w.Providers) {
			id := overrides[name]
			if id == "" {
				id = overrides[w.Providers[name].Kind]
			}
			if id == "" {
				id, err = client.Identity(ctx, name)
				if err != nil {
					return fail(fmt.Errorf("%s identity: %w", name, err))
				}
			}
			who[name] = id
		}
	}
	if o.gate != "" {
		if _, ok := w.Policy.Gates[o.gate]; !ok {
			return fail(fmt.Errorf("%s is not a gate in policy.yml", o.gate))
		}
	}
	if o.stage != "" && !slices.Contains(w.Policy.States, o.stage) {
		return fail(fmt.Errorf("%s is not a stage in policy.yml", o.stage))
	}
	limit := o.limit
	if !o.limited() || who != nil {
		limit = 0
	}
	tickets, err := client.Tickets(ctx, o.keys, limit)
	if err != nil {
		return fail(err)
	}
	missing := false
	for _, key := range o.keys {
		if !slices.ContainsFunc(tickets, func(t workfile.Ticket) bool { return t.Key == key }) {
			fmt.Fprintf(errOut, "wf: %s is not visible in the configured Jira scope\n", key)
			missing = true
		}
	}
	tickets, warnings, err := client.Records(ctx, tickets)
	if err != nil {
		return fail(err)
	}
	for _, warning := range warnings {
		fmt.Fprintln(errOut, "wf: "+clean(warning))
	}
	if who != nil {
		tickets = slices.DeleteFunc(tickets, func(t workfile.Ticket) bool { return !t.Involves(w.Policy.Tracker, who) })
	}
	if o.limited() && len(tickets) > o.limit {
		tickets = tickets[:o.limit]
	}
	assessments := make([]workfile.Assessment, len(tickets))
	code := 0
	for i, ticket := range tickets {
		a := w.Assess(ticket)
		assessments[i] = a
		if a.Violation() || o.command == "status" && a.Blocked() {
			code = max(code, 1)
		}
		if a.Ticket.Error != "" {
			code = 2
		}
	}
	view.report(w, assessments, o, who)
	if missing {
		return 2
	}
	return code
}

func testConnections(ctx context.Context, view view, client source, w *workfile.Workspace, names []string) int {
	if len(names) == 0 {
		names = workfile.SortedKeys(w.Providers)
	}
	for _, name := range names {
		if _, ok := w.Providers[name]; !ok {
			view.line("  Unknown provider instance: " + name)
			return 2
		}
	}
	view.heading("connections")
	code := 0
	for _, name := range names {
		detail, err := client.Test(ctx, name)
		if err != nil {
			view.line("  " + view.paint("31", "✗ "+name))
			view.wrap(err.Error(), "    ")
			code = 2
		} else {
			view.line("  " + view.paint("32", "✓ "+name))
			view.wrap(detail, "    ")
		}
		view.line("")
	}
	return code
}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

func parse(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("wf", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.dir, "dir", ".", "")
	fs.StringVar(&o.user, "user", "", "")
	fs.IntVar(&o.limit, "limit", 25, "")
	fs.StringVar(&o.gate, "gate", "", "")
	fs.StringVar(&o.stage, "stage", "", "")
	fs.BoolVar(&o.all, "all", false, "")
	fs.BoolVar(&o.me, "me", false, "")
	fs.BoolVar(&o.failing, "failing", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	// The standard flag package stops at positional arguments; collect options first.
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			return o, fmt.Errorf("unknown option %s", arg)
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("--%s needs a value", name)
			}
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return o, err
	}
	if len(positionals) > 0 {
		o.command = positionals[0]
		o.keys = positionals[1:]
	}
	if o.help || o.version {
		return o, nil
	}
	fs.Visit(func(f *flag.Flag) { o.limitSet = o.limitSet || f.Name == "limit" })
	if o.limit < 1 {
		return o, errors.New("--limit must be at least 1")
	}
	if o.me && o.user != "" {
		return o, errors.New("choose either --me or --user")
	}
	if o.command == "" {
		return o, nil
	}
	if !slices.Contains([]string{"health", "status", "test"}, o.command) {
		return o, fmt.Errorf("unknown command %s", o.command)
	}
	if o.command == "test" {
		var invalid string
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "dir" {
				invalid = f.Name
			}
		})
		if invalid != "" {
			return o, fmt.Errorf("--%s is only for health and status", invalid)
		}
	} else {
		if o.command != "health" && (o.gate != "" || o.stage != "") {
			return o, errors.New("--gate and --stage are only for health")
		}
		for i, key := range o.keys {
			key = strings.ToUpper(key)
			if !keyPattern.MatchString(key) {
				return o, fmt.Errorf("%s is not a ticket key (expected APP-42)", key)
			}
			o.keys[i] = key
		}
	}
	return o, nil
}
