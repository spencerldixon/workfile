// Package cli provides wf commands and their terminal presentation.
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
	"time"

	"workfile/internal/provider"
	"workfile/internal/workfile"
)

const help = `workfile — clear expectations for how work moves

Usage: wf <command> [options]

  setup                 Connect providers and create a starter .workfile/
  health [KEY ...]       Summarise policy health by gate and stage
  status [KEY ...]       Show all stages, or the full picture for named tickets
  check KEY --to STAGE   Ask whether a ticket may move to a stage now (exit 0 yes, 1 no)
  test [PROVIDER ...]    Test all provider connections, or named instances

Automations ("when this happens to my work, do that"):
  automations           List your automations and anything stopping them
  automations init      Create your private automations file for this workspace
  automations trust     Approve the actions in your shared automations file
  watch                 Check your work every 2 minutes and fire automations

Options for automations and watch:
  --all                 automations: everyone's shared automations, read only
  --log ID              automations: show the latest run log for an automation
  --once                watch: check once, then exit (for cron or launchd)
  --interval 5m         watch: time between checks (default 2m, at least 1m)
  --dry-run             watch: show what would run; run nothing

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
  --json                Emit a versioned machine-readable report (health/status)
  --evidence            Show observed evidence for each requirement (health/status)
  --dir DIR             Find .workfile/ from DIR or its parents (default .)
  --version             Print the installed version
  -h, --help            Show this help

Examples:
  wf health
  wf health --gate code_review --stage done
  wf status --me
  wf status APP-42
  wf check APP-42 --to done
  wf watch --dry-run
  wf test github

Output is for people by default. Use --json for scripts and agents.
Set NO_COLOR to disable colour.
`

