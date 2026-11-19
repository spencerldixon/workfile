package workfile

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// TrackerSettings only changes how the existing team scope is presented.
// Zero values inherit defaults; explicit false for comments needs a pointer.
type TrackerSettings struct {
	DefaultView string         `yaml:"default_view"`
	Views       []TrackerView  `yaml:"views"`
	Columns     []string       `yaml:"columns"`
	Preview     TrackerPreview `yaml:"preview"`
}

type TrackerView struct {
	ID        string        `yaml:"id"`
	Title     string        `yaml:"title"`
	Layout    string        `yaml:"layout"`    // list (default) or board
	Attention string        `yaml:"attention"` // color (default), icons or off
	Filter    TrackerFilter `yaml:"filter"`
}

type TrackerFilter struct {
	Assignee  string   `yaml:"assignee"`
	Origins   []string `yaml:"origins"`
	States    []string `yaml:"states"`
	Attention string   `yaml:"attention"`
}

type TrackerPreview struct {
	Comments *bool  `yaml:"comments"`
	Workflow string `yaml:"workflow"`
}

func (s TrackerSettings) ShowComments() bool {
	return s.Preview.Comments == nil || *s.Preview.Comments
}

func (s TrackerSettings) WithDefaults() TrackerSettings {
	if s.DefaultView == "" && len(s.Views) > 0 {
		s.DefaultView = s.Views[0].ID
	}
	return mergeTrackerSettings(TrackerSettings{
		DefaultView: "all",
		Columns:     []string{"key", "title", "status", "assignee", "action"},
		Preview:     TrackerPreview{Workflow: "auto"},
	}, s)
}

// ReadTrackerSettings reads optional layers, lowest precedence first. It is
// called by tracker only, so presentation preferences do not change policy.
func ReadTrackerSettings(w *Workspace) (TrackerSettings, error) {
	personal, err := ReadPersonal()
	if err != nil {
		return TrackerSettings{}, err
	}
	shared, path, err := readRepoConfig(w)
	if err != nil {
		return TrackerSettings{}, err
	}
	s := TrackerSettings{}
	for _, layer := range []struct {
		path     string
		settings TrackerSettings
	}{
		{PersonalPath("config.yml"), personal.Tracker},
		{path, shared.Tracker},
		{WorkspaceSettingsPath(w.Policy.Name), w.Settings.Tracker},
	} {
		if err := validateTrackerLayer(layer.settings); err != nil {
			return TrackerSettings{}, fmt.Errorf("%s: tracker.%w", layer.path, err)
		}
		s = mergeTrackerSettings(s, layer.settings)
	}
	s = s.WithDefaults()
	if err := validateTrackerViews(s, w.Policy); err != nil {
		return TrackerSettings{}, fmt.Errorf("tracker configuration: %w", err)
	}
	return s, nil
}

func mergeTrackerSettings(base, override TrackerSettings) TrackerSettings {
	if override.DefaultView != "" {
		base.DefaultView = override.DefaultView
	}
	if override.Columns != nil {
		base.Columns = slices.Clone(override.Columns)
	}
	if override.Preview.Comments != nil {
		base.Preview.Comments = override.Preview.Comments
	}
	if override.Preview.Workflow != "" {
		base.Preview.Workflow = override.Preview.Workflow
	}
	base.Views = slices.Clone(base.Views)
	for _, view := range override.Views {
		at := slices.IndexFunc(base.Views, func(v TrackerView) bool { return v.ID == view.ID })
		if at < 0 {
			base.Views = append(base.Views, view)
		} else {
			base.Views[at] = view
		}
	}
	return base
}

func validateTrackerLayer(s TrackerSettings) error {
	if s.Views != nil && len(s.Views) == 0 {
		return fmt.Errorf("views must contain at least one view, or be omitted for built-in tabs")
	}
	if s.Columns != nil {
		seen := map[string]bool{}
		for _, column := range s.Columns {
			if !slices.Contains([]string{"key", "title", "status", "assignee", "action"}, column) || seen[column] {
				return fmt.Errorf("columns must contain unique names from key, title, status, assignee and action")
			}
			seen[column] = true
		}
		if !seen["key"] || !seen["title"] {
			return fmt.Errorf("columns must include key and title")
		}
	}
	if !slices.Contains([]string{"", "auto", "text", "off"}, s.Preview.Workflow) {
		return fmt.Errorf("preview.workflow must be auto, text or off")
	}
	seen := map[string]bool{}
	for _, view := range s.Views {
		if !workspaceNamePattern.MatchString(view.ID) || len(view.ID) > 64 || seen[view.ID] {
			return fmt.Errorf("views must have unique lowercase dash-separated ids, at most 64 characters")
		}
		seen[view.ID] = true
		if strings.TrimSpace(view.Title) == "" || strings.ContainsFunc(view.Title, unicode.IsControl) {
			return fmt.Errorf("views.title must be nonempty text on one line")
		}
		if !slices.Contains([]string{"", "list", "board"}, view.Layout) {
			return fmt.Errorf("views.layout must be list or board")
		}
		if !slices.Contains([]string{"", "color", "icons", "off"}, view.Attention) {
			return fmt.Errorf("views.attention must be color, icons or off")
		}
		if !slices.Contains([]string{"", "me", "unassigned", "all"}, view.Filter.Assignee) {
			return fmt.Errorf("views.filter.assignee must be me, unassigned or all")
		}
		origins := map[string]bool{}
		for _, origin := range view.Filter.Origins {
			if strings.TrimSpace(origin) == "" || strings.ContainsFunc(origin, unicode.IsControl) || origins[strings.ToLower(origin)] {
				return fmt.Errorf("views.filter.origins must contain unique, nonempty source names")
			}
			origins[strings.ToLower(origin)] = true
		}
		if !slices.Contains([]string{"", "needs-me", "waiting", "ready", "unknown"}, view.Filter.Attention) {
			return fmt.Errorf("views.filter.attention must be needs-me, waiting, ready or unknown")
		}
		states := map[string]bool{}
		for _, state := range view.Filter.States {
			if state == "" || states[state] {
				return fmt.Errorf("views.filter.states must contain unique, nonempty states")
			}
			states[state] = true
		}
	}
	return nil
}

func validateTrackerViews(s TrackerSettings, p Policy) error {
	ids := map[string]bool{"all": true, "me": true}
	for _, state := range p.States {
		ids[strings.ToLower(state)] = true
	}
	for _, view := range s.Views {
		if ids[view.ID] {
			return fmt.Errorf("views.id conflicts with a built-in tab; choose a different id")
		}
		ids[view.ID] = true
		for _, state := range view.Filter.States {
			if !slices.Contains(p.States, state) {
				return fmt.Errorf("views.filter.states must name states from policy.yml")
			}
		}
	}
	available := ids
	if s.Views != nil {
		available = map[string]bool{}
		for _, view := range s.Views {
			available[view.ID] = true
		}
	}
	if !available[strings.ToLower(s.DefaultView)] {
		return fmt.Errorf("default_view must name an available tab; when views is set, use a configured view id")
	}
	return nil
}
