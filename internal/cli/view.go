package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
	"workfile/internal/workfile"
)

type view struct {
	out   io.Writer
	color bool
	width int
	bg    string // SGR background kept alive across styled text, set inside a lane
	tint  *tintCache
}

func newView(out io.Writer) view {
	v := view{out: out, width: 100, tint: &tintCache{}}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		_, noColor := os.LookupEnv("NO_COLOR")
		v.color = !noColor && os.Getenv("TERM") != "dumb"
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			v.width = width
		}
	}
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width >= 20 {
		v.width = width
	}
	v.width = max(20, min(v.width, 140))
	return v
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, text)
}

func (v view) paint(code, text string) string {
	if !v.color {
		return text
	}
	reset := "\x1b[0m"
	if v.bg != "" {
		reset += "\x1b[" + v.bg + "m"
	}
	return "\x1b[" + code + "m" + text + reset
}

func (v view) link(text, target string) string {
	text = clean(text)
	u, err := url.Parse(target)
	if !v.color || err != nil || u.Scheme != "https" || u.Host == "" || clean(target) != target {
		return text
	}
	return "\x1b]8;;" + target + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func (v view) line(text string) { fmt.Fprintln(v.out, text) }

func (v view) heading(name string) {
	v.line("")
	v.line("  " + v.paint("1;36", "workfile") + v.paint("2", " / "+name))
	v.line("")
}

func (v view) wrap(text, indent string) {
	v.text(text, indent, "", "")
}

// Wrap before styling so ANSI colours and hyperlinks never affect layout width.
func (v view) text(text, indent, color, target string) {
	for _, line := range wrapped(text, max(1, v.width-runewidth.StringWidth(indent))) {
		if target != "" {
			line = v.link(line, target)
		}
		if color != "" {
			line = v.paint(color, line)
		}
		v.line(indent + line)
	}
}

func wrapped(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(clean(text)) {
		if line != "" && runewidth.StringWidth(line+" "+word) > width {
			lines = append(lines, line)
			line = ""
		}
		parts := strings.Split(runewidth.Wrap(word, width), "\n")
		for i, part := range parts {
			if line != "" {
				line += " "
			}
			line += part
			if i < len(parts)-1 {
				lines = append(lines, line)
				line = ""
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func quantity(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func group(a workfile.Assessment) int {
	if a.Ticket.Error != "" {
		return 0
	}
	if a.Violation() {
		return 1
	}
	if a.Blocked() {
		return 2
	}
	if len(a.Routes) > 0 {
		return 3
	}
	return 4
}

func (v view) report(w *workfile.Workspace, all []workfile.Assessment, o options, who map[string]string) {
	if o.command == "health" {
		if o.listing() {
			v.healthList(w, all, o)
		} else if len(all) == 0 {
			v.heading("health")
			v.wrap("No tickets match this selection.", "  ")
			v.line("")
		} else {
			v.healthSummary(w, all, o)
		}
		return
	}
	if len(o.keys) > 0 {
		shown := slices.DeleteFunc(slices.Clone(all), func(a workfile.Assessment) bool { return o.failing && group(a) >= 3 })
		if len(shown) > 0 {
			for _, a := range shown {
				v.details(w, a)
			}
			return
		}
	}
	switch {
	case len(all) == 0:
		v.heading("status")
		v.wrap("No tickets match this selection.", "  ")
		v.line("")
	case o.me || o.user != "":
		v.meBoard(w, all, o, who)
	default:
		v.statusBoard(w, all, o)
	}
}