type options struct {
	command, dir, user, gate, stage string
	keys                            []string
	limit                           int
	all, me, failing, help, version bool
	json, evidence                  bool
	limitSet                        bool
	// check, automations and watch
	to, log      string
	once, dryRun bool
	interval     time.Duration
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
	jsonOutput := slices.Contains(args, "--json") || slices.Contains(args, "--json=true")
	fail := func(err error) int {
		if jsonOutput {
			writeJSONError(out, err)
			return 2
		}
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
	jsonOutput = o.json
	if o.version {
		fmt.Fprintln(out, "wf "+version)
		return 0
	}
	if o.help || o.command == "" {
		fmt.Fprint(out, help)
		return 0
	}
	if o.command == "setup" {
		if err := setup(ctx, view, o); err != nil {
			return fail(err)
		}
		return 0
	}
	w, err := workfile.Load(o.dir)
	if err != nil {
		return fail(err)
	}
	if o.command == "health" || o.command == "status" || o.command == "check" {
		if o.keys, err = completeKeys(o.keys, w.Providers[w.Policy.Tracker].DefaultProject); err != nil {
			return fail(err)
		}
	}
	client := connect(w)
	switch o.command {
	case "test":
		return testConnections(ctx, view, client, w, o.keys)
	case "check", "automations", "watch":
		code, err := commands[o.command](ctx, view, w, client, o, errOut)
		if err != nil {
			return fail(err)
		}
		return code
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
		if who, err = identity(ctx, w, client); err != nil {
			return fail(err)
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
			fmt.Fprintf(errOut, "wf: %s is not visible to your Jira token: check the key, or ask for access\n", key)
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
	if missing {
		code = 2
	}
	if o.json {
		if err := writeReport(out, w, assessments, o, warnings, code); err != nil {
			return fail(err)
		}
	} else if o.command == "health" && interactiveHealth(view, o, assessments) {
		if err := exploreHealth(ctx, view, w, assessments, o); err != nil {
			return fail(err)
		}
	} else if o.command == "status" && interactiveStatus(view, o, assessments) {
		// m switches to the person's own work, so ask who they are, unless the
		// list is already someone's. Without an answer the key is left out.
		var me map[string]string
		if who == nil {
			me, _ = identity(ctx, w, client)
		}
		if err := exploreStatus(ctx, view, w, assessments, o, me); err != nil {
			return fail(err)
		}
	} else {
		view.report(w, assessments, o)
		view.evidenceReport(assessments, o)
	}
	return code
}

// identity is who "me" is on each provider: the setting in config.yml, or
// whoever owns the token.
func identity(ctx context.Context, w *workfile.Workspace, client source) (map[string]string, error) {
	overrides, err := workfile.Me()
	if err != nil {
		return nil, err
	}
	who := map[string]string{}
	for _, name := range workfile.SortedKeys(w.Providers) {
		id := overrides[name]
		if id == "" {
			id = overrides[w.Providers[name].Kind]
		}
		if id == "" {
			id, err = client.Identity(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("%s identity: %w", name, err)
			}
		}
		who[name] = id
	}
	return who, nil
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
var numberPattern = regexp.MustCompile(`^[0-9]+$`)

// completeKeys turns bare numbers into full keys using the tracker's
// default_project, and drops repeats so a ticket is only fetched once.
func completeKeys(keys []string, project string) ([]string, error) {
	var result []string
	for _, key := range keys {
		if numberPattern.MatchString(key) {
			if project == "" {
				return nil, fmt.Errorf("%s is not a ticket key; use the full key (for example APP-%s) or set default_project in providers.yml", key, key)
			}
			number := strings.TrimLeft(key, "0")
			if number == "" {
				number = "0"
			}
			key = project + "-" + number
		}
		if !slices.Contains(result, key) {
			result = append(result, key)
		}
	}
	return result, nil
}

// command is a newer command. It returns an exit code, or an error to print.
type command func(context.Context, view, *workfile.Workspace, source, options, io.Writer) (int, error)

var commands = map[string]command{"check": checkMove, "automations": automations, "watch": watch}

// commandFlags lists the options each newer command takes.
var commandFlags = map[string][]string{
	"check":       {"dir", "to", "json"},
	"automations": {"dir", "all", "log"},
	"watch":       {"dir", "once", "interval", "dry-run"},
}

// checkCommand checks the positional arguments of the newer commands.
func checkCommand(o options) error {
	switch o.command {
	case "check":
		if len(o.keys) != 1 {
			return errors.New("check takes one ticket, such as wf check APP-42 --to done")
		}
		key := strings.ToUpper(o.keys[0])
		if !keyPattern.MatchString(key) && !numberPattern.MatchString(key) {
			return fmt.Errorf("%s is not a ticket key (expected APP-42 or a number)", key)
		}
		o.keys[0] = key
		if o.to == "" {
			return errors.New("check needs --to STAGE, the stage the ticket would move to")
		}
	case "automations":
		if len(o.keys) > 1 || len(o.keys) == 1 && o.keys[0] != "init" && o.keys[0] != "trust" {
			return errors.New("automations takes init, trust or nothing")
		}
		if len(o.keys) == 1 && (o.all || o.log != "") {
			return fmt.Errorf("automations %s takes no options", o.keys[0])
		}
		if o.all && o.log != "" {
			return errors.New("choose either --all or --log")
		}
	case "watch":
		if len(o.keys) > 0 {
			return errors.New("watch takes no positional arguments")
		}
		if o.interval < time.Minute {
			return errors.New("--interval must be at least 1m")
		}
	}
	return nil
}

func parse(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("wf", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.dir, "dir", ".", "")
	fs.StringVar(&o.user, "user", "", "")
	fs.IntVar(&o.limit, "limit", 25, "")
	fs.StringVar(&o.gate, "gate", "", "")
	fs.StringVar(&o.stage, "stage", "", "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&o.evidence, "evidence", false, "")
	fs.BoolVar(&o.all, "all", false, "")
	fs.BoolVar(&o.me, "me", false, "")
	fs.BoolVar(&o.failing, "failing", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	fs.StringVar(&o.to, "to", "", "")
	fs.StringVar(&o.log, "log", "", "")
	fs.BoolVar(&o.once, "once", false, "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.DurationVar(&o.interval, "interval", 2*time.Minute, "")
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
	if !slices.Contains([]string{"health", "status", "check", "test", "setup", "automations", "watch"}, o.command) {
		return o, fmt.Errorf("unknown command %s", o.command)
	}
	if allowed, ok := commandFlags[o.command]; ok {
		var invalid string
		fs.Visit(func(f *flag.Flag) {
			if !slices.Contains(allowed, f.Name) {
				invalid = f.Name
			}
		})
		if invalid != "" {
			return o, fmt.Errorf("--%s is not an option for wf %s", invalid, o.command)
		}
		return o, checkCommand(o)
	}
	var invalid string
	fs.Visit(func(f *flag.Flag) {
		if slices.Contains([]string{"to", "log", "once", "dry-run", "interval"}, f.Name) {
			invalid = f.Name
		}
	})
	if invalid != "" && o.command != "test" && o.command != "setup" {
		return o, fmt.Errorf("--%s is not an option for wf %s", invalid, o.command)
	}
	if o.command == "setup" && len(o.keys) > 0 {
		return o, errors.New("setup takes no positional arguments; use --dir to choose a folder")
	}
	if o.command == "test" || o.command == "setup" {
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
			// A bare number such as 2713 is completed once the workspace is
			// loaded, because the project comes from providers.yml.
			if !keyPattern.MatchString(key) && !numberPattern.MatchString(key) {
				return o, fmt.Errorf("%s is not a ticket key (expected APP-42 or a number)", key)
			}
			o.keys[i] = key
		}
	}
	return o, nil
}
