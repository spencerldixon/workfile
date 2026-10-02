package cli

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// terminalBackground asks the terminal for its background colour. It is a
// variable so tests can stand in for a real terminal.
var terminalBackground = queryTerminalBackground

var backgroundReply = regexp.MustCompile(`rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`)

// parseBackground reads the terminal's reply to an OSC 11 colour query.
func parseBackground(reply []byte) (r, g, b uint8, ok bool) {
	m := backgroundReply.FindSubmatch(reply)
	if m == nil {
		return 0, 0, 0, false
	}
	var out [3]uint8
	for i := range out {
		digits := m[i+1]
		n, err := strconv.ParseUint(string(digits), 16, 16)
		if err != nil {
			return 0, 0, 0, false
		}
		max := math.Pow(16, float64(len(digits))) - 1
		out[i] = uint8(math.Round(float64(n) / max * 255))
	}
	return out[0], out[1], out[2], true
}

// laneTint moves a background a little toward white on dark themes and a little
// toward black on light ones, so a block stands out without hiding any text.
func laneTint(r, g, b uint8) (uint8, uint8, uint8) {
	luminance := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
	target, amount := 255.0, 0.07
	if luminance >= 0.5 {
		target, amount = 0, 0.06
	}
	mix := func(c uint8) uint8 { return uint8(math.Round(float64(c) + (target-float64(c))*amount)) }
	return mix(r), mix(g), mix(b)
}

// backgroundCode returns the SGR parameters for a background colour, in true
// colour when the terminal supports it and the nearest of 256 colours if not.
func backgroundCode(r, g, b uint8) string {
	if c := os.Getenv("COLORTERM"); c == "truecolor" || c == "24bit" {
		return fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
	}
	levels := []int{0, 95, 135, 175, 215, 255}
	nearest := func(c uint8) int {
		best := 0
		for i, l := range levels {
			if math.Abs(float64(l)-float64(c)) < math.Abs(float64(levels[best])-float64(c)) {
				best = i
			}
		}
		return best
	}
	ri, gi, bi := nearest(r), nearest(g), nearest(b)
	index := 16 + 36*ri + 6*gi + bi
	dist := func(x, y, z int) int {
		return (x-int(r))*(x-int(r)) + (y-int(g))*(y-int(g)) + (z-int(b))*(z-int(b))
	}
	best := dist(levels[ri], levels[gi], levels[bi])
	for step := 0; step < 24; step++ {
		grey := 8 + 10*step
		if d := dist(grey, grey, grey); d < best {
			best, index = d, 232+step
		}
	}
	return fmt.Sprintf("48;5;%d", index)
}

// laneBackground is the SGR background for lanes, or "" when there is none:
// no colour, the setting turned off, or a terminal that did not answer.
func (v view) laneBackground() string {
	if !v.color || strings.EqualFold(os.Getenv("WORKFILE_BACKGROUND"), "off") {
		return ""
	}
	find := func() string {
		r, g, b, ok := terminalBackground()
		if !ok {
			return ""
		}
		return backgroundCode(laneTint(r, g, b))
	}
	if v.tint == nil {
		return find()
	}
	// Ask the terminal once per run, however many lanes are drawn.
	v.tint.once.Do(func() { v.tint.code = find() })
	return v.tint.code
}

type tintCache struct {
	once sync.Once
	code string
}
