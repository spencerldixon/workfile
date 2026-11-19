package cli

import (
	"context"
	"fmt"
	"image"
	"io"
	"slices"
	"strings"
	"time"

	bubblehelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"workfile/internal/workfile"
)

// ticketItem is one row of the list. Search looks at everything the row shows.
type ticketItem struct{ workfile.Listing }

func (i ticketItem) FilterValue() string {
	return i.Key + " " + i.Title + " " + i.Assignee + " " + i.Status
}

type ticketRow struct{ m *trackerModel }

func (ticketRow) Height() int                         { return 1 }
func (ticketRow) Spacing() int                        { return 0 }
func (ticketRow) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d ticketRow) Render(w io.Writer, l list.Model, index int, item list.Item) {
	fmt.Fprint(w, d.m.row(item.(ticketItem), index == l.Index(), l.MatchesForItem(index)))
}

// ticketRead is what has been read about one ticket. Replies carry their read,
// so a reply to a read that was dropped or refreshed is ignored.
type (
	ticketListMsg struct {
		all []workfile.Listing
		me  string
		err error
	}
	ticketMeMsg struct {
		me  string
		err error
	}
	ticketOpenedMsg struct{ err error }
	ticketImageMsg  struct {
		attachment string
		img        image.Image
		err        error
	}
	ticketTitleMsg struct {
		key        string
		width      int
		img        image.Image
		cols, rows int
		ok         bool
	}
)

// ticketPicture is an image the terminal holds. Its pixels are not kept: the
// terminal has them, and a resize needs only the size. width is the text width
// a title was drawn for.
type ticketPicture struct {
	picture
	size  image.Point
	width int
}

const cannotFindMe = "Cannot tell who you are. Check your connection with wf setup --test."

// wideTickets is the width from which the ticket and Workfile's view sit side
// by side.
const wideTickets = 110

type ticketKeys struct {
	Move, View, Search, Read, Focus, Refresh, Open, Quit, Keep, Clear, PR, Checks, More, Project key.Binding
	Workfile                                                                                     key.Binding
}

// trackerModel is the `wf tracker` screen: a list of the scope above the
// selected ticket. The list is read once. A ticket is read only after the
// selection rests on it, and then kept for the session.
type trackerModel struct {
	ticketReads                           trackerReads
	refresh                               trackerRefresh
	ctx                                   context.Context
	e                                     env
	v                                     view
	t                                     theme
	boardTheme                            theme
	boardSelectedTheme                    theme
	panelTheme                            theme
	tagPalette                            ticketTagPalette
	tabFocus                              bool // up from the top card focuses the view tabs
	headings                              map[ticketHeading]*ticketPicture
	headingTried                          map[ticketHeading]bool
	flows                                 map[flowSpec]*ticketPicture
	flowTried                             map[flowSpec]bool
	projectOrigins                        map[ticketView]string
	projectPicker                         list.Model
	pickingProject                        bool
	pickingPR                             bool
	prTicket                              string
	projectGeneration                     int
	list                                  list.Model
	pane                                  viewport.Model // the ticket
	side                                  viewport.Model // Workfile's view, on wide terminals
	spin                                  spinner.Model
	help                                  bubblehelp.Model
	keys                                  ticketKeys
	width, height                         int
	loaded                                bool
	all                                   []workfile.Listing
	total                                 int // tickets in the view before any search
	view                                  int // index into views()
	me                                    string
	findingMe                             bool
	counts                                map[ticketView]int
	keyWidth                              int
	statusWidth                           int
	onTicket                              bool // focus is on the ticket, not the list
	readerHeader                          []string
	readerWorkfile                        bool
	spinning                              bool
	current                               string // the ticket on show
	notice                                string
	fatal                                 error
	gfx                                   graphics
	images                                map[string]*ticketPicture // by attachment
	titles                                map[string]*ticketPicture // by ticket key
	tried                                 map[string]bool           // attachments already asked for
	nextID                                int
	open                                  func(string) error
	now                                   func() time.Time
	overview                              bool
	who                                   map[string]string
	whoStarted, whoIncomplete, whoLoading bool
	checkBudgets                          map[ticketView]int
}

