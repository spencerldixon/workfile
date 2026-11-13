package cli

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// pickAll is the key of the "everything" row some pickers start with. Picking
// it clears every other pick, and picking anything else clears it.
const pickAll = "*all*"

// pickItem is one row of a multi-select. The key is what setup writes.
type pickItem struct{ key, label string }

func (p pickItem) FilterValue() string { return p.key + " " + p.label }

// togglePick flips one key in the set. The all row and the other rows exclude
// each other.
func togglePick(set map[string]bool, key string) {
	if key == pickAll {
		wasOn := set[pickAll]
		clear(set)
		set[pickAll] = !wasOn
		if !set[pickAll] {
			delete(set, pickAll)
		}
		return
	}
	delete(set, pickAll)
	if set[key] {
		delete(set, key)
		return
	}
	set[key] = true
}

// chosenPicks is the picked keys in the order shown. The all row is not a key.
func chosenPicks(order []string, set map[string]bool) []string {
	var keys []string
	for _, key := range order {
		if key != pickAll && set[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

// anyPicked reports whether enter may continue: something, or all, is picked.
func anyPicked(set map[string]bool) bool { return len(set) > 0 }

type pickDelegate struct {
	t      theme
	picked map[string]bool
}

func (pickDelegate) Height() int                         { return 1 }
func (pickDelegate) Spacing() int                        { return 0 }
func (pickDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d pickDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	p, ok := item.(pickItem)
	if !ok {
		return
	}
	line := runewidth.Truncate(p.label, max(m.Width()-6, 4), "…")
	box := d.t.dim("[ ]")
	if d.picked[p.key] {
		box = d.t.good("[✓]")
	} else {
		line = d.t.dim(line)
	}
	if index == m.Index() {
		if d.picked[p.key] {
			line = d.t.bold(line)
		}
		fmt.Fprint(w, d.t.accent("› ")+box+" "+line)
		return
	}
	fmt.Fprint(w, "  "+box+" "+line)
}

// picker is a filterable list where space ticks rows. It is used for Jira
// projects and GitHub repositories.
type picker struct {
	list   list.Model
	picked map[string]bool
	order  []string
}

func newPicker(t theme, styles textinput.Styles, faint lipgloss.Style) picker {
	picked := map[string]bool{}
	l := list.New(nil, pickDelegate{t, picked}, 40, 6)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.KeyMap.ShowFullHelp.SetEnabled(false)
	l.KeyMap.CloseFullHelp.SetEnabled(false)
	l.Styles.Filter = styles
	l.Styles.NoItems = faint
	l.FilterInput.SetStyles(styles)
	return picker{list: l, picked: picked}
}

func (p *picker) setItems(items []pickItem) {
	rows := make([]list.Item, len(items))
	p.order = make([]string, len(items))
	for i, it := range items {
		rows[i], p.order[i] = it, it.key
	}
	clear(p.picked)
	p.list.SetItems(rows)
	p.list.ResetFilter()
	p.list.Select(0)
}

func (p *picker) filtering() bool { return p.list.FilterState() == list.Filtering }

// toggle ticks or unticks the row under the cursor.
func (p *picker) toggle() {
	if it, ok := p.list.SelectedItem().(pickItem); ok {
		togglePick(p.picked, it.key)
	}
}

func (p *picker) chosen() []string { return chosenPicks(p.order, p.picked) }

func (p *picker) size(width int) {
	p.list.SetSize(width, min(9, max(4, len(p.list.Items())+1))) // one row is the filter line
}

// key handles one key press. done is true when enter should continue. Space
// types into the filter while the filter is being typed.
func (p *picker) key(msg tea.KeyPressMsg) (cmd tea.Cmd, done bool) {
	k := msg.String()
	if !p.filtering() {
		switch k {
		case "space", " ":
			p.toggle()
			return nil, false
		case "enter":
			return nil, true
		}
	}
	p.list, cmd = p.list.Update(msg)
	return cmd, false
}
