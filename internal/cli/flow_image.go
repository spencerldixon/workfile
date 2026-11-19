package cli

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
	"strconv"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// lintImageID names the policy picture. It is in the range kitty ids may use,
// away from the ids the tracker hands out for tickets.
const lintImageID = 100

const maxFlowImageRows = 36

// Move colours for the picture. A picture cannot use the terminal's 16 colours,
// so these are mid tones that read on a light or a dark background.
var (
	flowGreen = color.NRGBA{R: 64, G: 176, B: 96, A: 255}
	flowRed   = color.NRGBA{R: 226, G: 84, B: 84, A: 255}
)

// flowImage draws a flowLayout as a picture: a rounded box for each stage and
// moves as arrows with rounded corners, running in the gaps between columns or
// over the top. It fills cols columns and as many rows as it needs. ok is false
// when the picture would be too cramped or too tall, and the caller draws text.
func flowImage(l *flowLayout, g graphics, cols int, routes []flowRoute) (image.Image, int, bool) {
	if !g.on || len(l.nodes) == 0 || cols < minImageWidth {
		return nil, 0, false
	}
	f, err := titleFont()
	if err != nil {
		return nil, 0, false
	}
	if img, rows, ok := flowDiagramImage(l, g, cols, routes); ok {
		return img, rows, true
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(g.cellH) * 0.7, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, 0, false
	}
	defer face.Close()
	return flowStripImage(l, g, cols, face, routes)
}

func flowDiagramImage(l *flowLayout, g graphics, cols int, routes []flowRoute) (image.Image, int, bool) {
	f, err := titleFont()
	if err != nil {
		return nil, 0, false
	}
	diagram := joinFlowRoutes(l, routes, false)
	for _, scale := range []float64{0.8, 0.7, 0.6} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(g.cellH) * scale, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, 0, false
		}
		img, rows, ok := flowImageWithFont(diagram, g, cols, face, false)
		face.Close()
		if ok {
			return img, rows, true
		}
	}
	// Gate vertices retain the connectors when a direct edge has no space
	// for its label. Try a denser drawing before falling back to strips.
	compact := joinFlowRoutes(l, routes, true)
	for _, scale := range []float64{0.7, 0.6, 0.55} {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(g.cellH) * scale, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, 0, false
		}
		img, rows, ok := flowImageWithFont(compact, g, cols*5/2, face, true)
		face.Close()
		if !ok {
			continue
		}
		width := cols * g.cellW
		canvas := image.NewRGBA(image.Rect(0, 0, width, img.Bounds().Dy()))
		fit := img.Bounds()
		if fit.Dx() > width {
			xdraw.ApproxBiLinear.Scale(canvas, canvas.Bounds(), img, fit, draw.Src, nil)
		} else {
			draw.Draw(canvas, fit.Add(image.Pt((width-fit.Dx())/2, 0)), img, fit.Min, draw.Src)
		}
		return canvas, rows, true
	}
	return nil, 0, false
}