func newTrackerModel(ctx context.Context, e env) *trackerModel {
	if e.view.tint == nil {
		e.view.tint = &tintCache{}
	}
	e.tracker = e.tracker.WithDefaults()
	t := newTheme(e.view.color, "")
	m := &trackerModel{ctx: ctx, e: e, v: e.view, t: t, width: e.view.width, height: 24, ticketReads: trackerReads{byKey: map[string]*ticketRead{}, pause: 200 * time.Millisecond}, counts: map[ticketView]int{}, images: map[string]*ticketPicture{}, titles: map[string]*ticketPicture{}, tried: map[string]bool{}, nextID: firstImageID, now: time.Now, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)), pane: viewport.New(), side: viewport.New(), help: t.help()}
	m.overview = attentionEnabled(e.tracker)
	m.boardTheme, m.boardSelectedTheme = t, t
	if slices.ContainsFunc(e.tracker.Views, func(v workfile.TrackerView) bool { return v.Layout == "board" }) {
		m.boardTheme = newTheme(e.view.color, e.view.laneBackground())
		m.boardSelectedTheme = newTheme(e.view.color, e.view.selectedLaneBackground())
	}
	m.panelTheme = newTheme(e.view.color, e.view.laneBackground())
	m.headings = map[ticketHeading]*ticketPicture{}
	m.headingTried = map[ticketHeading]bool{}
	m.flows, m.flowTried = map[flowSpec]*ticketPicture{}, map[flowSpec]bool{}
	m.who = map[string]string{}
	m.checkBudgets = map[ticketView]int{}
	m.projectOrigins = map[ticketView]string{}
	m.open = func(target string) error { return openBrowser(ctx, target) }
	m.keys = ticketKeys{
		Move:     key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Read:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "read")),
		Focus:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "list")),
		View:     key.NewBinding(key.WithKeys("left", "right", "[", "]"), key.WithHelp("←→", "view")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Open:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
		Quit:     key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Keep:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep")),
		Clear:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
		PR:       key.NewBinding(key.WithKeys("p", "P"), key.WithHelp("p", "PR")),
		Checks:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "checks")),
		More:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "check more")),
		Project:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "scope")),
		Workfile: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workfile")),
	}
	m.help.ShortSeparator = " · "
	start, _ := pickView(e.o, e.w.Policy, e.tracker)
	m.view = max(0, slices.Index(m.views(), start))
	m.list = list.New(nil, ticketRow{m}, 0, 0)
	m.list.SetShowTitle(false)
	m.list.SetShowFilter(false) // the header shows the search
	m.list.SetShowStatusBar(false)
	m.list.SetShowPagination(false)
	m.list.SetShowHelp(false)
	m.list.DisableQuitKeybindings()
	m.list.KeyMap.ShowFullHelp.SetEnabled(false)
	m.list.KeyMap.CloseFullHelp.SetEnabled(false)
	m.layout()
	return m
}

func (m *trackerModel) Init() tea.Cmd {
	e, ctx, wantsMe := m.e, m.ctx, m.shownView().me
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		all, err := e.client.Listing(ctx, "")
		if err != nil || !wantsMe {
			return ticketListMsg{all: all, err: err}
		}
		me, err := trackerIdentity(ctx, e)
		return ticketListMsg{all, me, err}
	})
}

func (m *trackerModel) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}

func (m *trackerModel) busy() bool {
	if m.refresh.syncing || !m.loaded || m.findingMe {
		return true
	}
	if m.overview {
		if m.whoLoading {
			return true
		}
		for _, r := range m.ticketReads.byKey {
			if r.policyStarted && !r.policyDone() {
				return true
			}
		}
	}
	r := m.ticketReads.byKey[m.current]
	return r != nil && r.pending()
}

