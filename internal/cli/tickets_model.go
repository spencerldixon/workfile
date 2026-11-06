package cli

import (
	"context"
	"errors"
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
	"workfile/internal/workfile"
)

// ticketItem is one row of the list. Search looks at everything the row shows.
type ticketItem struct{ workfile.Listing }

func (i ticketItem) FilterValue() string {
	return i.Key + " " + i.Title + " " + i.Assignee + " " + i.Status
}

type ticketRow struct{ m *ticketsModel }

func (ticketRow) Height() int                         { return 1 }
func (ticketRow) Spacing() int                        { return 0 }
func (ticketRow) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d ticketRow) Render(w io.Writer, l list.Model, index int, item list.Item) {
	fmt.Fprint(w, d.m.row(item.(ticketItem), index == l.Index(), l.MatchesForItem(index)))
}

// ticketRead is what has been read about one ticket. Replies carry their read,
// so a reply to a read that was dropped or refreshed is ignored.
type ticketRead struct {
	detail    *workfile.Detail
	detailErr error
	policy    *workfile.Assessment
	policyErr error
	cancel    context.CancelFunc
}

func (r *ticketRead) detailDone() bool { return r.detail != nil || r.detailErr != nil }
func (r *ticketRead) policyDone() bool { return r.policy != nil || r.policyErr != nil }
func (r *ticketRead) pending() bool    { return !r.detailDone() || !r.policyDone() }

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
	// ticketSettledMsg says the selection stayed put long enough to read.
	ticketSettledMsg struct {
		gen int
		key string
	}
	ticketDetailMsg struct {
		read   *ticketRead
		detail workfile.Detail
		err    error
	}
	ticketPolicyMsg struct {
		read   *ticketRead
		policy workfile.Assessment
		err    error
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

const cannotFindMe = "Cannot tell who you are. Check your connection with wf test."

// wideTickets is the width from which the ticket and Workfile's view sit side
// by side.
const wideTickets = 110

type ticketKeys struct {
	Move, View, Search, Read, Focus, Refresh, Open, Quit, Keep, Clear key.Binding
}

// ticketsModel is the `wf tickets` screen: a list of the scope above the
// selected ticket. The list is read once. A ticket is read only after the
// selection rests on it, and then kept for the session.
type ticketsModel struct {
	ctx           context.Context
	e             env
	v             view
	t             theme
	list          list.Model
	pane          viewport.Model // the ticket
	side          viewport.Model // Workfile's view, on wide terminals
	spin          spinner.Model
	help          bubblehelp.Model
	keys          ticketKeys
	width, height int
	loaded        bool
	all           []workfile.Listing
	total         int // tickets in the view before any search
	view          int // index into views()
	me            string
	findingMe     bool
	counts        map[ticketView]int
	keyWidth      int
	statusWidth   int
	onTicket      bool // focus is on the ticket, not the list
	spinning      bool
	current       string // the ticket on show
	gen           int    // counts selection changes, to spot a stale pause
	reads         map[string]*ticketRead
	notice        string
	fatal         error
	gfx           graphics
	images        map[string]*ticketPicture // by attachment
	titles        map[string]*ticketPicture // by ticket key
	tried         map[string]bool           // attachments already asked for
	nextID        int
	pause         time.Duration
	open          func(string) error
	now           func() time.Time
}

func newTicketsModel(ctx context.Context, e env) *ticketsModel {
	t := newTheme(e.view.color, e.view.laneBackground())
	m := &ticketsModel{ctx: ctx, e: e, v: e.view, t: t, width: e.view.width, height: 24, reads: map[string]*ticketRead{}, counts: map[ticketView]int{}, images: map[string]*ticketPicture{}, titles: map[string]*ticketPicture{}, tried: map[string]bool{}, nextID: firstImageID, pause: 200 * time.Millisecond, now: time.Now, spin: spinner.New(spinner.WithSpinner(spinner.MiniDot)), pane: viewport.New(), side: viewport.New(), help: t.help()}
	m.open = func(target string) error { return openBrowser(ctx, target) }
	m.keys = ticketKeys{
		Move:    key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Read:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "read")),
		Focus:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "list")),
		View:    key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "view")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Open:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
		Quit:    key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Keep:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "keep")),
		Clear:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
	}
	m.help.ShortSeparator = " · "
	start, _ := pickView(e.o, e.w.Policy)
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

func (m *ticketsModel) Init() tea.Cmd {
	e, ctx, wantsMe := m.e, m.ctx, m.shownView().me
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		all, err := e.client.Listing(ctx, e.o.scope)
		if err != nil || !wantsMe {
			return ticketListMsg{all: all, err: err}
		}
		me, err := trackerIdentity(ctx, e)
		return ticketListMsg{all, me, err}
	})
}

func (m *ticketsModel) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}

func (m *ticketsModel) busy() bool {
	if !m.loaded {
		return true
	}
	r := m.reads[m.current]
	return r != nil && r.pending()
}

