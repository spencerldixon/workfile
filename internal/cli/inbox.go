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
	printInbox(e.view, e.inboxSettings.ScopedRows(inboxRows(data, st), e.inboxSettings.DefaultScope), now)
	return 0, nil
}

// printInbox writes newest-first alerts and their web addresses.
func printInbox(v view, rows []workfile.InboxRow, now time.Time) {
	fmt.Fprintf(v.out, "Inbox %d\n\n", len(rows))
	if len(rows) == 0 {
		fmt.Fprintln(v.out, "No alerts.")
	}
	for _, r := range rows {
		id := inboxKey(r)
		line := inboxSymbol(r.Kind) + " "
		if id != "" && !strings.HasPrefix(r.ID, "!") {
			line += clean(id) + "  "
		}
		if r.Title != "" {
			line += clean(r.Title) + "  "
		}
		line += clean(r.Text)
		if r.ReadyReason != "" {
			line += " · " + clean(r.ReadyReason)
		}
		if age := shortAge(r.Latest(), now); age != "" {
			line += "  " + age
		}
		fmt.Fprintln(v.out, line)
		if r.Ticket != "" && trackerWebURL(r.URL) {
			fmt.Fprintln(v.out, "  "+clean(r.URL))
		}
		if target := inboxPRTarget(r); target != "" && (r.Ticket == "" || target != r.URL) {
			fmt.Fprintln(v.out, "  "+clean(target))
		}
	}
}
