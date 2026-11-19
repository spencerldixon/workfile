package cli

import (
	"image"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"workfile/internal/workfile"
)

// workflowDiagram renders a policy graph as a picture or terminal text.
type workflowDiagram struct {
	edges      []pipelineEdge
	layout     flowLayout
	centreText bool
}

func newWorkflowDiagram(p workfile.Policy, edges []pipelineEdge, broken map[string]bool, showGates bool) workflowDiagram {
	if !showGates {
		edges = slices.Clone(edges)
		for i := range edges {
			edges[i].gates = nil
		}
	}
	ends := make(map[string]bool, len(p.States))
	for _, state := range p.States {
		ends[state] = p.Transitions[state].End
	}
	layout := newFlowLayout(p.States, ends, edges, broken)
	for i := range layout.nodes {
		layout.nodes[i].name = boardStateTitle(layout.nodes[i].name)
	}
	return workflowDiagram{edges: edges, layout: layout}
}

func (d workflowDiagram) textRows(v view, width int) []row {
	if len(d.layout.nodes) == 0 {
		return nil
	}
	if !v.color {
		d.layout.nodes = slices.Clone(d.layout.nodes)
		for i := range d.layout.nodes {
			node := &d.layout.nodes[i]
			if node.current {
				node.name = "[" + node.name + "]"
			}
		}
	}
	v.width = width + 10
	moves := flowRoutes(d.layout.moves)
	if rows, ok := v.textFlow(joinFlowRoutes(&d.layout, moves, false), width); ok {
		return rows
	}
	focused := slices.ContainsFunc(d.layout.nodes, func(n flowNode) bool { return n.current })
	labels := make([]cell, len(d.layout.nodes))
	for i, node := range d.layout.nodes {
		labels[i] = styled(node.name, node.textStyle(focused))
	}
	if len(moves) < len(d.layout.moves) {
		return v.groupedFlowRows(labels, moves)
	}
	return v.pipelineRows(labels, d.edges)
}

// lines uses a prepared picture when it fits, and otherwise draws the text
// fallback. The caller owns transmission and caching of the picture.
func (d workflowDiagram) lines(v view, width int, img *picture) []string {
	if v.color && img != nil && img.cols <= width {
		lines := placeholderRows(*img)
		pad := strings.Repeat(" ", (width-img.cols)/2)
		for i := range lines {
			lines[i] = pad + lines[i]
		}
		return lines
	}
	var lines []string
	for _, r := range d.textRows(v, width) {
		lines = append(lines, strings.TrimRight(v.render(r), " "))
	}
	if d.centreText {
		return centre(lines, width)
	}
	return lines
}

func (d workflowDiagram) image(g graphics, width int) (image.Image, int, bool) {
	return flowImage(&d.layout, g, min(width, 120), flowRoutes(d.layout.moves))
}

func (d workflowDiagram) printedPicture(g graphics, width, id int) (string, *picture) {
	img, rows, ok := d.image(g, width)
	if !ok {
		return "", nil
	}
	p := picture{id: id, cols: img.Bounds().Dx() / g.cellW, rows: rows}
	sequence, err := transmit(img, p)
	if err != nil {
		return "", nil
	}
	return sequence, &p
}

// centre pads lines on the left so the block sits in the middle of width.
// Lines keep their positions relative to each other.
func centre(lines []string, width int) []string {
	widest := 0
	for _, line := range lines {
		widest = max(widest, ansi.StringWidth(line))
	}
	pad := strings.Repeat(" ", max(0, (width-widest)/2))
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = pad + line
	}
	return out
}