func (m *ticketsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(20, msg.Width), max(10, msg.Height)
		m.v.width = m.width
		resized := m.resizeImages()
		m.layout()
		item, _ := m.selected()
		return m, tea.Batch(resized, m.loadTitle(item))
	case ticketListMsg:
		return m, m.listed(msg)
	case ticketSettledMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		item, _ := m.selected()
		return m, tea.Batch(m.read(msg.key), m.loadTitle(item))
	case ticketDetailMsg:
		if msg.read == m.reads[m.current] {
			msg.read.detail, msg.read.detailErr = &msg.detail, msg.err
			if msg.err != nil {
				msg.read.detail = nil
			}
			m.fill(false)
			if msg.err == nil {
				return m, m.loadImages(msg.detail)
			}
		}
		return m, nil
	case ticketPolicyMsg:
		if msg.read == m.reads[m.current] {
			msg.read.policy, msg.read.policyErr = &msg.policy, msg.err
			if msg.err != nil {
				msg.read.policy = nil
			}
			m.fill(false)
		}
		return m, nil
	case ticketMeMsg:
		m.findingMe = false
		if msg.err != nil {
			m.notice = cannotFindMe
			return m, nil
		}
		m.me = msg.me
		return m, m.showTickets()
	case ticketImageMsg:
		return m, m.showImage(msg)
	case ticketTitleMsg:
		return m, m.showTitle(msg)
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
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, m.updateList(msg)
}

func (m *ticketsModel) listed(msg ticketListMsg) tea.Cmd {
	if msg.err != nil {
		m.fatal = msg.err
		return tea.Quit
	}
	m.loaded, m.all, m.me = true, msg.all, msg.me
	for _, t := range msg.all {
		m.keyWidth = max(m.keyWidth, len(clean(t.Key)))
		m.statusWidth = min(14, max(m.statusWidth, len(clean(t.Status))))
	}
	return m.showTickets()
}

func (m *ticketsModel) views() []ticketView {
	return ticketViews(m.e.w.Policy)
}

func (m *ticketsModel) shownView() ticketView { return m.views()[m.view] }

// findMe asks who "me" is the first time Assigned to me needs it, and never if it
// never does. A failed answer is asked again on the next visit.
func (m *ticketsModel) findMe() tea.Cmd {
	if !m.shownView().me || m.me != "" || m.findingMe {
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
func (m *ticketsModel) showTickets() tea.Cmd {
	shown := arrangeTickets(m.all, m.e.w.Policy, m.shownView(), m.me)
	m.total = len(shown)
	for _, view := range m.views() {
		m.counts[view] = len(arrangeTickets(m.all, m.e.w.Policy, view, m.me))
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
	return tea.Batch(cmd, m.follow())
}

// updateList passes a message to the list, then follows the selection, which a
// key or a search result may have moved.
func (m *ticketsModel) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return tea.Batch(cmd, m.follow())
}

func (m *ticketsModel) selected() (ticketItem, bool) {
	item, ok := m.list.SelectedItem().(ticketItem)
	return item, ok
}

// follow shows the selected ticket at once, with what is known, and waits for
// the selection to rest before reading it, so moving down the list reads
// nothing. A read still running for the ticket left behind is cancelled.
func (m *ticketsModel) follow() tea.Cmd {
	item, ok := m.selected()
	if item.Key == m.current {
		return nil
	}
	if r := m.reads[m.current]; r != nil && r.pending() {
		r.cancel()
		delete(m.reads, m.current)
	}
	m.current = item.Key
	m.gen++
	m.fill(true)
	if !ok {
		return nil
	}
	if m.reads[item.Key] != nil {
		return m.loadTitle(item)
	}
	gen, ticket := m.gen, item.Key
	return tea.Tick(m.pause, func(time.Time) tea.Msg { return ticketSettledMsg{gen, ticket} })
}

// read starts the reads for one ticket: its text from the tracker, and the policy's
// view of it. Each draws when it arrives.
func (m *ticketsModel) read(ticket string) tea.Cmd {
	if m.reads[ticket] != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	r := &ticketRead{cancel: cancel}
	m.reads[ticket] = r
	m.fill(false)
	e := m.e
	return tea.Batch(m.startSpinner(), func() tea.Msg {
		detail, err := e.client.Detail(ctx, ticket)
		return ticketDetailMsg{r, detail, err}
	}, func() tea.Msg {
		policy, err := readPolicy(ctx, e, ticket)
		return ticketPolicyMsg{r, policy, err}
	})
}

// readPolicy checks one ticket as `wf status TICKET` does.
func readPolicy(ctx context.Context, e env, ticket string) (workfile.Assessment, error) {
	found, err := e.client.Tickets(ctx, []string{ticket}, 0)
	if err != nil {
		return workfile.Assessment{}, err
	}
	if len(found) == 0 {
		return workfile.Assessment{}, errors.New(ticket + " is not visible to your account")
	}
	found, _, err = e.client.Records(ctx, found[:1])
	if err != nil {
		return workfile.Assessment{}, err
	}
	return e.w.Assess(found[0]), nil
}

func (m *ticketsModel) key(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return m.quit()
	}
	// While searching, every letter belongs to the search.
	if m.list.SettingFilter() {
		return m.updateList(msg)
	}
	m.notice = ""
	item, ok := m.selected()
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Focus):
		m.onTicket = ok && !m.onTicket
		return nil
	case key.Matches(msg, m.keys.Read) && !m.onTicket:
		m.onTicket = ok
		return nil
	case key.Matches(msg, m.keys.View):
		count := len(m.views())
		step := 1
		if msg.String() == "left" {
			step = count - 1
		}
		m.view = (m.view + step) % count
		return tea.Batch(m.findMe(), m.showTickets())
	case key.Matches(msg, m.keys.Open):
		if !ok {
			return nil
		}
		open, target := m.open, item.URL
		return func() tea.Msg { return ticketOpenedMsg{open(target)} }
	case key.Matches(msg, m.keys.Refresh):
		if !ok {
			return nil
		}
		if r := m.reads[item.Key]; r != nil {
			r.cancel()
			delete(m.reads, item.Key)
		}
		// An image that failed gets another try when the ticket is read again.
		for attachment := range m.tried {
			if m.images[attachment] == nil {
				delete(m.tried, attachment)
			}
		}
		return m.read(item.Key)
	case key.Matches(msg, m.keys.Search):
		m.onTicket = false
	}
	if !m.onTicket {
		return m.updateList(msg)
	}
	if key.Matches(msg, m.keys.Clear) {
		m.onTicket = false
		return nil
	}
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(msg)
	m.side.SetYOffset(m.pane.YOffset())
	return cmd
}