func flowImageWithFont(l *flowLayout, g graphics, cols int, face font.Face, compact bool) (image.Image, int, bool) {
	cw, ch := g.cellW, g.cellH
	width := cols * cw
	boxH := ch * 2
	pad := ch / 3
	gap := cw * 2
	if compact {
		pad = max(2, ch/5)
		gap = cw
	}
	margin := cw
	thick := max(2, ch/8)

	boxW, colW := make([]int, len(l.nodes)), make([]int, l.cols)
	gateNodes := make([]gateBadge, len(l.nodes))
	nodeHeights := make([]int, len(l.nodes))
	for i, n := range l.nodes {
		nodeHeights[i] = boxH
		boxW[i] = font.MeasureString(face, n.name).Ceil() + 2*pad
		if len(n.gates) > 0 {
			badge := newGateBadge(face, n.gates, ch)
			gateNodes[i] = badge
			boxW[i], nodeHeights[i] = badge.width, badge.height
		}
		colW[n.col] = max(colW[n.col], boxW[i])
	}
	gapMin := make([]int, len(l.uses))
	badgeSpace := make([]int, len(l.uses))
	for i, uses := range l.uses {
		gapMin[i] = gap + len(uses)*thick*3
	}
	badges := make([]gateBadge, len(l.moves))
	laneH := ch + ch/2
	for i, move := range l.moves {
		if len(move.gates) == 0 {
			continue
		}
		badge := newGateBadge(face, move.gates, ch)
		badges[i] = badge
		laneH = max(laneH, badge.height+thick*2)
		if move.kind == moveStraight || move.kind == moveElbow {
			col := l.nodes[move.from].col
			badgeSpace[col] = max(badgeSpace[col], badge.width+thick*6+1)
		}
	}
	for col, space := range badgeSpace {
		gapMin[col] = max(gapMin[col], space+len(l.uses[col])*thick*3)
	}

	// Spare width widens the gaps evenly, up to a limit, and the diagram is centred.
	used := 2 * margin
	for _, w := range colW {
		used += w
	}
	for _, w := range gapMin {
		used += w
	}
	if used > width {
		return nil, 0, false
	}
	gapW := append([]int{}, gapMin...)
	if compact {
		width = used
	} else if len(gapW) > 0 {
		extra := min((width-used)/len(gapW), cw*6)
		for i := range gapW {
			gapW[i] += extra
			used += extra
		}
	}
	rowGap := ch
	top := margin + l.lanes*laneH
	height := top + l.rows*boxH + (l.rows-1)*rowGap + margin + l.returnLanes*laneH
	rows := (height + ch - 1) / ch
	if rows > maxFlowImageRows {
		return nil, 0, false
	}
	geo := &pixelGeometry{l: l, boxW: boxW, nodeHeights: nodeHeights, badgeSpace: badgeSpace, top: top, boxH: boxH, rowGap: rowGap, laneH: laneH}
	x := margin + max(0, (width-used)/2)
	for c := range l.cols {
		geo.colX = append(geo.colX, x)
		geo.colW = append(geo.colW, colW[c])
		x += colW[c]
		if c < len(gapW) {
			geo.gapX0 = append(geo.gapX0, x)
			geo.gapW = append(geo.gapW, gapW[c])
			x += gapW[c]
		}
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, rows*ch))
	inkColor := flowInk(g)
	d := &painter{canvas: canvas, thick: thick}
	paths := l.paths(geo)

	// Quiet moves first, so a gap shows on top where lines cross. The boxes go
	// last, over the ends of the moves.
	for _, name := range []string{muted, green, red} {
		for i, m := range l.moves {
			if m.color == name {
				d.stroke(flowMoveInk(m.color), paths[i], ch/2)
			}
		}
	}
	metrics := face.Metrics()
	for i, badge := range badges {
		if len(badge.labels) == 0 {
			continue
		}
		box, ok := flowLabelBox(paths, i, badge.width+thick*6, badge.height)
		if !ok {
			return nil, 0, false
		}
		d.gateBadge(face, badge, box.Min.X+box.Dx()/2, box.Min.Y+box.Dy()/2, flowMoveInk(l.moves[i].color))
	}
	focused := slices.ContainsFunc(l.nodes, func(n flowNode) bool { return n.current })
	for i, n := range l.nodes {
		if len(n.gates) > 0 {
			d.gateBadge(face, gateNodes[i], geo.left(i)+boxW[i]/2, geo.rowY(n.row), flowMoveInk(n.tone))
			continue
		}
		fill, text := inkColor, inkColor
		fill.A = 36
		switch {
		case n.broken:
			fill, text = flowRed, flowRed
			fill.A = 48
		case n.current:
			fill.A = 96
			if n.tone != "" {
				fill, text = flowMoveInk(n.tone), flowMoveInk(n.tone)
				fill.A = 64
			}
		case focused:
			// Other stages step back, so the ticket's stage is the one you see.
			fill.A, text.A = 20, 180
		}
		x0 := geo.startX(i) - boxW[i]
		y0 := geo.rowY(n.row) - boxH/2
		d.roundRect(fill, x0, y0, boxW[i], boxH, ch/2)
		baseline := y0 + (boxH+metrics.Ascent.Ceil()-metrics.Descent.Ceil())/2
		dr := font.Drawer{Dst: canvas, Src: image.NewUniform(text), Face: face, Dot: fixed.P(x0+pad, baseline)}
		dr.DrawString(n.name)
	}
	return canvas, rows, true
}

func flowInk(g graphics) color.NRGBA {
	if g.hasInk {
		return color.NRGBA{R: g.ink.R, G: g.ink.G, B: g.ink.B, A: 255}
	}
	// A mid grey reads on light and dark terminal backgrounds.
	return color.NRGBA{R: 140, G: 140, B: 152, A: 255}
}

func flowMoveInk(tone string) color.NRGBA {
	switch tone {
	case green:
		return flowGreen
	case red:
		return flowRed
	}
	return flowInk(graphics{})
}

