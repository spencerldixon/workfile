package cli

import (
	"image"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"workfile/internal/workfile"
)

func ticketDiagram(p workfile.Policy, a workfile.Assessment) workflowDiagram {
	diagram := newWorkflowDiagram(p, pipelineEdges(p, a), nil, false)
	diagram.centreText = true
	for i := range diagram.layout.nodes {
		node := &diagram.layout.nodes[i]
		if p.States[i] == a.Ticket.State {
			node.current, node.tone = true, flowTone(a)
		}
	}
	return diagram
}

// flowSpec names one drawing of the workflow. Tickets in the same stage with
// the same open and blocked moves share a picture.
type flowSpec struct {
	current, tone, moves string
	width                int
}

type workflowPictureMsg struct {
	spec flowSpec
	img  image.Image
	rows int
	ok   bool
}

func ticketFlowSpec(p workfile.Policy, a workfile.Assessment, width int) flowSpec {
	edges := pipelineEdges(p, a)
	colors := make([]string, len(edges))
	for i, edge := range edges {
		colors[i] = edge.color
	}
	return flowSpec{a.Ticket.State, flowTone(a), strings.Join(colors, ","), width}
}

func drawTicketWorkflow(p workfile.Policy, a workfile.Assessment, g graphics, spec flowSpec) tea.Cmd {
	return func() tea.Msg {
		img, rows, ok := ticketDiagram(p, a).image(g, spec.width)
		return workflowPictureMsg{spec, img, rows, ok}
	}
}

// workflowPreview retains the current picture in views that show one ticket
// at a time. The tracker keeps a cache of several pictures instead.
type workflowPreview struct {
	gfx     graphics
	spec    flowSpec
	picture *picture
	lastID  int
}

func (w *workflowPreview) clear() {
	w.spec, w.picture = flowSpec{}, nil
}

func (w *workflowPreview) request(p workfile.Policy, a workfile.Assessment, width int) tea.Cmd {
	if !w.gfx.on || width < minImageWidth || a.Ticket.Error != "" || !slices.Contains(p.States, a.Ticket.State) {
		w.clear()
		return nil
	}
	spec := ticketFlowSpec(p, a, width)
	if w.spec == spec {
		return nil
	}
	w.spec, w.picture = spec, nil
	return drawTicketWorkflow(p, a, w.gfx, spec)
}

func (w *workflowPreview) receive(msg workflowPictureMsg) tea.Cmd {
	if !msg.ok || msg.spec != w.spec || w.picture != nil {
		return nil
	}
	w.lastID++
	if w.lastID < firstImageID || w.lastID > lastImageID {
		w.lastID = firstImageID
	}
	w.picture = &picture{id: w.lastID, cols: msg.img.Bounds().Dx() / w.gfx.cellW, rows: msg.rows}
	return sendPicture(msg.img, *w.picture)
}

func ticketWorkflowLines(p workfile.Policy, a workfile.Assessment, v view, width int, img *picture) []string {
	return ticketDiagram(p, a).lines(v, width, img)
}

func moveTone(r workfile.RouteAssessment) string {
	switch {
	case r.Available():
		return green
	case slices.ContainsFunc(r.Checks, func(c workfile.Check) bool { return c.Outcome == "fail" }):
		return red
	}
	return muted
}

// flowTone is green when a forward move is open, and red when the ticket broke
// policy getting here or every way forward has a failed check. Anything less
// certain stays uncoloured.
func flowTone(a workfile.Assessment) string {
	if a.Ticket.Error != "" {
		return ""
	}
	if a.Violation() {
		return red
	}
	forward, open, failed := false, false, false
	for _, r := range a.Routes {
		if r.Backward {
			continue
		}
		forward = true
		switch moveTone(r) {
		case green:
			open = true
		case red:
			failed = true
		}
	}
	switch {
	case open:
		return green
	case forward && failed:
		return red
	}
	return ""
}