// quit frees the terminal's copies of the images on the way out.
func (m *ticketsModel) quit() tea.Cmd {
	if held := m.forgetAll(); held != "" {
		return tea.Sequence(tea.Raw(held), tea.Quit)
	}
	return tea.Quit
}

func (m *ticketsModel) forgetAll() string {
	var b strings.Builder
	for _, held := range []map[string]*ticketPicture{m.images, m.titles} {
		for _, p := range held {
			b.WriteString(forget(p.id))
		}
	}
	return b.String()
}

// allocate gives the next image id. Ids run out at lastImageID and start again,
// which drops the image that held the id, so it is read again if it comes back.
func (m *ticketsModel) allocate() int {
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
	return id
}

// loadImages reads the images of one ticket, only when the terminal can show
// them. An image that fails stays a named link.
func (m *ticketsModel) loadImages(d workfile.Detail) tea.Cmd {
	if !m.gfx.on {
		return nil
	}
	blocks := append(slices.Clone(d.Body), commentText(d.Comments)...)
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

func (m *ticketsModel) showImage(msg ticketImageMsg) tea.Cmd {
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
func (m *ticketsModel) loadTitle(item ticketItem) tea.Cmd {
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

func (m *ticketsModel) showTitle(msg ticketTitleMsg) tea.Cmd {
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
func (m *ticketsModel) resizeImages() tea.Cmd {
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
// header and the help line take one row each.
func (m *ticketsModel) layout() {
	rows := m.listRows()
	left, right := m.columns()
	m.list.SetSize(m.width, rows)
	m.pane.SetWidth(left)
	m.side.SetWidth(right)
	height := max(1, m.height-rows-2)
	m.pane.SetHeight(height)
	m.side.SetHeight(height)
	m.help.SetWidth(m.width - len(ticketIndent))
	m.fill(false)
}

// columns splits the ticket on wide terminals: its text at a comfortable
// length, with the rest of the width for Workfile's view, which stays in sight
// while the text scrolls. Narrow terminals stack them, so right is 0.
func (m *ticketsModel) columns() (left, right int) {
	if m.width < wideTickets {
		return m.width, 0
	}
	left = min(m.width*3/5, 116)
	return left, m.width - left - 1
}

// listRows is the top half, or less when there are few tickets, so the room
// goes to the ticket. It follows everything read, not the view or the search,
// so the screen does not jump.
func (m *ticketsModel) listRows() int {
	half := max(3, (m.height-2)/2)
	if len(m.all) > 0 {
		return min(half, max(3, len(m.all)))
	}
	return half
}

func (m *ticketsModel) fill(top bool) {
	main, side := m.ticketText()
	m.pane.SetContent(main)
	m.side.SetContent(side)
	if top {
		m.pane.GotoTop()
	}
	m.side.SetYOffset(m.pane.YOffset())
}

func (m *ticketsModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *ticketsModel) helpKeys() []key.Binding {
	if m.list.SettingFilter() {
		return []key.Binding{m.keys.Keep, m.keys.Clear}
	}
	if m.onTicket {
		return []key.Binding{m.keys.Move, m.keys.Focus, m.keys.View, m.keys.Refresh, m.keys.Open, m.keys.Quit}
	}
	return []key.Binding{m.keys.View, m.keys.Search, m.keys.Read, m.keys.Refresh, m.keys.Open, m.keys.Quit}
}
