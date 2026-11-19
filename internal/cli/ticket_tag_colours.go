package cli

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Read the inks and fills once, before Bubble Tea takes over the terminal.
// Device attributes ends the probe even if the emulator ignores OSC 4.
const ticketPaletteQuery = "\x1b]4;0;?\x07\x1b]4;15;?\x07\x1b]4;1;?\x07\x1b]4;2;?\x07\x1b]4;4;?\x07\x1b]4;6;?\x07\x1b]4;8;?\x07"

var ticketPaletteReply = regexp.MustCompile(`\x1b\]4;([0-9]{1,3});([^\x1b\x07]+)(?:\x07|\x1b\\)`)

type ticketTagPalette map[lipgloss.ANSIColor]float64

func queryTicketPalette() ticketTagPalette {
	return parseTicketPalette(askTerminal(ticketPaletteQuery+ansi.RequestPrimaryDeviceAttributes, deviceReply.Match))
}

func parseTicketPalette(reply []byte) ticketTagPalette {
	palette := make(ticketTagPalette)
	for _, entry := range ticketPaletteReply.FindAllSubmatch(reply, -1) {
		index, _ := strconv.Atoi(string(entry[1]))
		if luminance, ok := paletteLuminance(string(entry[2])); ok && index < 16 {
			palette[lipgloss.ANSIColor(index)] = luminance
		}
	}
	return palette
}

func paletteLuminance(spec string) (float64, bool) {
	weights := [3]float64{0.2126, 0.7152, 0.0722}
	if rgb := backgroundReply.FindStringSubmatch(spec); rgb != nil && rgb[0] == spec {
		var luminance float64
		for i, channel := range rgb[1:] {
			value, _ := strconv.ParseUint(channel, 16, 16)
			scale := uint64(1)<<(4*len(channel)) - 1
			luminance += weights[i] * linearSRGB(float64(value)/float64(scale))
		}
		return luminance, true
	}
	if strings.HasPrefix(spec, "#") {
		r, g, b, a := ansi.HexColor(spec).RGBA()
		if a == 65535 {
			return weights[0]*linearSRGB(float64(r)/65535) +
				weights[1]*linearSRGB(float64(g)/65535) +
				weights[2]*linearSRGB(float64(b)/65535), true
		}
	}
	return 0, false
}

func linearSRGB(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func (palette ticketTagPalette) ink(fill lipgloss.ANSIColor) (lipgloss.ANSIColor, bool) {
	bg, ok := palette[fill]
	if !ok {
		return 0, false
	}
	best, contrast := lipgloss.ANSIColor(0), 0.0
	for _, candidate := range []lipgloss.ANSIColor{0, 15} {
		if fg, ok := palette[candidate]; ok {
			ratio := (max(bg, fg) + 0.05) / (min(bg, fg) + 0.05)
			if ratio > contrast {
				best, contrast = candidate, ratio
			}
		}
	}
	return best, contrast >= 4.5
}

func (p ticketPreview) metadataTag(body string, fill lipgloss.ANSIColor) string {
	if ink, ok := p.tagPalette.ink(fill); ok {
		return p.t.roundedTag(body, fill, ink)
	}
	// Reverse video preserves the contrast between the terminal's own default
	// text and background when the indexed palette cannot be verified.
	middle := p.t.render(body, func(st lipgloss.Style) lipgloss.Style { return st.Reverse(true) })
	return "" + middle + ""
}
