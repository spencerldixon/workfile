package cli

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"workfile/internal/workfile"
)

var testGraphics = graphics{on: true, hasInk: true, ink: color.RGBA{200, 200, 240, 255}, cellW: 8, cellH: 16}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func graphicTickets(t *testing.T, f *fakeSource, width, height int) *trackerDriver {
	t.Helper()
	return driveWith(t, f, width, height, testGraphics, options{})
}

func imageBlocks(attachments ...string) []workfile.Block {
	var blocks []workfile.Block
	for _, a := range attachments {
		blocks = append(blocks, workfile.Block{Kind: workfile.BlockImage, Name: a + ".png", Attachment: a})
	}
	return blocks
}

func TestGraphicsReplies(t *testing.T) {
	g := parseGraphics([]byte("\x1b_Gi=31;OK\x1b\\\x1b]10;rgb:c0c0/caca/f5f5\x1b\\\x1b[6;18;9t\x1b[?62;22c"))
	if !g.on || !g.hasInk || g.ink != (color.RGBA{0xc0, 0xca, 0xf5, 255}) || g.cellW != 9 || g.cellH != 18 {
		t.Fatalf("%+v", g)
	}
	if g := parseGraphics([]byte("\x1b_Gi=31;OK\x1b\\\x1b[?62c")); !g.on || g.hasInk || g.cellW != 8 || g.cellH != 16 {
		t.Fatalf("a terminal that only answers the graphics query: %+v", g)
	}
	if g := parseGraphics([]byte("\x1b_Gi=31;OK\x1b\\\x1b[6;0;0t\x1b[?62c")); g.cellW != 8 || g.cellH != 16 {
		t.Fatalf("a zero cell size: %+v", g)
	}
	for _, reply := range []string{"", "\x1b[?62;22c", "\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c", "\x1b]10;rgb:c0c0/caca/f5f5\x1b\\"} {
		if parseGraphics([]byte(reply)).on {
			t.Errorf("%q: graphics should be off", reply)
		}
	}
}

func TestGraphicsCanBeTurnedOff(t *testing.T) {
	t.Setenv("WORKFILE_GRAPHICS", "off")
	asked := false
	saved := terminalGraphics
	terminalGraphics = func() []byte { asked = true; return nil }
	defer func() { terminalGraphics = saved }()
	if detectGraphics().on || asked {
		t.Fatalf("on, or asked the terminal (%v)", asked)
	}
}

func TestPlaceholderRowsAreCellsOfTheRightWidth(t *testing.T) {
	rows := placeholderRows(picture{id: 42, cols: 12, rows: 3})
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for i, row := range rows {
		if w := ansi.StringWidth(row); w != 12 {
			t.Fatalf("row %d is %d wide", i, w)
		}
		// The 256-colour form survives on terminals without true colour.
		if !strings.Contains(row, "\x1b[38;5;42m") {
			t.Fatalf("%q", row)
		}
		if !strings.ContainsRune(row, kitty.Diacritic(i)) {
			t.Fatalf("row %d is not marked: %q", i, row)
		}
	}
}

func TestFitCellsKeepsTheShape(t *testing.T) {
	g := testGraphics
	for _, c := range []struct{ width, height, maxCols, wantCols, wantRows int }{
		{800, 160, 50, 50, 5},  // wide: limited by the columns
		{80, 32, 50, 10, 2},    // small: never enlarged
		{400, 1600, 50, 8, 16}, // tall: limited by the rows
	} {
		cols, rows := fitCells(c.width, c.height, g, c.maxCols)
		if cols != c.wantCols || rows != c.wantRows {
			t.Errorf("%+v: got %d x %d", c, cols, rows)
		}
	}
}

func TestDecodeImageRefusesWhatItShould(t *testing.T) {
	if _, err := decodeImage([]byte("<svg>not a picture</svg>"), maxImagePixels); err == nil {
		t.Fatal("want an error for a non-image")
	}
	if _, err := decodeImage(pngBytes(t, 100, 100), 9999); err == nil {
		t.Fatal("want an error for an image over the limit")
	}
	img, err := decodeImage(pngBytes(t, 4000, 100), maxImagePixels)
	if err != nil || img.Bounds().Dx() != maxImageWidth {
		t.Fatalf("%v %v", img, err)
	}
}

func TestTitleImageFallsBackWhenItCannotDrawWell(t *testing.T) {
	img, cols, rows, ok := titleImage("Add single sign-on login", testGraphics, 60)
	if !ok || rows != 2 || cols > 60 || img.Bounds().Dx() != cols*8 || img.Bounds().Dy() != rows*16 {
		t.Fatalf("%v %d %d %v", img.Bounds(), cols, rows, ok)
	}
	if _, _, rows, ok := titleImage(strings.Repeat("word ", 12), testGraphics, 40); !ok || rows <= 2 {
		t.Fatalf("a title should wrap: %d %v", rows, ok)
	}
	for name, title := range map[string]string{
		"too long":    strings.Repeat("words go here ", 40),
		"one word":    strings.Repeat("x", 300),
		"no glyphs":   "ログインを修正",
		"empty":       "",
		"only spaces": "   ",
	} {
		if _, _, _, ok := titleImage(title, testGraphics, 40); ok {
			t.Errorf("%s: want bold text instead", name)
		}
	}
}