func (m *trackerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(20, msg.Width), max(10, msg.Height)
		m.v.width = m.width
		resized := m.resizeImages()
		m.layout()
		item, _ := m.selected()
		return m, tea.Batch(resized, m.loadTitle(item), m.loadPreviewPictures(), m.pumpChecks())
	case ticketListMsg:
		return m, m.listed(msg)
	case ticketSettledMsg:
		if msg.gen != m.ticketReads.generation {
			return m, nil
		}
		item, _ := m.selected()
		if m.showingBoard() {
			return m, m.pumpChecks()
		}
		return m, tea.Batch(m.read(msg.key), m.loadTitle(item))
	case ticketDetailMsg:
		if m.ticketReads.receivedDetail(msg) && msg.read == m.ticketReads.byKey[m.current] {
			m.fill(false)
			if msg.err == nil {
				return m, tea.Batch(m.loadImages(msg.detail), m.loadPreviewPictures())
			}
		}
		return m, nil
	case ticketPolicyMsg:
		if !m.ticketReads.receivedPolicy(msg, m.now()) {
			return m, nil
		}
		if msg.read != m.ticketReads.byKey[m.current] {
			if m.overview {
				return m, m.showTickets()
			}
			return m, nil
		}
		more := tea.Batch(m.loadWorkflow(), m.loadFailedChecks())
		if m.overview {
			return m, tea.Batch(m.showTickets(), more)
		}
		m.fill(false)
		return m, more
	case ticketActionIdentityMsg:
		m.who, m.whoIncomplete, m.whoLoading = msg.who, msg.incomplete, false
		if msg.incomplete {
			m.notice = "Some action owners could not be established."
		}
		if m.me == "" {
			m.me = m.who[m.e.w.Policy.Tracker]
		}
		return m, m.showTickets()
	case ticketMeMsg:
		m.findingMe = false
		if msg.err != nil {
			m.notice = cannotFindMe
			return m, nil
		}
		m.me = msg.me
		m.who[m.e.w.Policy.Tracker] = msg.me
		return m, m.showTickets()
	case ticketImageMsg:
		return m, m.showImage(msg)
	case ticketTitleMsg:
		return m, m.showTitle(msg)
	case ticketHeadingMsg:
		return m, m.showHeading(msg)
	case workflowPictureMsg:
		return m, m.showWorkflow(msg)
	case failedChecksMsg:
		m.showFailedChecks(msg)
		return m, nil
	case trackerSyncScheduleMsg:
		return m, m.refresh.timer(msg, m.now())
	case trackerSyncDueMsg:
		return m, m.syncDue(msg)
	case trackerSyncedMsg:
		return m, m.synced(msg)
	case trackerPRsOpenedMsg:
		m.notice = m.prOpenNotice(msg)
		return m, nil
	case ticketOpenedMsg:
		m.notice = ""
		if msg.err != nil {
			m.notice = "Cannot open the browser."
		}
		return m, nil
	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case trackerProjectMsg:
		if (m.pickingProject || m.pickingPR) && msg.generation == m.projectGeneration {
			return m, m.updateProjectPicker(msg.msg)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, m.updateList(msg)
}

func (m *trackerModel) listed(msg ticketListMsg) tea.Cmd {
	if msg.err != nil {
		m.fatal = msg.err
		return tea.Quit
	}
	m.loaded, m.all, m.me = true, msg.all, msg.me
	for _, t := range msg.all {
		m.keyWidth = max(m.keyWidth, runewidth.StringWidth(clean(t.Key)))
		m.statusWidth = min(14, max(m.statusWidth, runewidth.StringWidth(clean(t.Status))))
	}
	if msg.me != "" {
		m.who[m.e.w.Policy.Tracker] = msg.me
	}
	return tea.Batch(m.showTickets(), m.actionIdentity(), m.refresh.markSynced(m.now()))
}

func (m *trackerModel) views() []ticketView {
	return ticketViews(m.e.w.Policy, m.e.tracker)
}

func (m *trackerModel) shownView() ticketView { return m.views()[m.view] }

// showingBoard reports whether cards are visible. A board view can also show
// the ticket reader while keeping the board tab selected.
func (m *trackerModel) showingBoard() bool {
	return m.shownView().board() && !m.onTicket
}

// findMe asks who "me" is the first time Assigned to me needs it, and never if it
// never does. A failed answer is asked again on the next visit.
func (m *trackerModel) findMe() tea.Cmd {
	if !m.shownView().me || m.me != "" || m.findingMe || m.whoLoading {
		return nil
	}
	m.findingMe, m.notice = true, ""
	e, ctx := m.e, m.ctx
	return func() tea.Msg {
		me, err := trackerIdentity(ctx, e)
		return ticketMeMsg{me, err}
	}
}

// showTickets fills the list, staying on the current ticket if it is still there.
func (m *trackerModel) showTickets() tea.Cmd {
	shown := m.shownTickets(m.shownView())
	m.total = len(shown)
	for _, view := range m.views() {
		m.counts[view] = len(m.shownTickets(view))
	}
	items := make([]list.Item, len(shown))
	at := 0
	for i, t := range shown {
		items[i] = ticketItem{t}
		if t.Key == m.current {
			at = i
		}
	}
	cmd := m.list.SetItems(items)
	if m.list.FilterState() == list.Unfiltered {
		m.list.Select(at)
	}
	m.layout()
	return tea.Batch(cmd, m.follow(), m.pumpChecks())
}

// updateList passes a message to the list, then follows the selection, which a
// key or a search result may have moved.
func (m *trackerModel) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return tea.Batch(cmd, m.follow(), m.pumpChecks())
}

