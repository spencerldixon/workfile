package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"workfile/internal/workfile"
)

var inboxCommand = command{
	flags: []string{"dir"},
	args: func(o options) error {
		if len(o.keys) > 0 {
			return errors.New("inbox takes no positional arguments")
		}
		return nil
	},
	run: inbox,
}

// inbox answers "what happened to my work, and what wants me now?" It only
// reads; clearing a row changes inbox.json on this computer and nothing else.
func inbox(ctx context.Context, e env) (int, error) {
	settings, err := workfile.ReadInboxSettings(e.w)
	if err != nil {
		return 2, err
	}
	st := workfile.ReadInboxState(e.w)
	if !e.view.color || !isTerminal(os.Stdin) || !isTerminal(e.view.out) {
		// Piped or plain: one read, printed. Nothing is written, so a script
		// never clears a "new" marker before the person sees it.
		now := time.Now()
		finish := e.startProgramWork()
		data := loadInbox(ctx, e, st, nil, now, pollFull)
		finish()
		printInbox(e.view, inboxRows(data, st), now)
		return 0, nil
	}
	m := newInboxModel(ctx, e, settings, st)
	m.workflow.gfx = e.view.outputGraphics()
	if _, err := runSyncTUI(ctx, e.view, m); err != nil && ctx.Err() == nil {
		return 2, err
	}
	return 0, nil
}

// printInbox writes the rows as plain text: rows that need the person first,
// each with its web address on the next line.
func printInbox(v view, rows []workfile.InboxRow, now time.Time) {
	actions := 0
	for _, r := range rows {
		if r.Action() {
			actions++
		}
	}
	fmt.Fprintf(v.out, "Inbox %d\n\n", actions)
	if actions == 0 {
		fmt.Fprintln(v.out, "Nothing needs you.")
		if len(rows) > 0 {
			fmt.Fprintln(v.out)
		}
	}
	for _, r := range rows {
		id := firstOf(r.Ticket, r.PR)
		line := inboxSymbol(r.Kind) + " "
		if id != "" && !strings.HasPrefix(r.ID, "!") {
			line += clean(id) + "  "
		}
		line += clean(r.Text)
		if age := shortAge(r.Since, now); age != "" {
			line += "  " + age
		}
		fmt.Fprintln(v.out, line)
		if target := firstOf(r.PRURL, r.URL); target != "" {
			fmt.Fprintln(v.out, "  "+clean(target))
		}
	}
}