func TestTicketsDrawImagesInline(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"a1": pngBytes(t, 320, 160)}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks("a1")}
	d := graphicTickets(t, f, 100, 60)
	if !strings.ContainsRune(d.pane(), kitty.Placeholder) {
		t.Fatalf("no image:\n%s", d.pane())
	}
	if !strings.Contains(strings.Join(d.raw, ""), "a=T") {
		t.Fatalf("the image was never sent: %d", len(d.raw))
	}
	d.refuses("a1.png")
	d.press("down", "up")
	if len(f.imageReads) != 1 {
		t.Fatalf("read %v", f.imageReads)
	}
}

func TestTicketsNameAnImageTheyCannotDraw(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"bad": []byte("not a picture")}
	blocks := append(imageBlocks("bad", "gone"), workfile.Block{Kind: workfile.BlockImage, Name: "elsewhere.png"})
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: append(blocks, para("Text still here")...)}
	d := graphicTickets(t, f, 100, 60)
	d.wants("bad.png", "gone.png", "elsewhere.png", "Text still here")
	if len(d.m.images) != 0 {
		t.Fatalf("a broken image drew: %v", d.m.images)
	}
	if len(f.imageReads) != 2 {
		t.Fatalf("read %v", f.imageReads)
	}
}

func TestTicketsRetryAFailedImageOnRefresh(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks("a1")}
	d := graphicTickets(t, f, 100, 60)
	d.wants("a1.png")
	f.images["a1"] = pngBytes(t, 32, 32)
	d.press("r")
	if d.m.images["a1"] == nil || len(f.imageReads) != 2 {
		t.Fatalf("%v, read %v", d.m.images, f.imageReads)
	}
	d.press("r")
	if len(f.imageReads) != 2 {
		t.Fatalf("an image that worked was read again: %v", f.imageReads)
	}
}

func TestTicketsFindImagesInReplies(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"a1": pngBytes(t, 32, 32)}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", CommentTotal: 2, Comments: []workfile.Comment{{Author: "Alice", Replies: []workfile.Comment{{Author: "Bob", Body: imageBlocks("a1")}}}}}
	graphicTickets(t, f, 100, 60)
	if len(f.imageReads) != 1 {
		t.Fatalf("read %v", f.imageReads)
	}
}

func TestTicketsReadNoImagesWithoutGraphics(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"a1": pngBytes(t, 32, 32)}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks("a1")}
	d := driveTracker(t, f, 100, 60)
	d.wants("a1.png")
	if len(f.imageReads) != 0 || len(d.raw) != 0 {
		t.Fatalf("read %v, sent %d", f.imageReads, len(d.raw))
	}
}

func TestTicketsLimitImagesPerTicket(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{}
	var names []string
	for i := range maxTicketImages + 4 {
		name := "a" + string(rune('a'+i))
		names = append(names, name)
		f.images[name] = pngBytes(t, 16, 16)
	}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks(names...)}
	graphicTickets(t, f, 100, 60)
	if len(f.imageReads) != maxTicketImages {
		t.Fatalf("read %d", len(f.imageReads))
	}
}

func TestTicketsReplaceImagesOnResize(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"a1": pngBytes(t, 1200, 300)}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks("a1")}
	d := graphicTickets(t, f, 120, 60)
	d.raw = nil
	d.width = 60
	d.send(tea.WindowSizeMsg{Width: 60, Height: 60})
	if !strings.Contains(strings.Join(d.raw, ""), "a=p") {
		t.Fatalf("no new placement: %q", d.raw)
	}
	d.check()
	d.width = 21
	d.send(tea.WindowSizeMsg{Width: 21, Height: 60})
	d.wants("a1.png")
}

func TestTicketsDrawLargeTitlesWhereTheyFit(t *testing.T) {
	d := graphicTickets(t, trackerSource(), 100, 60)
	if d.m.titles["APP-1"] == nil || !strings.ContainsRune(d.pane(), kitty.Placeholder) {
		t.Fatalf("no title image:\n%s", d.pane())
	}
	if strings.Contains(d.pane(), "Fix redirect loop") {
		t.Fatalf("the title is drawn twice:\n%s", d.pane())
	}
	d = graphicTickets(t, trackerSource(), 30, 60)
	d.wants("Fix redirect loop")

	g := testGraphics
	g.hasInk = false
	d = driveWith(t, trackerSource(), 100, 60, g, options{})
	if !strings.Contains(d.pane(), "Fix redirect loop") {
		t.Fatal("without the text colour the title should be plain bold")
	}
}

func TestTicketsRecycleImageIDs(t *testing.T) {
	d := graphicTickets(t, trackerSource(), 100, 60)
	d.m.images["old"] = &ticketPicture{picture: picture{id: firstImageID}}
	d.m.nextID = lastImageID
	if id := d.m.allocate(); id != lastImageID {
		t.Fatal(id)
	}
	if id := d.m.allocate(); id != firstImageID {
		t.Fatal(id)
	}
	if d.m.images["old"] != nil {
		t.Fatal("a recycled id must drop the image that held it")
	}
}

func TestTicketsForgetImagesOnQuit(t *testing.T) {
	f := trackerSource()
	f.images = map[string][]byte{"a1": pngBytes(t, 32, 32)}
	f.details["APP-1"] = workfile.Detail{Key: "APP-1", Body: imageBlocks("a1")}
	d := graphicTickets(t, f, 100, 60)
	if got := d.m.forgetAll(); strings.Count(got, "a=d") != 2 {
		t.Fatalf("%q", got)
	}
}
