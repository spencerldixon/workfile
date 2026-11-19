package cli

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"workfile/internal/provider"
	"workfile/internal/workfile"
)

type setupOption struct {
	kind, label string
	role        *workfile.Role
	form        provider.SetupForm
}

func setupOptions(tracker bool) []setupOption {
	var options []setupOption
	for _, key := range workfile.SortedKeys(workfile.Kinds) {
		kind := workfile.Kinds[key]
		role := kind.CodeHost
		if tracker {
			role = kind.Tracker
		}
		form, ok := provider.SetupFormFor(key, tracker)
		if role == nil || !ok {
			continue
		}
		label := kind.Label
		if role.Label != "" {
			label = role.Label
		}
		options = append(options, setupOption{key, label, role, form})
	}
	if !tracker {
		options = append(options, setupOption{label: "Skip for now"})
	}
	return options
}

type setupConnection struct {
	setupOption
	answers map[string]string
}

func (c setupConnection) provider(name, prefix string, values map[string]string) workfile.Provider {
	p := workfile.Provider{Kind: c.kind, Site: c.answers["site"], Org: c.answers["org"], Auth: map[string]string{}}
	for _, key := range workfile.Kinds[c.kind].Auth {
		variable := prefix + "_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_" + strings.ToUpper(key)
		values[variable] = c.answers[key]
		p.Auth[key] = "${" + variable + "}"
	}
	return p
}

func (m *setupModel) hostName() string {
	if m.host.kind == m.tracker.kind {
		return m.host.kind + "_code"
	}
	return m.host.kind
}

func (m *setupModel) connection() *setupConnection {
	switch m.step {
	case stepHostSettings, stepHostTest:
		return &m.host
	default:
		return &m.tracker
	}
}

func (m *setupModel) field() provider.SetupField {
	return m.connection().form.Fields[m.fieldIndex]
}

func (m *setupModel) showOptions(step setupStep) tea.Cmd {
	m.step, m.err = step, ""
	options, selected := m.trackerOptions, m.tracker.kind
	if step == stepCodeHostKind {
		options, selected = m.hostOptions, m.host.kind
	}
	items := make([]pickItem, len(options))
	index := 0
	for i, choice := range options {
		items[i] = pickItem{key: choice.kind, label: choice.label}
		if choice.kind == selected {
			index = i
		}
	}
	m.providerPick.setItems(items)
	m.providerPick.list.Select(index)
	m.resize()
	return nil
}

func (m *setupModel) chooseProvider() tea.Cmd {
	options, connection := m.trackerOptions, &m.tracker
	if m.step == stepCodeHostKind {
		options, connection = m.hostOptions, &m.host
	}
	if len(options) == 0 {
		return nil
	}
	picked := options[m.providerPick.list.Index()]
	if picked.kind != connection.kind {
		*connection = setupConnection{setupOption: picked, answers: map[string]string{}}
		m.invalidateScope(connection)
	}
	if m.step == stepTrackerKind {
		return m.showOptions(stepCodeHostKind)
	}
	return m.focus(stepFolder, 0)
}

func (m *setupModel) focus(step setupStep, index int) tea.Cmd {
	m.step, m.fieldIndex, m.err, m.loading, m.opened = step, index, "", false, ""
	in := &m.input
	in.Reset()
	in.EchoMode = textinput.EchoNormal
	if step == stepFolder {
		in.SetValue(m.dir)
		in.Placeholder = ""
	} else {
		field := m.field()
		in.SetValue(m.connection().answers[field.Key])
		in.Placeholder = field.Placeholder
		if field.Key == "token" {
			in.EchoMode, in.EchoCharacter = textinput.EchoPassword, '*'
			in.Placeholder = "Paste your token"
		}
	}
	in.CursorEnd()
	m.resize()
	return tea.Batch(in.Focus(), textinput.Blink)
}

func (m *setupModel) lastField(step setupStep) tea.Cmd {
	connection := &m.tracker
	if step == stepHostSettings {
		connection = &m.host
	}
	return m.focus(step, len(connection.form.Fields)-1)
}

func (m *setupModel) openPage() tea.Cmd {
	target := m.connection().form.TokenPage
	open := m.deps.open
	return func() tea.Msg { return openedMsg{err: open(target)} }
}

func (m *setupModel) submit() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		m.err = "A value is required."
		if m.isToken() {
			m.err = "Paste your token to continue."
		}
		return nil
	}
	if m.step == stepFolder {
		dir, err := setupDirectory(value)
		if err != nil {
			m.err = err.Error()
			return nil
		}
		m.dir = dir
		return m.focus(stepTrackerSettings, 0)
	}
	field := m.field()
	value, err := field.Validate(value)
	if err != nil {
		m.err = capitalise(err.Error()) + "."
		return nil
	}
	connection := m.connection()
	if connection.answers[field.Key] != value {
		connection.answers[field.Key] = value
		m.invalidateScope(connection)
	}
	if m.fieldIndex+1 < len(connection.form.Fields) {
		return m.focus(m.step, m.fieldIndex+1)
	}
	if m.step == stepTrackerSettings {
		return m.test(stepTrackerTest)
	}
	return m.test(stepHostTest)
}

func (m *setupModel) invalidateScope(connection *setupConnection) {
	if connection == &m.tracker {
		m.haveProjects, m.haveTrackerRepos, m.statusFor = false, false, ""
		m.projects, m.trackerRepos, m.statuses, m.items = nil, nil, nil, nil
		return
	}
	m.haveRepos, m.repos = false, nil
}
