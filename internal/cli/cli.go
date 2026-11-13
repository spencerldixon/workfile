// Package cli provides wf commands and their terminal presentation.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"workfile/internal/provider"
	"workfile/internal/snapshot"
	"workfile/internal/workfile"
)

// helpText is the text of wf --help, ending with what to do next from dir.
func helpText(dir string) string {
	return `wf - where your work really is, and what it needs

Usage: wf <command> [options]

  setup                       Connect your tools and write .workfile/
  tracker                     Your tracker, scoped to the team: browse and read tickets
  status [TICKET ...]         Where work is, and what it needs
  check TICKET --to STAGE     May this ticket move now? (exit 0 yes, 1 no, 2 cannot tell)
  lint                        Is the policy sound? Which gates guard each stage

Options for status:
  --me                        Your work: assigned to you, or PRs you opened or must review
  --user NAME                 The same for someone in people.yml
  --since last|12h|1d         What changed since an earlier run on this computer
  --gate NAME                 Tickets failing this gate
  --stage NAME                Tickets in this stage that are out of policy
  --failing                   Only tickets that need attention
  --limit N                   Check up to N tickets
  --all                       Check every ticket in scope
  --evidence                  Show the evidence for each requirement

Options for tracker:
  --view all|me|STAGE         Start on this view (left and right to switch)

Options:
  --test [NAME ...]           setup: test your connections, or the named ones
  --json                      Machine-readable output (status, check, lint)
  --dir DIR                   Find .workfile/ from DIR or its parents (default .)
  --version                   Print the installed version
  -h, --help                  Show this help

` + nextStep(dir) + "\n"
}

// nextStep is the closing line of the help: set up, or try status.
func nextStep(dir string) string {
	found, err := workfile.Find(dir)
	if err != nil {
		return "No .workfile/ here. Start with: wf setup"
	}
	shown := found
	if abs, err := filepath.Abs(dir); err == nil {
		if rel, err := filepath.Rel(abs, found); err == nil {
			shown = rel
		}
	}
	return "Using " + clean(shown) + ". Try: wf status --me"
}

type options struct {
	command, dir, user, gate, stage, since string
	keys                                   []string
	limit                                  int
	all, me, failing, help, version        bool
	json, evidence                         bool
	limitSet                               bool
	// check
	to string
	// tracker
	view string
	// setup
	test bool
	// note is printed to errOut when an old command name was used.
	note string
}

// limited is false when every ticket in scope is checked.
func (o options) limited() bool {
	mode := o.statusMode()
	return !o.all && len(o.keys) == 0 && ((mode != modeRollup && mode != modeListing) || o.limitSet)
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
	// connect opens the providers of a workspace. A noWorkspace command that
	// reads tickets, such as setup --test, calls it after loading one itself.
	connect func(*workfile.Workspace) source
}

// commands is every wf command. To add one, write <name>.go with its command
// value, list it here, and describe it in help.
var commands = map[string]command{
	"setup":   setupCommand,
	"tracker": trackerCommand,
	"status":  statusCommand,
	"check":   checkCommand,
	"lint":    lintCommand,
}

// alias keeps an old command name working for one release.
type alias struct {
	to   string
	set  func(*options) // flags the old name implied
	note string         // printed to errOut
}

var aliases = map[string]alias{
	"health":  {to: "status", note: "health is now status"},
	"test":    {to: "setup", set: func(o *options) { o.test = true }, note: "test is now setup --test"},
	"prove":   {to: "lint", note: "prove is now lint"},
	"tickets": {to: "tracker", note: "tickets is now tracker"},
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
		fmt.Fprint(out, helpText(o.dir))
		return 0
	}
	if o.note != "" {
		fmt.Fprintf(errOut, "wf: %s. The old name will stop working in a later release.\n", o.note)
	}
	c := commands[o.command]
	e := env{view: view, o: o, errOut: errOut, connect: connect}
	if !c.noWorkspace {
		if e.w, err = workfile.Load(o.dir); err != nil {
			return fail(err)
		}
		if c.ticketKeys {
			if e.o.keys, err = completeKeys(o.keys, e.w.TrackerKeys(), e.w.Providers[e.w.Policy.Tracker].KeyProject()); err != nil {
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
	fs.StringVar(&o.since, "since", "", "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&o.evidence, "evidence", false, "")
	fs.BoolVar(&o.all, "all", false, "")
	fs.BoolVar(&o.me, "me", false, "")
	fs.BoolVar(&o.failing, "failing", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	fs.StringVar(&o.to, "to", "", "")
	fs.StringVar(&o.view, "view", "", "")
	fs.BoolVar(&o.test, "test", false, "")
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
	if o.since != "" {
		if _, err := snapshot.ParseSince(o.since); err != nil {
			return o, err
		}
	}
	if a, ok := aliases[o.command]; ok {
		o.command, o.note = a.to, a.note
		if a.set != nil {
			a.set(&o)
		}
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
