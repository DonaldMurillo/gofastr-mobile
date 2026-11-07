// Package cell holds the screen model: cells, styles and colors, the Buffer
// that widgets paint into, and the Renderer that diffs buffers into the
// smallest escape output it can find.
//
// Nothing reaches the terminal except through cells, and Buffer.Set refuses
// control characters, so application text can never inject escape sequences.
package cell

import "math"

// Color is a terminal color: the default color, a palette index, or 24-bit RGB.
// The zero value is the terminal's default color.
type Color uint32

const (
	kindDefault = 0
	kindIndexed = 1
	kindRGB     = 2
)

// Default is the terminal's own foreground or background color.
const Default Color = 0

// The 16 basic palette colors. Their actual appearance is the user's theme.
var (
	Black         = Indexed(0)
	Red           = Indexed(1)
	Green         = Indexed(2)
	Yellow        = Indexed(3)
	Blue          = Indexed(4)
	Magenta       = Indexed(5)
	Cyan          = Indexed(6)
	White         = Indexed(7)
	BrightBlack   = Indexed(8)
	BrightRed     = Indexed(9)
	BrightGreen   = Indexed(10)
	BrightYellow  = Indexed(11)
	BrightBlue    = Indexed(12)
	BrightMagenta = Indexed(13)
	BrightCyan    = Indexed(14)
	BrightWhite   = Indexed(15)
)

// Indexed returns palette color n of the 256-color palette.
func Indexed(n uint8) Color { return Color(kindIndexed<<24 | uint32(n)) }

// RGB returns a 24-bit color.
func RGB(r, g, b uint8) Color { return Color(kindRGB<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }

func (c Color) kind() uint32 { return uint32(c) >> 24 }

// IsDefault reports whether c is the terminal default color.
func (c Color) IsDefault() bool { return c.kind() == kindDefault }

// Index returns the palette index of an indexed color.
func (c Color) Index() (uint8, bool) { return uint8(c), c.kind() == kindIndexed }

// RGB returns the components of a 24-bit color.
func (c Color) RGB() (r, g, b uint8, ok bool) {
	return uint8(c >> 16), uint8(c >> 8), uint8(c), c.kind() == kindRGB
}

// ColorMode is how many colors the terminal can show.
type ColorMode uint8

const (
	// ColorNone emits no colors (NO_COLOR); attributes still apply.
	ColorNone ColorMode = iota
	Color16
	Color256
	ColorTrue
)

// Downgrade maps c to the nearest color the mode can show.
func (c Color) Downgrade(mode ColorMode) Color {
	switch {
	case c.IsDefault() || mode == ColorTrue:
		return c
	case mode == ColorNone:
		return Default
	}
	if n, ok := c.Index(); ok {
		if mode == Color256 || n < 16 {
			return c
		}
		r, g, b := paletteRGB(n)
		return Indexed(nearest16(r, g, b))
	}
	r, g, b, _ := c.RGB()
	if mode == Color16 {
		return Indexed(nearest16(r, g, b))
	}
	return Indexed(nearest256(r, g, b))
}

var cubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// xterm's default values for the 16 basic colors, used only to pick the
// nearest one. The terminal shows the user's own theme for them.
var basic16 = [16][3]uint8{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func paletteRGB(n uint8) (uint8, uint8, uint8) {
	switch {
	case n < 16:
		c := basic16[n]
		return c[0], c[1], c[2]
	case n < 232:
		i := n - 16
		return cubeLevels[i/36], cubeLevels[i/6%6], cubeLevels[i%6]
	}
	v := 8 + 10*(n-232)
	return v, v, v
}

// distance is the "redmean" weighted RGB distance, a cheap perceptual fit.
func distance(r1, g1, b1, r2, g2, b2 uint8) float64 {
	rm := (float64(r1) + float64(r2)) / 2
	dr, dg, db := float64(r1)-float64(r2), float64(g1)-float64(g2), float64(b1)-float64(b2)
	return (2+rm/256)*dr*dr + 4*dg*dg + (2+(255-rm)/256)*db*db
}

func nearest16(r, g, b uint8) uint8 {
	best, bestDistance := uint8(0), math.Inf(1)
	for i, c := range basic16 {
		if d := distance(r, g, b, c[0], c[1], c[2]); d < bestDistance {
			best, bestDistance = uint8(i), d
		}
	}
	return best
}

func nearest256(r, g, b uint8) uint8 {
	level := func(v uint8) uint8 {
		best := uint8(0)
		for i, l := range cubeLevels {
			if absDiff(v, l) < absDiff(v, cubeLevels[best]) {
				best = uint8(i)
			}
		}
		return best
	}
	ri, gi, bi := level(r), level(g), level(b)
	cube := 16 + 36*ri + 6*gi + bi
	cr, cg, cb := paletteRGB(cube)
	average := (int(r) + int(g) + int(b)) / 3
	grayIndex := uint8(min(max((average-8+5)/10, 0), 23))
	gv := 8 + 10*grayIndex
	if distance(r, g, b, gv, gv, gv) < distance(r, g, b, cr, cg, cb) {
		return 232 + grayIndex
	}
	return cube
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// Attr is a set of text attributes.
type Attr uint16

const (
	Bold Attr = 1 << iota
	Dim
	Italic
	Blink
	Reverse
	Conceal
	Strike
	Overline
)

// Underline is an underline shape. Shapes other than single need terminal
// support; without it they render as a single underline.
type Underline uint8

const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Style is the complete appearance of a cell. Link is an OSC 8 hyperlink URL.
type Style struct {
	Fg, Bg         Color
	UnderlineColor Color
	Attrs          Attr
	Underline      Underline
	Link           string
}

// erasable reports whether erase operations (EL, ECH), which paint only the
// background color, draw a blank in this style correctly.
func (s Style) erasable() bool {
	return s.Attrs&(Reverse|Strike|Overline) == 0 && s.Underline == UnderlineNone && s.Link == ""
}