func (m *trackerModel) selected() (ticketItem, bool) {
	item, ok := m.list.SelectedItem().(ticketItem)
	return item, ok
}

// follow shows the selected ticket at once, with what is known, and waits for
// the selection to rest before reading it, so moving down the list reads
// nothing. A read still running for the ticket left behind is cancelled.
func (m *trackerModel) follow() tea.Cmd {
	item, ok := m.selected()
	if item.Key == m.current {
		return nil
	}
	m.ticketReads.selectionChanged(m.current, m.overview)
	m.current = item.Key
	m.readerWorkfile = false
	m.fill(true)
	if !ok {
		return nil
	}
	if r := m.ticketReads.byKey[item.Key]; r != nil && r.detailStarted {
		return tea.Batch(m.loadTitle(item), m.loadPreviewPictures())
	}
	return m.ticketReads.settle(item.Key)
}

// read starts the reads for one ticket: its text from the tracker, and the policy's
// view of it. Each draws when it arrives.
func (m *trackerModel) read(ticket string) tea.Cmd {
	cmds := m.ticketReads.start(m.ctx, m.e, ticket)
	if len(cmds) == 0 {
		return m.loadPreviewPictures()
	}
	m.fill(false)
	return tea.Batch(append(cmds, m.startSpinner(), m.loadPreviewPictures())...)
}

