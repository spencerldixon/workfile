package cli

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTicketTagPaletteUsesTheReportedColours(t *testing.T) {
	reply := "\x1b]4;0;rgb:1515/1616/1e1e\x07" +
		"\x1b]4;15;rgb:c0c0/caca/f5f5\x1b\\" +
		"\x1b]4;4;rgb:7a7a/a2a2/f7f7\x07" +
		"\x1b]4;6;#7dcfff\x1b\\" +
		"\x1b]4;8;rgb:broken/0000/ffff\x07\x1b[?1;2c"
	palette := parseTicketPalette([]byte(reply))
	for _, fill := range []lipgloss.ANSIColor{4, 6} {
		if ink, ok := palette.ink(fill); !ok || ink != 0 {
			t.Fatalf("bright fill %d needs the dark ink, got %d (verified %v)", fill, ink, ok)
		}
	}
	if _, ok := palette.ink(8); ok {
		t.Fatal("malformed colour reply supplied a fill")
	}
}

func TestTicketTagsFallBackToRoundedReverseVideo(t *testing.T) {
	for _, palette := range []ticketTagPalette{nil, {4: 0.5}, {0: 0.4, 4: 0.5, 15: 0.6}} {
		p := ticketPreview{t: newTheme(true, ""), tagPalette: palette}
		got := p.metadataTag("Story", 4)
		if !strings.HasPrefix(got, "") || !strings.HasSuffix(got, "") ||
			!strings.Contains(got, "\x1b[7mStory") || strings.Contains(got, "48;") {
			t.Fatalf("expected reverse video with rounded caps, got %q", got)
		}
	}
}