// flowStripImage keeps dense workflows graphical. Numbered stage boxes wrap
// above the routes, which keep their named badges between their endpoints.
func flowStripImage(l *flowLayout, g graphics, cols int, face font.Face, moves []flowRoute) (image.Image, int, bool) {
	if len(moves) == 0 {
		return nil, 0, false
	}
	cw, ch := g.cellW, g.cellH
	width := cols * cw
	thick := max(2, ch/8)
	padding := max(1, cw)
	x, y, rowHeight := padding, padding, 0
	pack := func(w, h int) image.Rectangle {
		if x+w+padding > width {
			x, y, rowHeight = padding, y+rowHeight+padding, 0
		}
		box := image.Rect(x, y, x+w, y+h)
		x += w + padding
		rowHeight = max(rowHeight, h)
		return box
	}
	measure := func(label string) int { return font.MeasureString(face, label).Ceil() }

	stages := make([]string, len(l.nodes))
	stageBoxes := make([]image.Rectangle, len(l.nodes))
	for i, node := range l.nodes {
		stages[i] = strconv.Itoa(i+1) + " · " + node.name
		stageBoxes[i] = pack(measure(stages[i])+2*padding, ch*3/2)
		if stageBoxes[i].Max.X+padding > width {
			return nil, 0, false
		}
	}
	x, y, rowHeight = padding, y+rowHeight+padding*2, 0

	type route struct {
		left, right string
		badge       gateBadge
		box         image.Rectangle
		back        bool
		tone        string
	}
	routes := make([]route, 0, len(moves))
	for _, move := range moves {
		r := route{left: stageNumbers(move.from), right: stageNumbers(move.to), tone: move.color, back: move.backward}
		if r.back {
			r.left, r.right = r.right, r.left
		}
		if len(move.gates) > 0 {
			r.badge = newGateBadge(face, move.gates, ch)
		}
		length := measure(r.left) + measure(r.right) + r.badge.width + padding*4 + thick*8
		r.box = pack(length, max(ch, r.badge.height)+thick*2)
		if r.box.Max.X+padding > width {
			return nil, 0, false
		}
		routes = append(routes, r)
	}
	rows := (y + rowHeight + padding + ch - 1) / ch
	if rows > maxFlowImageRows {
		return nil, 0, false
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, rows*ch))
	p := &painter{canvas: canvas, thick: thick}
	ink := flowInk(g)
	metrics := face.Metrics()
	drawLabel := func(text string, x, y int, c color.NRGBA) {
		baseline := y + (metrics.Ascent.Ceil()-metrics.Descent.Ceil())/2
		d := font.Drawer{Dst: canvas, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, baseline)}
		d.DrawString(text)
	}
	for i, box := range stageBoxes {
		node := l.nodes[i]
		text := ink
		switch {
		case node.broken:
			text = flowRed
		case node.current && node.tone != "":
			text = flowMoveInk(node.tone)
		}
		fill := text
		fill.A = 36
		if node.current {
			fill.A = 96
		}
		p.roundRect(fill, box.Min.X, box.Min.Y, box.Dx(), box.Dy(), ch/3)
		drawLabel(stages[i], box.Min.X+padding, box.Min.Y+box.Dy()/2, text)
	}
	for _, r := range routes {
		y := r.box.Min.Y + r.box.Dy()/2
		left := r.box.Min.X + padding
		right := r.box.Max.X - padding - measure(r.right)
		drawLabel(r.left, left, y, ink)
		drawLabel(r.right, right, y, ink)
		from, to := left+measure(r.left)+padding, right-padding
		mid := (from + to) / 2
		if r.back {
			from, to = to, from
		}
		tone := flowMoveInk(r.tone)
		p.stroke(tone, []flowPoint{{from, y}, {to, y}}, 0)
		if len(r.badge.labels) > 0 {
			p.gateBadge(face, r.badge, mid, y, tone)
		}
	}
	return canvas, rows, true
}

// Gate names stack inside one pointed shape when a move has multiple gates.
type gateBadge struct {
	labels                       []string
	width, height, tip, lockSize int
}

func newGateBadge(face font.Face, gates []string, cellHeight int) gateBadge {
	lineHeight := face.Metrics().Height.Ceil()
	badge := gateBadge{tip: cellHeight / 3, lockSize: max(5, lineHeight*2/3)}
	for _, gate := range gates {
		label := boardStateTitle(gate)
		badge.labels = append(badge.labels, label)
		badge.width = max(badge.width, font.MeasureString(face, label).Ceil())
	}
	badge.width += cellHeight + badge.lockSize + 3
	badge.height = lineHeight*len(gates) + cellHeight/4
	return badge
}