func (m *trackerModel) key(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	if m.pickingProject || m.pickingPR {
		return m.projectKey(msg)
	}
	// While searching, every letter belongs to the search.
	if m.list.SettingFilter() {
		return m.updateList(msg)
	}
	m.notice = ""
	if m.showingBoard() {
		switch msg.String() {
		case "left", "right", "up", "down":
			if m.tabFocus {
				if msg.String() == "down" {
					m.tabFocus = false
					return nil
				}
				if msg.String() == "up" {
					return nil
				}
				return m.switchView(msg.String() == "left")
			}
			if msg.String() == "up" && m.atBoardTop() {
				m.tabFocus = true
				return nil
			}
			return m.moveCard(msg.String())
		case "esc", "enter":
			if m.tabFocus {
				m.tabFocus = false
				return nil
			}
		}
	}
	item, ok := m.selected()
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Project) && !m.onTicket:
		return m.openProjectPicker()
	case key.Matches(msg, m.keys.Focus):
		m.setReading(ok && !m.onTicket)
		if m.onTicket && m.shownView().board() {
			return tea.Batch(m.read(item.Key), m.loadTitle(item))
		}
		return nil
	case key.Matches(msg, m.keys.Read) && !m.onTicket:
		m.setReading(ok)
		if ok && m.shownView().board() {
			return tea.Batch(m.read(item.Key), m.loadTitle(item))
		}
		return nil
	case m.onTicket && key.Matches(msg, m.keys.Workfile):
		m.readerWorkfile = !m.readerWorkfile
		return nil
	case key.Matches(msg, m.keys.View):
		return m.switchView(msg.String() == "left" || msg.String() == "[")
	case key.Matches(msg, m.keys.Open):
		if !ok {
			return nil
		}
		open, target := m.open, item.URL
		return func() tea.Msg { return ticketOpenedMsg{open(target)} }
	case key.Matches(msg, m.keys.PR):
		return m.openPullRequests()
	case key.Matches(msg, m.keys.Checks):
		target := m.actionURL(true)
		if target == "" {
			m.notice = "No matching PR or checks link is available."
			return nil
		}
		open := m.open
		return func() tea.Msg { return ticketOpenedMsg{open(target)} }
	case key.Matches(msg, m.keys.More):
		if !m.overview {
			return nil
		}
		view := m.shownView()
		budget := m.checkBudgets[view]
		if budget == 0 {
			budget = trackerCheckBatch
		}
		m.checkBudgets[view] = budget + trackerCheckBatch
		return m.pumpChecks()
	case key.Matches(msg, m.keys.Refresh):
		if m.showingBoard() {
			return m.syncTracker()
		}
		if !ok {
			if m.overview {
				return m.refreshAttentionSample()
			}
			return nil
		}
		m.ticketReads.discard(item.Key)
		// An image that failed gets another try when the ticket is read again.
		for attachment := range m.tried {
			if m.images[attachment] == nil {
				delete(m.tried, attachment)
			}
		}
		var refresh tea.Cmd
		if m.showingBoard() {
			refresh = tea.Batch(m.ticketReads.ensure(m.ctx, item.Key).checkPolicy(m.e, item.Key), m.startSpinner())
		} else {
			refresh = m.read(item.Key)
		}
		if m.overview && m.whoIncomplete {
			m.whoStarted = false
			return tea.Batch(refresh, m.actionIdentity())
		}
		return refresh
	case key.Matches(msg, m.keys.Search):
		m.setReading(false)
	}
	// Number keys open a fix whenever the Workfile pane is on screen.
	if n, isFix := fixKey(msg); isFix && ok && !m.showingBoard() {
		return m.openFix(n)
	}
	if !m.onTicket {
		return m.updateList(msg)
	}
	if key.Matches(msg, m.keys.Clear) {
		m.setReading(false)
		return nil
	}
	var cmd tea.Cmd
	if m.readerWorkfile {
		m.side, cmd = m.side.Update(msg)
		return cmd
	}
	m.pane, cmd = m.pane.Update(msg)
	return cmd
}

// quit frees the terminal's copies of the images on the way out.
func (m *trackerModel) quit() tea.Cmd {
	m.ticketReads.cancelAll()
	if held := m.forgetAll(); held != "" {
		return tea.Sequence(tea.Raw(held), tea.Quit)
	}
	return tea.Quit
}

func (m *trackerModel) forgetAll() string {
	var b strings.Builder
	for _, held := range []map[string]*ticketPicture{m.images, m.titles} {
		for _, p := range held {
			b.WriteString(forget(p.id))
		}
	}
	for _, p := range m.headings {
		b.WriteString(forget(p.id))
	}
	for _, p := range m.flows {
		b.WriteString(forget(p.id))
	}
	return b.String()
}

// allocate gives the next image id. Ids run out at lastImageID and start again,
// which drops the image that held the id, so it is read again if it comes back.
func (m *trackerModel) allocate() int {
	id := m.nextID
	m.nextID++
	if m.nextID > lastImageID {
		m.nextID = firstImageID
	}
	for attachment, p := range m.images {
		if p.id == id {
			delete(m.images, attachment)
			delete(m.tried, attachment)
		}
	}
	for ticket, p := range m.titles {
		if p.id == id {
			delete(m.titles, ticket)
		}
	}
	for heading, p := range m.headings {
		if p.id == id {
			delete(m.headings, heading)
			delete(m.headingTried, heading)
		}
	}
	for spec, p := range m.flows {
		if p.id == id {
			delete(m.flows, spec)
			delete(m.flowTried, spec)
		}
	}
	return id
}

