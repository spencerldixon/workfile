package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"workfile/internal/workfile"
)

func TestTrackerBoardProjectPickerIsACenteredModal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60, 120} {
		d := driveWithSettings(t, boardSource(), width, 30, graphics{}, options{}, boardSettings())
		board := d.screen()
		d.press("s")
		if !d.m.pickingProject {
			t.Fatal("s did not open the scope modal")
		}
		w, h := d.m.projectModalSize()
		x, y := (width-w)/2, (d.height-h)/2
		lines := strings.Split(d.screen(), "\n")
		if strings.TrimRight(lines[0], " ") != strings.TrimRight(strings.Split(board, "\n")[0], " ") {
			t.Fatal("modal replaced the board instead of overlaying it")
		}
		title := lines[y+1]
		at := strings.Index(title, "Choose a project")
		if at < 0 || ansi.StringWidth(title[:at]) != x+2 {
			t.Fatalf("modal is not centered: %q, want title at %d,%d", title, x+2, y+1)
		}
		if strings.TrimSpace(ansi.Cut(lines[y], x, x+w)) != "" {
			t.Fatal("modal padding did not cover the board beneath it")
		}
		if d.m.projectPicker.Width() != w-4 {
			t.Fatal("picker did not fit inside modal padding")
		}
		row := ansi.Cut(lines[y+4], x+2, x+w-2)
		if !strings.HasPrefix(row, "▌ All projects") || strings.Contains(row, "[All projects]") || ansi.Strip(row) != row {
			t.Fatalf("plain selection must have a marker and no escapes: %q", row)
		}
		d.press("esc")
		if d.m.pickingProject || d.screen() != board {
			t.Fatal("cancel did not restore the board")
		}
		d.press("s")
		d.width, d.height = 40, 16
		d.send(tea.WindowSizeMsg{Width: 40, Height: 16})
		w, h = d.m.projectModalSize()
		x, y = (d.width-w)/2, (d.height-h)/2
		title = strings.Split(d.screen(), "\n")[y+1]
		at = strings.Index(title, "Choose a project")
		if at < 0 || ansi.StringWidth(title[:at]) != x+2 {
			t.Fatal("modal did not recenter on resize")
		}
	}
}

func TestTrackerScopeHighlightsTheWholeSelectedRow(t *testing.T) {
	for _, width := range []int{40, 60} {
		d := driveWithSettings(t, boardSource(), width, 30, graphics{}, options{}, boardSettings())
		d.m.t = newTheme(true, "")
		d.press("s")
		for _, label := range []string{"All projects", "example-app"} {
			w, h := d.m.projectModalSize()
			x, y := (width-w)/2+2, (d.height-h)/2+4
			lines := strings.Split(d.screen(), "\n")
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("scope picker overflowed %d columns: %q", width, line)
				}
			}
			row := ansi.Cut(lines[y+d.m.projectPicker.Index()], x, x+w-4)
			if plain := ansi.Strip(row); !strings.HasPrefix(plain, "▌ "+label) {
				t.Fatalf("selection did not follow the cursor: %q", row)
			}
			if end := strings.LastIndex(row, "\x1b["); end < 0 || !strings.Contains(row[:end], ";7m") {
				t.Fatalf("missing reverse video on the selected row: %q", row)
			}
			if count := ansi.Cut(row, w-6, w-4); !strings.Contains(count, ";7m") {
				t.Fatalf("highlight did not reach the ticket count: %q", row)
			}
			d.press("down")
		}
	}
}

func TestTrackerBoardProjectAndPRShortcutsDoNotConflict(t *testing.T) {
	d := driveWithSettings(t, inboxSource(1), 100, 30, graphics{}, options{}, boardSettings())
	enableInbox(d)
	d.press("s")
	if !d.m.pickingProject || len(d.opened) != 0 {
		t.Fatal("s did not open scope selection independently of PRs")
	}
	d.press("esc", "p")
	if d.m.pickingProject || len(d.opened) != 1 {
		t.Fatal("p did not open the linked PR")
	}
	if help := d.m.keys.Project.Help(); help.Key != "s" || help.Desc != "scope" {
		t.Fatal("scope help is out of date")
	}
	for _, binding := range d.m.attentionKeys() {
		if help := binding.Help(); help.Desc == "PR" && help.Key != "p" {
			t.Fatal("PR help must advertise lowercase p")
		}
	}
}

func TestTrackerBoardProjectPickerFiltersAndReturnsToAll(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	initial := listKeys(d)
	d.press("s")
	d.wants("Choose a project", "All projects", "example-app", "example-web")
	d.press("down", "down", "enter")
	if d.m.pickingProject || listKeys(d) != "APP-5" {
		t.Fatalf("project filter ignored: %s", listKeys(d))
	}
	if !strings.Contains(d.m.masthead(), "example-web") {
		t.Fatal("active project is not shown")
	}
	d.press("s")
	if projects := d.m.projects(); len(projects) != 3 || projects[0].count != 5 {
		t.Fatalf("picker lost unselected projects: %+v", projects)
	}
	d.press("up", "up", "enter")
	if listKeys(d) != initial {
		t.Fatalf("All projects did not restore configured scope: %s", listKeys(d))
	}
	if f.listingReads != 1 || len(f.policyReads) != 0 || len(f.detailReads) != 0 {
		t.Fatal("changing project fetched provider data")
	}
}