func (p *painter) gateBadge(face font.Face, badge gateBadge, x, y int, ink color.NRGBA) {
	left, right := x-badge.width/2, x+(badge.width+1)/2
	top, bottom := y-badge.height/2, y+(badge.height+1)/2
	tip := badge.tip
	p.fill(ink, func(r *vector.Rasterizer) {
		r.MoveTo(float32(left), float32(y))
		r.LineTo(float32(left+tip), float32(top))
		r.LineTo(float32(right-tip), float32(top))
		r.LineTo(float32(right), float32(y))
		r.LineTo(float32(right-tip), float32(bottom))
		r.LineTo(float32(left+tip), float32(bottom))
		r.ClosePath()
	})
	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	baseline := y + (metrics.Ascent.Ceil()-metrics.Descent.Ceil()-(len(badge.labels)-1)*lineHeight)/2
	black := color.NRGBA{A: 255}
	for _, label := range badge.labels {
		prefix := badge.lockSize + 3
		width := prefix + font.MeasureString(face, label).Ceil()
		left := x - width/2
		p.gateLock(left, baseline-badge.lockSize, badge.lockSize, black)
		dr := font.Drawer{Dst: p.canvas, Src: image.NewUniform(black), Face: face, Dot: fixed.P(left+prefix, baseline)}
		dr.DrawString(label)
		baseline += lineHeight
	}
}

func (p *painter) gateLock(x, y, size int, ink color.NRGBA) {
	left, right := float32(1), float32(size-1)
	middle, bottom := float32(size/2), float32(size)
	centre := (left + right) / 2
	r := vector.NewRasterizer(size, size)
	r.MoveTo(left, middle)
	r.QuadTo(left, 0, centre, 0)
	r.QuadTo(right, 0, right, middle)
	r.LineTo(right-1, middle)
	r.QuadTo(right-1, 1, centre, 1)
	r.QuadTo(left+1, 1, left+1, middle)
	r.ClosePath()
	r.MoveTo(0, middle)
	r.LineTo(bottom, middle)
	r.LineTo(bottom, bottom)
	r.LineTo(0, bottom)
	r.ClosePath()
	r.Draw(p.canvas, image.Rect(x, y, x+size, y+size), image.NewUniform(ink), image.Point{})
}

// pixelGeometry places a flowLayout in pixels.
type pixelGeometry struct {
	nodeHeights             []int
	l                       *flowLayout
	boxW                    []int
	colX, colW, gapX0, gapW []int
	badgeSpace              []int
	top, boxH, rowGap       int
	laneH                   int
}

// Stages are centred in their column.
func (g *pixelGeometry) left(node int) int {
	n := g.l.nodes[node]
	return g.colX[n.col] + (g.colW[n.col]-g.boxW[node])/2
}
func (g *pixelGeometry) startX(node int) int { return g.left(node) + g.boxW[node] }
func (g *pixelGeometry) endX(node int) int   { return g.left(node) }
func (g *pixelGeometry) gapX(gap, slot int) int {
	// Keep vertical runs toward the start of gated gaps so the horizontal
	// stretch into the next stage has room for an inline badge.
	return g.gapX0[gap] + (slot+1)*(g.gapW[gap]-g.badgeSpace[gap])/(len(g.l.uses[gap])+1)
}
func (g *pixelGeometry) rowY(row int) int {
	return g.top + row*(g.boxH+g.rowGap) + g.boxH/2
}
func (g *pixelGeometry) topY(node int) int {
	return g.rowY(g.l.nodes[node].row) - g.nodeHeights[node]/2
}
func (g *pixelGeometry) bottomY(node int) int {
	return g.rowY(g.l.nodes[node].row) + (g.nodeHeights[node]+1)/2
}
func (g *pixelGeometry) bottomX(node, slot int) int {
	return g.left(node) + (slot+1)*g.boxW[node]/(len(g.l.bottoms[node])+1)
}
func (g *pixelGeometry) topX(node, slot int) int {
	return g.left(node) + (slot+1)*g.boxW[node]/(len(g.l.tops[node])+1)
}
func (g *pixelGeometry) laneY(lane int) int { return g.top - (lane+1)*g.laneH + g.laneH/2 }
func (g *pixelGeometry) returnY(lane int) int {
	return g.rowY(g.l.rows-1) + g.boxH/2 + (lane+1)*g.laneH - g.laneH/2
}

// painter draws the shapes of the picture.
type painter struct {
	canvas *image.RGBA
	thick  int
}

func (p *painter) fill(c color.NRGBA, path func(r *vector.Rasterizer)) {
	r := vector.NewRasterizer(p.canvas.Bounds().Dx(), p.canvas.Bounds().Dy())
	path(r)
	r.Draw(p.canvas, p.canvas.Bounds(), image.NewUniform(c), image.Point{})
}