// loadImages reads the images of one ticket, only when the terminal can show
// them. An image that fails stays a named link.
func (m *trackerModel) loadImages(d workfile.Detail) tea.Cmd {
	if !m.gfx.on {
		return nil
	}
	blocks := slices.Clone(d.Body)
	if m.e.tracker.ShowComments() {
		blocks = append(blocks, commentText(d.Comments)...)
	}
	var cmds []tea.Cmd
	for _, b := range blocks {
		if b.Kind != workfile.BlockImage || b.Attachment == "" || m.tried[b.Attachment] || len(cmds) == maxTicketImages {
			continue
		}
		m.tried[b.Attachment] = true
		client, ctx, attachment := m.e.client, m.ctx, b.Attachment
		cmds = append(cmds, func() tea.Msg {
			data, err := client.Image(ctx, attachment)
			if err != nil {
				return ticketImageMsg{attachment: attachment, err: err}
			}
			img, err := decodeImage(data, maxImagePixels)
			return ticketImageMsg{attachment, img, err}
		})
	}
	return tea.Batch(cmds...)
}

func commentText(comments []workfile.Comment) []workfile.Block {
	var blocks []workfile.Block
	for _, c := range comments {
		blocks = append(append(blocks, c.Body...), commentText(c.Replies)...)
	}
	return blocks
}

func (m *trackerModel) showImage(msg ticketImageMsg) tea.Cmd {
	if msg.err != nil || m.images[msg.attachment] != nil {
		return nil
	}
	size := msg.img.Bounds().Size()
	cols, rows := fitCells(size.X, size.Y, m.gfx, m.imageWidth())
	p := &ticketPicture{picture: picture{m.allocate(), cols, rows}, size: size}
	m.images[msg.attachment] = p
	m.fill(false)
	return sendPicture(msg.img, p.picture)
}

// sendPicture encodes off the update loop, because a large image takes a moment.
func sendPicture(img image.Image, p picture) tea.Cmd {
	return func() tea.Msg {
		sequence, err := transmit(img, p)
		if err != nil {
			return nil
		}
		return tea.RawMsg{Msg: sequence}
	}
}

// loadTitle draws the large title for a ticket, when the terminal can show it
// and it is not drawn already for this width.
func (m *trackerModel) loadTitle(item ticketItem) tea.Cmd {
	width := m.textWidth()
	if !m.gfx.on || !m.gfx.hasInk || item.Key == "" || width < minTitleWidth {
		return nil
	}
	if p := m.titles[item.Key]; p != nil && p.width == width {
		return nil
	}
	g, title, ticket := m.gfx, item.Title, item.Key
	return func() tea.Msg {
		img, cols, rows, ok := titleImage(title, g, width)
		return ticketTitleMsg{ticket, width, img, cols, rows, ok}
	}
}

func (m *trackerModel) showTitle(msg ticketTitleMsg) tea.Cmd {
	if !msg.ok {
		return nil
	}
	p := m.titles[msg.key]
	if p == nil {
		p = &ticketPicture{picture: picture{id: m.allocate()}}
		m.titles[msg.key] = p
	}
	p.cols, p.rows, p.width = msg.cols, msg.rows, msg.width
	m.fill(false)
	return sendPicture(msg.img, p.picture)
}

