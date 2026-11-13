// Package cli provides wf commands and their terminal presentation.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"workfile/internal/provider"
	"workfile/internal/workfile"
)

const help = `workfile — clear expectations for how work moves

Usage: wf <command> [options]

  setup                 Connect providers and create a starter .workfile/
  health [KEY ...]       Summarise policy health by gate and stage
  status [KEY ...]       Show all stages, or the full picture for named tickets
  tickets               Browse tickets in scope: read each one with what Workfile knows
  check KEY --to STAGE   Ask whether a ticket may move to a stage now (exit 0 yes, 1 no)
  prove                 Check the policy for paths that skip a gate (exit 0 sound, 1 not)
  test [PROVIDER ...]    Test all provider connections, or named instances

Options for tickets:
  --view NAME           Start on all, me or a state in your policy (left and right to switch)
  --scope QUERY         Replace the configured scope, in your tracker's own query language

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
  --json                Emit a versioned machine-readable report (health/status/check/prove)
  --evidence            Show observed evidence for each requirement (health/status)
  --dir DIR             Find .workfile/ from DIR or its parents (default .)
  --version             Print the installed version
  -h, --help            Show this help

Examples:
  wf health
  wf health --gate code_review --stage done
  wf status --me
  wf status APP-42
  wf tickets --view me
  wf check APP-42 --to done
  wf prove
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
	// check
	to string
	// tickets
	scope, view string
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
	Listing(context.Context, string) ([]workfile.Listing, error)
	Detail(context.Context, string) (workfile.Detail, error)
	Image(context.Context, string) ([]byte, error)
}

// command is one wf command. Each has its own file, named after it.
type command struct {
	flags       []string                                // the options it takes
	args        func(options) error                     // checks its positional arguments; nil takes any
	ticketKeys  bool                                    // its arguments are tickets: check them and complete bare numbers
	noWorkspace bool                                    // it runs without a .workfile/
	run         func(context.Context, env) (int, error) // returns an exit code, or an error to print
}

// env is what a command works with. w and client are nil for a noWorkspace
// command.
type env struct {
	view   view
	w      *workfile.Workspace
	client source
	o      options
	errOut io.Writer
}

// commands is every wf command. To add one, write <name>.go with its command
// value, list it here, and describe it in help.
var commands = map[string]command{
	"setup":   setupCommand,
	"health":  healthCommand,
	"status":  statusCommand,
	"tickets": ticketsCommand,
	"check":   checkCommand,
	"prove":   proveCommand,
	"test":    testCommand,
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
	c := commands[o.command]
	e := env{view: view, o: o, errOut: errOut}
	if !c.noWorkspace {
		if e.w, err = workfile.Load(o.dir); err != nil {
			return fail(err)
		}
		if c.ticketKeys {
			if e.o.keys, err = completeKeys(o.keys, e.w.TrackerKeys(), e.w.Providers[e.w.Policy.Tracker].DefaultProject); err != nil {
				return fail(err)
			}
		}
		e.client = connect(e.w)
	}
	code, err := c.run(ctx, e)
	if err != nil {
		return fail(err)
	}
	return code
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
	fs.StringVar(&o.scope, "scope", "", "")
	fs.StringVar(&o.view, "view", "", "")
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
	c, ok := commands[o.command]
	if !ok {
		return o, fmt.Errorf("unknown command %s", o.command)
	}
	var invalid string
	fs.Visit(func(f *flag.Flag) {
		if !slices.Contains(c.flags, f.Name) {
			invalid = f.Name
		}
	})
	if invalid != "" {
		return o, fmt.Errorf("--%s is not an option for wf %s", invalid, o.command)
	}
	if c.args != nil {
		return o, c.args(o)
	}
	return o, nil
}
