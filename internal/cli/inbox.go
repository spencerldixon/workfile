package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"workfile/internal/workfile"
)

func trackerInboxReport(ctx context.Context, e env) (int, error) {
	now := time.Now()
	st := workfile.ReadInboxState(e.w)
	defer e.startProgramWork()()
	data := loadInbox(ctx, e, st, nil, now, pollFull)
	st.Observe(inboxInput(data, st), nil)
	printInbox(e.view, inboxRows(data, st), now)
	return 0, nil
}

// printInbox writes newest-first alerts and their web addresses.
func printInbox(v view, rows []workfile.InboxRow, now time.Time) {
	fmt.Fprintf(v.out, "Inbox %d\n\n", len(rows))
	if len(rows) == 0 {
		fmt.Fprintln(v.out, "No alerts.")
	}
	for _, r := range rows {
		id := firstOf(r.Ticket, r.PR)
		line := inboxSymbol(r.Kind) + " "
		if id != "" && !strings.HasPrefix(r.ID, "!") {
			line += clean(id) + "  "
		}
		line += clean(r.Text)
		if age := shortAge(r.Latest(), now); age != "" {
			line += "  " + age
		}
		fmt.Fprintln(v.out, line)
		if target := firstOf(r.PRURL, r.URL); target != "" {
			fmt.Fprintln(v.out, "  "+clean(target))
		}
	}
}
