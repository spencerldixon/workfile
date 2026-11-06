package cli

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	// Images are named by their foreground colour. The 256-colour form keeps the
	// id intact on terminals without true colour, and 0 to 15 are left alone
	// because renderers may rewrite them as basic colours.
	firstImageID, lastImageID = 16, 255

	maxImagePixels  = 40_000_000
	maxImageWidth   = 1600 // pixels kept and sent; the terminal scales the rest
	maxImageRows    = 16
	maxTicketImages = 8
	minImageWidth   = 20 // columns; narrower terminals name the image instead

	titleRows     = 2 // terminal rows for each line of a large title
	maxTitleLines = 3
	minTitleWidth = 30
)

// graphics is what the terminal said it can do. It is found by asking, never by
// the terminal's name, so any terminal with the kitty graphics protocol gets
// images and every other terminal gets text.
type graphics struct {
	on           bool
	hasInk       bool // the terminal told us its text colour
	ink          color.RGBA
	cellW, cellH int
}

// The kitty query, then the text colour, then the cell size in pixels. Device
// attributes comes last: every terminal answers it, so the wait ends there even
// on terminals that ignored the rest.
const graphicsQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b]10;?\x07" + "\x1b[16t" + "\x1b[c"

var (
	graphicsYes = regexp.MustCompile(`\x1b_Gi=31;OK\x1b\\`)
	cellReply   = regexp.MustCompile(`\x1b\[6;([0-9]+);([0-9]+)t`)
	deviceReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// terminalGraphics asks the terminal. It is a variable so tests can stand in
// for a real terminal.
var terminalGraphics = func() []byte { return askTerminal(graphicsQuery, deviceReply.Match) }

// detectGraphics asks once, before Bubble Tea takes over the terminal.
// WORKFILE_GRAPHICS=off turns images off, for a terminal that says it can draw
// them but does not.
func detectGraphics() graphics {
	if strings.EqualFold(os.Getenv("WORKFILE_GRAPHICS"), "off") {
		return graphics{}
	}
	return parseGraphics(terminalGraphics())
}

func parseGraphics(reply []byte) graphics {
	g := graphics{on: graphicsYes.Match(reply), cellW: 8, cellH: 16}
	if !g.on {
		return graphics{}
	}
	if r, gr, b, ok := parseBackground(reply); ok {
		g.hasInk, g.ink = true, color.RGBA{r, gr, b, 255}
	}
	if cell := cellReply.FindSubmatch(reply); cell != nil {
		h, _ := strconv.Atoi(string(cell[1]))
		w, _ := strconv.Atoi(string(cell[2]))
		if h > 0 && w > 0 {
			g.cellW, g.cellH = w, h
		}
	}
	return g
}

// picture is an image the terminal holds, and the cells it fills.
type picture struct{ id, cols, rows int }

// placeholderRows draws a picture with kitty's Unicode placeholders. They are
// ordinary cells, so the image scrolls and clips with the text around it.
func placeholderRows(p picture) []string {
	rows := make([]string, p.rows)
	for r := range rows {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", p.id)
		for c := range p.cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
		rows[r] = b.String()
	}
	return rows
}

// transmit is the sequence that sends an image and places it for placeholders.
func transmit(img image.Image, p picture) (string, error) {
	var b bytes.Buffer
	err := kitty.EncodeGraphics(&b, img, &kitty.Options{Action: kitty.TransmitAndPut, Format: kitty.PNG, ID: p.id, Columns: p.cols, Rows: p.rows, VirtualPlacement: true, Quiet: 2, Chunk: true, Transmission: kitty.Direct})
	return b.String(), err
}

// place changes the cells of an image the terminal already holds.
func place(p picture) string {
	return ansi.KittyGraphics(nil, "a=p", "U=1", fmt.Sprintf("i=%d", p.id), fmt.Sprintf("c=%d", p.cols), fmt.Sprintf("r=%d", p.rows), "q=2")
}

// forget frees an image in the terminal.
func forget(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}

// fitCells is the cells an image fills: no larger than it is, no wider than
// maxCols, no taller than maxImageRows, keeping its shape.
func fitCells(width, height int, g graphics, maxCols int) (cols, rows int) {
	cols = max(1, min(maxCols, (width+g.cellW-1)/g.cellW))
	rows = max(1, (cols*g.cellW*height+width*g.cellH-1)/(width*g.cellH))
	if rows > maxImageRows {
		rows = maxImageRows
		cols = max(1, rows*g.cellH*width/(height*g.cellW))
	}
	return cols, rows
}

// decodeImage reads a downloaded file. Its size is checked from the header
// before decoding, so a small file cannot claim a huge picture.
func decodeImage(data []byte, maxPixels int) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not an image")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxPixels {
		return nil, errors.New("image is too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("not an image")
	}
	if config.Width <= maxImageWidth {
		return img, nil
	}
	height := max(1, config.Height*maxImageWidth/config.Width)
	small := image.NewRGBA(image.Rect(0, 0, maxImageWidth, height))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	return small, nil
}

var titleFont = sync.OnceValues(func() (*sfnt.Font, error) { return opentype.Parse(gobold.TTF) })

// titleImage draws a title large, in the terminal's own text colour on a clear
// background. ok is false when bold text would serve better: the colour is not
// known, the font lacks a character, or the title would be too tall.
func titleImage(text string, g graphics, maxCols int) (img image.Image, cols, rows int, ok bool) {
	words := strings.Fields(clean(text))
	if !g.hasInk || len(words) == 0 {
		return nil, 0, 0, false
	}
	f, err := titleFont()
	if err != nil {
		return nil, 0, 0, false
	}
	var buffer sfnt.Buffer
	for _, r := range strings.Join(words, "") {
		if index, err := f.GlyphIndex(&buffer, r); err != nil || index == 0 || unicode.IsControl(r) {
			return nil, 0, 0, false
		}
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(g.cellH) * 1.4, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, 0, 0, false
	}
	defer face.Close()
	room := maxCols * g.cellW
	var lines []string
	widest := 0
	for _, word := range words {
		if font.MeasureString(face, word).Ceil() > room {
			return nil, 0, 0, false
		}
		if last := len(lines) - 1; last >= 0 && font.MeasureString(face, lines[last]+" "+word).Ceil() <= room {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	if len(lines) > maxTitleLines {
		return nil, 0, 0, false
	}
	for _, line := range lines {
		widest = max(widest, font.MeasureString(face, line).Ceil())
	}
	cols = int(math.Ceil(float64(widest) / float64(g.cellW)))
	rows = len(lines) * titleRows
	canvas := image.NewRGBA(image.Rect(0, 0, cols*g.cellW, rows*g.cellH))
	drawer := font.Drawer{Dst: draw.Image(canvas), Src: image.NewUniform(g.ink), Face: face}
	metrics := face.Metrics()
	height := titleRows * g.cellH
	for i, line := range lines {
		baseline := i*height + (height+metrics.Ascent.Ceil()-metrics.Descent.Ceil())/2
		drawer.Dot = fixed.P(0, baseline)
		drawer.DrawString(line)
	}
	return canvas, cols, rows, true
}
