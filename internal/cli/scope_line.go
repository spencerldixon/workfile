package cli

import (
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// scopeText is the line that names what is read, such as
// "Jira · APP, WEB · repos web, api". It is cut to width, and has no escapes.
func scopeText(w *workfile.Workspace, width int) string {
	return runewidth.Truncate(clean(w.ScopeLine()), max(1, width), "…")
}

// scopeLines is the faint scope line as a list of zero or one lines, so a
// screen can add it under its title.
func (t theme) scopeLines(w *workfile.Workspace, width int) []string {
	if text := scopeText(w, width); text != "" {
		return []string{t.dim(text)}
	}
	return nil
}