func (p *painter) rect(c color.NRGBA, x0, y0, x1, y1 int) {
	draw.Draw(p.canvas, image.Rect(min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)), image.NewUniform(c), image.Point{}, draw.Over)
}

func (p *painter) roundRect(c color.NRGBA, x, y, w, h, radius int) {
	x0, y0, x1, y1, r := float32(x), float32(y), float32(x+w), float32(y+h), float32(radius)
	p.fill(c, func(v *vector.Rasterizer) {
		v.MoveTo(x0+r, y0)
		v.LineTo(x1-r, y0)
		v.QuadTo(x1, y0, x1, y0+r)
		v.LineTo(x1, y1-r)
		v.QuadTo(x1, y1, x1-r, y1)
		v.LineTo(x0+r, y1)
		v.QuadTo(x0, y1, x0, y1-r)
		v.LineTo(x0, y0+r)
		v.QuadTo(x0, y0, x0+r, y0)
		v.ClosePath()
	})
}

// stroke draws a path of horizontal and vertical runs with rounded corners and
// an arrowhead at the end, pointing along the last run.
func (p *painter) stroke(c color.NRGBA, path []flowPoint, radius int) {
	t := p.thick
	head := t * 4
	n := len(path)
	// How far each corner is rounded: no more than half of either run beside it.
	round := make([]int, n)
	for i := 1; i < n-1; i++ {
		before := abs(path[i].x-path[i-1].x) + abs(path[i].y-path[i-1].y)
		after := abs(path[i+1].x-path[i].x) + abs(path[i+1].y-path[i].y)
		round[i] = min(radius, before/2, after/2)
	}
	for i := 1; i < n; i++ {
		a, b := path[i-1], path[i]
		dx, dy := sign(b.x-a.x), sign(b.y-a.y)
		start := flowPoint{a.x + dx*round[i-1], a.y + dy*round[i-1]}
		end := flowPoint{b.x - dx*round[i], b.y - dy*round[i]}
		if i == n-1 {
			end.x -= dx * head
			end.y -= dy * head
		}
		if dy == 0 {
			p.rect(c, start.x, a.y-t/2, end.x, a.y+(t+1)/2)
		} else {
			p.rect(c, a.x-t/2, start.y, a.x+(t+1)/2, end.y)
		}
	}
	for i := 1; i < n-1; i++ {
		if round[i] < 1 {
			continue
		}
		in := flowPoint{sign(path[i].x - path[i-1].x), sign(path[i].y - path[i-1].y)}
		out := flowPoint{sign(path[i+1].x - path[i].x), sign(path[i+1].y - path[i].y)}
		r := float64(round[i])
		// The arc's centre is a radius along the outgoing run from where the
		// rounding starts. It sweeps from there to where it ends.
		cx := float64(path[i].x) - float64(in.x)*r + float64(out.x)*r
		cy := float64(path[i].y) - float64(in.y)*r + float64(out.y)*r
		from := math.Atan2(float64(-out.y), float64(-out.x))
		to := math.Atan2(float64(in.y), float64(in.x))
		sweep := math.Remainder(to-from, 2*math.Pi)
		p.ring(c, cx, cy, r, from, from+sweep)
	}
	// The arrowhead points the way the last run goes.
	tip, last := path[n-1], path[n-2]
	dx, dy := float32(sign(tip.x-last.x)), float32(sign(tip.y-last.y))
	s := float32(t * 3)
	bx, by := float32(tip.x)-dx*s*1.2, float32(tip.y)-dy*s*1.2
	p.fill(c, func(v *vector.Rasterizer) {
		v.MoveTo(float32(tip.x), float32(tip.y))
		v.LineTo(bx+dy*s, by+dx*s)
		v.LineTo(bx-dy*s, by-dx*s)
		v.ClosePath()
	})
}

// ring fills a thick arc, the rounded corner of a move.
func (p *painter) ring(c color.NRGBA, cx, cy, r, from, to float64) {
	outer, inner := r+float64(p.thick)/2, r-float64(p.thick)/2
	p.fill(c, func(v *vector.Rasterizer) {
		const steps = 16
		for i := 0; i <= steps; i++ {
			a := from + (to-from)*float64(i)/steps
			x, y := float32(cx+outer*math.Cos(a)), float32(cy+outer*math.Sin(a))
			if i == 0 {
				v.MoveTo(x, y)
			} else {
				v.LineTo(x, y)
			}
		}
		for i := steps; i >= 0; i-- {
			a := from + (to-from)*float64(i)/steps
			v.LineTo(float32(cx+inner*math.Cos(a)), float32(cy+inner*math.Sin(a)))
		}
		v.ClosePath()
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