func TestTrackerListProjectPickerUsesTheSameScopeControls(t *testing.T) {
	f := boardSource()
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	d.press("]")
	d.wants("s scope")
	d.press("s")
	d.wants("Choose a project", "example-web")
	d.press("down", "down", "enter")
	if listKeys(d) != "APP-5" || f.listingReads != 1 {
		t.Fatalf("list scope should use the cached listing, got %q after %d reads", listKeys(d), f.listingReads)
	}
	d.wants("example-web", "Ticket preview")
	d.press("[")
	if listKeys(d) == "APP-5" {
		t.Fatal("the list project selection affected the board tab")
	}
}

func TestTrackerBoardProjectSearchAndCancelDoNotMoveCards(t *testing.T) {
	d := driveWithSettings(t, boardSource(), 100, 30, graphics{}, options{}, boardSettings())
	d.press("right")
	selected := d.m.current
	d.press("s", "down", "esc")
	if d.m.current != selected || d.m.projectOrigins[d.m.shownView()] != "" {
		t.Fatal("cancel changed scope or card")
	}
	d.press("s", "/", "w", "e", "b", "enter")
	if listKeys(d) != "APP-5" || d.m.pickingProject {
		t.Fatalf("search did not select project: %s", listKeys(d))
	}
	// A late result from an old picker must never update a reopened picker.
	before := listKeys(d)
	d.press("s")
	index := d.m.projectPicker.Index()
	d.send(trackerProjectMsg{generation: d.m.projectGeneration - 1, msg: tea.KeyPressMsg{Code: tea.KeyUp}})
	if listKeys(d) != before || d.m.projectPicker.Index() != index {
		t.Fatal("stale picker message changed tickets or project selection")
	}
	d.press("esc")
	d.press("]")
	if d.m.shownView().board() || len(d.m.list.Items()) != 5 {
		t.Fatal("board scope leaked into list view")
	}
	d.press("[")
	if listKeys(d) != "APP-5" {
		t.Fatal("board project choice did not survive tab switch")
	}
}

func TestTrackerBoardProjectChoicesRespectConfiguredScope(t *testing.T) {
	s := boardSettings()
	s.Views[0].Filter.Origins = []string{"example-app"}
	f := boardSource()
	f.listing[0].AssigneeID = "example-alice"
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, s)
	d.press("s")
	projects := d.m.projects()
	if len(projects) != 2 || projects[0].count != 3 || projects[1].origin != "example-app" {
		t.Fatalf("picker broadened configured scope: %+v", projects)
	}
	d.press("enter")
	if got := listKeys(d); got != "APP-2 APP-3 APP-4" {
		t.Fatalf("All projects broadened scope: %s", got)
	}
}

func TestTrackerBoardProjectPickerFitsNarrowPlainTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, width := range []int{40, 60} {
		f := boardSource()
		f.listing[0].Origin = strings.Repeat("界example-", 10) + "\x1b[31m"
		d := driveWithSettings(t, f, width, 24, graphics{}, options{}, boardSettings())
		d.press("s", "down")
		if !strings.Contains(d.screen(), "Choose a project") {
			t.Fatal("picker not visible")
		}
		if strings.Contains(d.screen(), "\x1b") {
			t.Fatal("provider origin injected terminal escapes")
		}
		d.width, d.height = 40, 16
		d.send(tea.WindowSizeMsg{Width: 40, Height: 16})
		d.press("/", "n", "o", "n", "e")
		d.wants("No matching projects.")
		d.press("esc")
		if d.m.pickingProject {
			t.Fatal("escape did not close project picker")
		}
	}
}

func TestTrackerBoardProjectFilterCombinesWithTicketSearch(t *testing.T) {
	f := boardSource()
	f.listing[4].Title = "Improve login"
	d := driveWithSettings(t, f, 100, 30, graphics{}, options{}, boardSettings())
	d.press("/", "l", "o", "g", "i", "n", "enter")
	d.press("s", "down", "down", "enter")
	if listKeys(d) != "APP-5" {
		t.Fatalf("ticket and project filters did not combine: %s", listKeys(d))
	}
	// Filtering remains entirely independent of the provider kind.
	view := d.m.shownView()
	d.m.all = append(d.m.all, workfile.Listing{Key: "example#6", Title: "Improve login", State: "doing", AssigneeID: "example-bob", Origin: "EXAMPLE-WEB"})
	if len(d.m.baseTickets(view)) != 2 {
		t.Fatal("origin comparison should ignore case")
	}
}