// resizeImages gives each image the cells it fills at the new width. The
// terminal keeps the image itself, so only the placement changes.
func (m *trackerModel) resizeImages() tea.Cmd {
	var b strings.Builder
	for _, p := range m.images {
		cols, rows := fitCells(p.size.X, p.size.Y, m.gfx, m.imageWidth())
		if cols != p.cols || rows != p.rows {
			p.cols, p.rows = cols, rows
			b.WriteString(place(p.picture))
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return tea.Raw(b.String())
}

// layout gives the list the top half and the ticket the rest, edge to edge. The
// masthead, tabs, column headings, preview heading and help stay outside it.
func (m *trackerModel) layout() {
	if m.pickingProject || m.pickingPR {
		m.resizeProjectPicker()
	}
	m.list.SetSize(m.width, m.listRows())
	m.help.SetWidth(m.width - len(ticketIndent))
	m.fill(false)
}

// columns splits the ticket on wide terminals: its text at a comfortable
// length, with the rest of the width for Workfile's view, which stays in sight
// while the text scrolls. Narrow terminals stack them, so right is 0.
func (m *trackerModel) columns() (left, right int) {
	if m.width < wideTickets {
		return m.width, 0
	}
	left = min(m.width*3/5, 116)
	return left, m.width - left - 1
}

// topRows counts the masthead and tabs, plus space for headings in list views.
func (m *trackerModel) topRows() int {
	if m.shownView().board() {
		return trackerHeaderRows
	}
	return trackerHeaderRows + 2
}

// listRows is the top half, or less when there are few tickets, so the room
// goes to the ticket. It follows everything read, not the view or the search,
// so the screen does not jump.
func (m *trackerModel) listRows() int {
	if m.shownView().board() {
		return 0
	}
	half := max(1, (m.height-3-m.topRows())/2)
	if len(m.all) > 0 {
		return min(half, max(3, len(m.all)))
	}
	return half
}

// switchView moves to the previous or next view, wrapping at the ends.
func (m *trackerModel) switchView(back bool) tea.Cmd {
	count := len(m.views())
	step := 1
	if back {
		step = count - 1
	}
	wasBoard := m.shownView().board()
	m.view = (m.view + step) % count
	m.tabFocus = m.tabFocus && m.shownView().board()
	if wasBoard || m.shownView().board() {
		m.onTicket = false
		m.readerWorkfile = false
	}
	return tea.Batch(m.findMe(), m.showTickets())
}

// atBoardTop is true when up has no card above the selected one.
func (m *trackerModel) atBoardTop() bool {
	for _, l := range m.boardLanes() {
		if at := slices.Index(l.indices, m.list.Index()); at >= 0 {
			return at == 0
		}
	}
	return true
}

func (m *trackerModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *trackerModel) helpKeys() []key.Binding {
	if m.showingBoard() && !m.list.SettingFilter() {
		move, views, read := m.keys.Move, m.keys.View, m.keys.Read
		move.SetHelp("↑↓←→", "move")
		views.SetHelp("↑ then ←→", "view")
		if m.tabFocus {
			return []key.Binding{views, m.keys.Project, m.keys.Refresh, key.NewBinding(key.WithKeys("down"), key.WithHelp("↓/esc", "back to cards")), m.keys.Quit}
		}
		read.SetHelp("enter", "preview")
		return append(append([]key.Binding{move, m.keys.Project, m.keys.Refresh, read, views, m.keys.Search, m.keys.Open}, m.attentionKeys()...), m.keys.Quit)
	}
	if m.list.SettingFilter() {
		return []key.Binding{m.keys.Keep, m.keys.Clear}
	}
	if m.onTicket {
		scroll := m.keys.Move
		scroll.SetHelp("↑↓", "scroll")
		keys := []key.Binding{scroll, m.keys.Workfile, m.keys.Focus}
		keys = append(keys, m.fixHelp()...)
		keys = append(keys, m.keys.View, m.keys.Refresh, m.keys.Open)
		return append(append(keys, m.attentionKeys()...), m.keys.Quit)
	}
	keys := []key.Binding{m.keys.Move, m.keys.Project, m.keys.Refresh}
	if !m.shownView().board() {
		keys = append(keys, m.fixHelp()...)
	}
	keys = append(keys, m.keys.View, m.keys.Search, m.keys.Read, m.keys.Open)
	return append(append(keys, m.attentionKeys()...), m.keys.Quit)
}

func (m *trackerModel) attentionKeys() []key.Binding {
	if !m.overview {
		return nil
	}
	var keys []key.Binding
	if m.actionURL(false) != "" {
		keys = append(keys, m.keys.PR)
	}
	if m.actionURL(true) != "" {
		keys = append(keys, m.keys.Checks)
	}
	if checked, total := m.checkProgress(m.shownView()); checked < total {
		keys = append(keys, m.keys.More)
	}
	return keys
}
