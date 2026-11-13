package cli

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// lintImageID names the policy picture. It is in the range kitty ids may use,
// away from the ids the tracker hands out for tickets.
const lintImageID = 100

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
func flowImage(l *flowLayout, g graphics, cols int) (img image.Image, rows int, ok bool) {
	if !g.on || len(l.nodes) == 0 || cols < minImageWidth {
		return nil, 0, false
	}
	f, err := titleFont()
	if err != nil {
		return nil, 0, false
	}
	cw, ch := g.cellW, g.cellH
	width := cols * cw
	boxH := ch * 2
	pad := ch / 2
	margin := cw
	thick := max(2, ch/8)

	// Many stages shrink the lettering until they fit, down to a floor.
	var face font.Face
	var boxW, colW, gapMin []int
	for _, scale := range []float64{0.8, 0.7, 0.6} {
		candidate, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(ch) * scale, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, 0, false
		}
		sizes, columns := make([]int, len(l.nodes)), make([]int, l.cols)
		for i, n := range l.nodes {
			sizes[i] = font.MeasureString(candidate, n.name).Ceil() + 2*pad
			columns[n.col] = max(columns[n.col], sizes[i])
		}
		gaps, need := make([]int, len(l.uses)), 2*margin
		for _, w := range columns {
			need += w
		}
		for i, uses := range l.uses {
			gaps[i] = cw*3 + len(uses)*thick*4
			need += gaps[i]
		}
		if need <= width {
			face, boxW, colW, gapMin = candidate, sizes, columns, gaps
			break
		}
		candidate.Close()
	}
	if face == nil {
		return nil, 0, false
	}
	defer face.Close()

	// Spare width widens the gaps evenly, up to a limit, and the diagram is centred.
	used := 2 * margin
	for _, w := range colW {
		used += w
	}
	for _, w := range gapMin {
		used += w
	}
	gapW := append([]int{}, gapMin...)
	if len(gapW) > 0 {
		extra := min((width-used)/len(gapW), cw*6)
		for i := range gapW {
			gapW[i] += extra
			used += extra
		}
	}
	laneH := ch + ch/2
	rowGap := ch
	top := margin + l.lanes*laneH
	height := top + l.rows*boxH + (l.rows-1)*rowGap + margin
	rows = (height + ch - 1) / ch
	if rows > maxImageRows {
		return nil, 0, false
	}
	geo := &pixelGeometry{l: l, boxW: boxW, thick: thick, top: top, boxH: boxH, rowGap: rowGap, laneH: laneH}
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
	// Not every terminal tells us its text colour (Rex does not). A mid grey
	// reads on both a light and a dark background.
	ink := color.RGBA{R: 140, G: 140, B: 152, A: 255}
	if g.hasInk {
		ink = g.ink
	}
	inkColor := color.NRGBA{R: ink.R, G: ink.G, B: ink.B, A: 255}
	paint := func(name string) color.NRGBA {
		switch name {
		case green:
			return flowGreen
		case red:
			return flowRed
		}
		// Opaque, so moves that share a stretch do not darken where they overlap.
		return color.NRGBA{R: 140, G: 140, B: 152, A: 255}
	}
	d := &painter{canvas: canvas, thick: thick}

	// Quiet moves first, so a gap shows on top where lines cross. The boxes go
	// last, over the ends of the moves.
	for _, name := range []string{muted, green, red} {
		for i, m := range l.moves {
			if m.color == name {
				d.stroke(paint(m.color), l.route(i, geo), ch/2)
			}
		}
	}
	metrics := face.Metrics()
	for i, n := range l.nodes {
		fill, text := inkColor, inkColor
		fill.A = 36
		if n.broken {
			fill, text = flowRed, flowRed
			fill.A = 48
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

// pixelGeometry places a flowLayout in pixels.
type pixelGeometry struct {
	l                        *flowLayout
	boxW                     []int
	colX, colW, gapX0, gapW  []int
	thick, top, boxH, rowGap int
	laneH                    int
}

// Stages are centred in their column.
func (g *pixelGeometry) left(node int) int {
	n := g.l.nodes[node]
	return g.colX[n.col] + (g.colW[n.col]-g.boxW[node])/2
}
func (g *pixelGeometry) startX(node int) int { return g.left(node) + g.boxW[node] }
func (g *pixelGeometry) endX(node int) int   { return g.left(node) }
func (g *pixelGeometry) gapX(gap, slot int) int {
	return g.gapX0[gap] + (slot+1)*g.gapW[gap]/(len(g.l.uses[gap])+1)
}
func (g *pixelGeometry) rowY(row int) int {
	return g.top + row*(g.boxH+g.rowGap) + g.boxH/2
}
func (g *pixelGeometry) topY(node int) int { return g.rowY(g.l.nodes[node].row) - g.boxH/2 }
func (g *pixelGeometry) topX(node, slot int) int {
	return g.left(node) + (slot+1)*g.boxW[node]/(len(g.l.tops[node])+1)
}
func (g *pixelGeometry) laneY(lane int) int { return g.top - (lane+1)*g.laneH + g.laneH/2 }

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
// an arrowhead at the end. The last run always goes right.
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
	// The arrowhead points the way the last run goes: right, or down.
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
