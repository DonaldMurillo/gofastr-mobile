package cell

import (
	"strconv"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// Options describe what the terminal supports. The zero value is the safest
// profile: no colors, no synchronized output, plain underlines, no links.
type Options struct {
	Images                ImageProtocol
	CellWidth, CellHeight int
	Colors                ColorMode
	// SyncOutput wraps each frame in DEC mode 2026 so it appears at once.
	SyncOutput bool
	// StyledUnderline enables underline shapes (4:3) and colors (58).
	StyledUnderline bool
	// Hyperlinks enables OSC 8 links.
	Hyperlinks bool
}

// Cursor is where the terminal cursor should be after a frame.
type Cursor struct {
	X, Y    int
	Visible bool
}

// Renderer turns successive frames into terminal output. It keeps a front
// buffer of what the terminal shows and writes only the differences.
//
// The output assumes autowrap is off and the screen was not changed by
// anyone else. If a write fails partway, or another program used the
// terminal, call Invalidate so the next frame repaints everything.
type Renderer struct {
	imageFrame    uint64
	transmitted   map[uint32]imageTransmission
	imageFull     bool
	imageProtocol ImageProtocol
	placements    []ImagePlacement
	rasterChanged bool
	options       Options
	front         *Buffer
	out           []byte
	scratch       []byte
	alternate     []byte

	full        bool
	pen         Style
	penValid    bool
	x, y        int
	cursorValid bool
	// last is the cell text written just before the cursor, while no
	// cursor movement has happened since; the next cell must not join it.
	last       string
	shown      bool
	shownValid bool
}

// NewRenderer returns a renderer for a width by height screen. The first
// frame repaints the whole screen.
func NewRenderer(width, height int, options Options) *Renderer {
	return &Renderer{options: options, front: NewBuffer(width, height), full: true}
}

// Options returns the renderer's terminal profile.
func (r *Renderer) Options() Options { return r.options }

// SetOptions changes the terminal profile and repaints on the next frame.
func (r *Renderer) SetOptions(options Options) {
	r.options = options
	r.Invalidate()
}

// Invalidate forgets what the terminal shows; the next frame repaints all.
func (r *Renderer) Invalidate() { r.full = true }

// Resize changes the screen size and repaints on the next frame.
func (r *Renderer) Resize(width, height int) {
	r.front.Resize(width, height)
	r.full = true
}

// Reset returns output that leaves the terminal pen in its default state:
// SGR reset and no open hyperlink. Write it before restoring the terminal.
func (r *Renderer) Reset() []byte {
	out := []byte("\x1b[0m")
	for id := range r.transmitted {
		out = append(out, "\x1b_Ga=d,d=I,q=2,i="...)
		out = strconv.AppendUint(out, uint64(id), 10)
		out = append(out, "\x1b\\"...)
	}
	clear(r.transmitted)
	clear(r.placements)
	r.placements = r.placements[:0]
	if r.options.Hyperlinks {
		out = append(out, "\x1b]8;;\x1b\\"...)
	}
	r.pen, r.penValid = Style{}, true
	return out
}

// Render returns the output that turns the current screen into back with the
// cursor at c. The result is valid until the next call. A back buffer of a
// different size resizes the renderer and repaints.
func (r *Renderer) Render(back *Buffer, c Cursor) []byte {
	if back.width != r.front.width || back.height != r.front.height {
		r.Resize(back.width, back.height)
	}
	r.out = r.out[:0]
	if r.options.SyncOutput {
		r.out = append(r.out, "\x1b[?2026h"...)
	}
	start := len(r.out)
	if r.full {
		r.imageFull = true
		r.repaintStart()
	}
	r.prepareImages(back)
	for y := 0; y < back.height; y++ {
		r.renderRow(back, y)
	}
	r.renderImages(back)
	r.placeCursor(c, back.width, back.height)
	if len(r.out) == start {
		r.out = r.out[:0]
		return r.out
	}
	if r.options.SyncOutput {
		r.out = append(r.out, "\x1b[?2026l"...)
	}
	return r.out
}

func (r *Renderer) repaintStart() {
	r.out = append(r.out, "\x1b[0m"...)
	if r.options.Hyperlinks {
		r.out = append(r.out, "\x1b]8;;\x1b\\"...)
	}
	r.out = append(r.out, "\x1b[H\x1b[2J"...)
	r.front.Clear()
	r.pen, r.penValid = Style{}, true
	r.x, r.y, r.cursorValid = 0, 0, true
	r.shownValid = false
	r.full = false
}

// minErase is the shortest blank run worth an erase sequence instead of spaces.
const minErase = 6

func (r *Renderer) renderRow(back *Buffer, y int) {
	w := back.width
	brow := back.cells[y*w : (y+1)*w]
	frow := r.front.cells[y*w : (y+1)*w]
	for x := 0; x < w; {
		bc := brow[x]
		if bc.Reserved {
			frow[x] = bc
			x++
			continue
		}
		if bc == frow[x] || bc.Width == 0 {
			x++
			continue
		}
		if run := blankRun(brow, x); run >= minErase && r.effective(bc.Style).erasable() {
			r.moveTo(x, y, brow, frow)
			r.setPen(bc.Style)
			if x+run == w {
				r.out = append(r.out, "\x1b[K"...)
			} else {
				r.out = append(r.out, "\x1b["...)
				r.out = strconv.AppendInt(r.out, int64(run), 10)
				r.out = append(r.out, 'X')
			}
			copy(frow[x:x+run], brow[x:x+run])
			x += run
			continue
		}
		// Separate cells must not merge into one grapheme, as two lone
		// regional indicators would. A cursor move ends the cluster.
		if r.cursorValid && r.x == x && r.y == y && uni.Joins(r.last, bc.Text) {
			r.cursorValid = false
		}
		r.moveTo(x, y, brow, frow)
		r.setPen(bc.Style)
		r.out = append(r.out, bc.Text...)
		r.last = bc.Text
		frow[x] = bc
		if bc.Width == 2 {
			frow[x+1] = brow[x+1]
		}
		x += int(bc.Width)
		r.x = x
		if r.x >= w {
			// With autowrap off the cursor sticks in the last column, and
			// some terminals keep a pending-wrap state. Use absolute moves.
			r.cursorValid = false
		}
	}
}

// blankRun counts the blank cells from x that share brow[x]'s style.
func blankRun(brow []Cell, x int) int {
	first := brow[x]
	if first.Text != " " {
		return 0
	}
	n := 0
	for i := x; i < len(brow) && !brow[i].Reserved && brow[i].Text == " " && brow[i].Style == first.Style; i++ {
		n++
	}
	return n
}

// moveTo positions the cursor at x, y using the shortest sequence. On the same
// row it may rewrite a few unchanged cells instead of moving over them.
func (r *Renderer) moveTo(x, y int, brow, frow []Cell) {
	if r.cursorValid && r.x == x && r.y == y {
		return
	}
	r.scratch = appendCUP(r.scratch[:0], x, y)
	switch {
	case r.cursorValid && brow != nil && r.y == y && x > r.x && x-r.x <= len(r.scratch) && r.canRewrite(brow, frow, r.x, x):
		for i := r.x; i < x; i++ {
			r.out = append(r.out, brow[i].Text...)
		}
		r.x, r.y, r.last = x, y, brow[x-1].Text
		return
	case r.cursorValid:
		r.alternate = r.appendRelative(r.alternate[:0], x, y)
		if len(r.alternate) < len(r.scratch) {
			r.out = append(r.out, r.alternate...)
		} else {
			r.out = append(r.out, r.scratch...)
		}
	default:
		r.out = append(r.out, r.scratch...)
	}
	r.x, r.y, r.cursorValid, r.last = x, y, true, ""
}

// canRewrite reports whether cells from..to are unchanged ASCII cells in the
// current pen, so writing them again moves the cursor without changing them.
func (r *Renderer) canRewrite(brow, frow []Cell, from, to int) bool {
	if !r.penValid {
		return false
	}
	for i := from; i < to; i++ {
		if brow[i].Reserved || brow[i] != frow[i] || brow[i].Width != 1 || len(brow[i].Text) != 1 || brow[i].Text[0] >= utf8.RuneSelf || r.effective(brow[i].Style) != r.pen {
			return false
		}
	}
	return true
}

func appendCUP(out []byte, x, y int) []byte {
	out = append(out, "\x1b["...)
	if y > 0 || x > 0 {
		out = strconv.AppendInt(out, int64(y+1), 10)
	}
	if x > 0 {
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(x+1), 10)
	}
	return append(out, 'H')
}

func (r *Renderer) appendRelative(out []byte, x, y int) []byte {
	switch dy := y - r.y; {
	case dy == 1 && x == 0:
		return append(out, "\r\n"...)
	case dy > 0:
		out = appendCount(out, dy, 'B')
	case dy < 0:
		out = appendCount(out, -dy, 'A')
	}
	switch dx := x - r.x; {
	case x == 0 && dx != 0:
		out = append(out, '\r')
	case dx > 0:
		out = appendCount(out, dx, 'C')
	case dx < 0:
		out = appendCount(out, -dx, 'D')
	}
	return out
}

func appendCount(out []byte, n int, final byte) []byte {
	out = append(out, "\x1b["...)
	if n != 1 {
		out = strconv.AppendInt(out, int64(n), 10)
	}
	return append(out, final)
}

func (r *Renderer) placeCursor(c Cursor, w, h int) {
	visible := c.Visible && c.X >= 0 && c.Y >= 0 && c.X < w && c.Y < h
	if visible {
		r.moveTo(c.X, c.Y, nil, nil)
	}
	if !r.shownValid || r.shown != visible {
		if visible {
			r.out = append(r.out, "\x1b[?25h"...)
		} else {
			r.out = append(r.out, "\x1b[?25l"...)
		}
		r.shown, r.shownValid = visible, true
	}
}

// effective is s as this terminal will show it, after downgrades.
func (r *Renderer) effective(s Style) Style {
	s.Fg = s.Fg.Downgrade(r.options.Colors)
	s.Bg = s.Bg.Downgrade(r.options.Colors)
	if r.options.StyledUnderline {
		s.UnderlineColor = s.UnderlineColor.Downgrade(r.options.Colors)
	} else {
		s.UnderlineColor = Default
		if s.Underline != UnderlineNone {
			s.Underline = UnderlineSingle
		}
	}
	if !r.options.Hyperlinks {
		s.Link = ""
	}
	return s
}

func (r *Renderer) setPen(s Style) {
	s = r.effective(s)
	if r.penValid && s == r.pen {
		return
	}
	if !r.penValid || s.Link != r.pen.Link {
		if r.options.Hyperlinks {
			r.out = append(r.out, "\x1b]8;;"...)
			r.out = appendLink(r.out, s.Link)
			r.out = append(r.out, "\x1b\\"...)
		}
	}
	from := r.pen
	if !r.penValid {
		from = Style{}
	}
	r.scratch = appendSGR(r.scratch[:0], from, s)
	r.alternate = appendSGR(append(r.alternate[:0], '0'), Style{}, s)
	codes := r.scratch
	if !r.penValid || len(r.alternate) < len(r.scratch) {
		codes = r.alternate
	}
	if len(codes) > 0 {
		r.out = append(r.out, "\x1b["...)
		r.out = append(r.out, codes...)
		r.out = append(r.out, 'm')
	}
	r.pen, r.penValid = s, true
}

// appendLink appends a URL with every byte outside printable ASCII removed,
// so a link cannot end its own escape sequence. URLs should be percent-encoded.
func appendLink(out []byte, link string) []byte {
	for i := 0; i < len(link); i++ {
		if c := link[i]; c >= 0x20 && c < 0x7f {
			out = append(out, c)
		}
	}
	return out
}

var attrCodes = []struct {
	attr    Attr
	on, off string
}{
	{Italic, "3", "23"}, {Blink, "5", "25"}, {Reverse, "7", "27"},
	{Conceal, "8", "28"}, {Strike, "9", "29"}, {Overline, "53", "55"},
}

// appendSGR appends the SGR parameters that change from into to, separated
// by semicolons, after any parameters already in codes.
func appendSGR(codes []byte, from, to Style) []byte {
	sep := func() {
		if len(codes) > 0 {
			codes = append(codes, ';')
		}
	}
	add := func(code string) {
		sep()
		codes = append(codes, code...)
	}
	boldDim := Bold | Dim
	if from.Attrs&boldDim&^to.Attrs != 0 {
		add("22") // clears both bold and dim
		from.Attrs &^= boldDim
	}
	if to.Attrs&Bold != 0 && from.Attrs&Bold == 0 {
		add("1")
	}
	if to.Attrs&Dim != 0 && from.Attrs&Dim == 0 {
		add("2")
	}
	for _, a := range attrCodes {
		switch on, was := to.Attrs&a.attr != 0, from.Attrs&a.attr != 0; {
		case on && !was:
			add(a.on)
		case !on && was:
			add(a.off)
		}
	}
	if to.Underline != from.Underline {
		switch to.Underline {
		case UnderlineNone:
			add("24")
		case UnderlineSingle:
			add("4")
		default:
			add("4:")
			codes = append(codes, byte('0'+to.Underline))
		}
	}
	if to.Fg != from.Fg {
		sep()
		codes = appendColor(codes, to.Fg, 30, 90, "38", "39")
	}
	if to.Bg != from.Bg {
		sep()
		codes = appendColor(codes, to.Bg, 40, 100, "48", "49")
	}
	if to.UnderlineColor != from.UnderlineColor {
		sep()
		codes = appendColor(codes, to.UnderlineColor, -1, -1, "58", "59")
	}
	return codes
}

func appendColor(out []byte, c Color, base, bright int, extended, reset string) []byte {
	if c.IsDefault() {
		return append(out, reset...)
	}
	if n, ok := c.Index(); ok {
		switch {
		case n < 8 && base >= 0:
			return strconv.AppendInt(out, int64(base+int(n)), 10)
		case n < 16 && bright >= 0:
			return strconv.AppendInt(out, int64(bright+int(n)-8), 10)
		}
		out = append(out, extended...)
		out = append(out, ";5;"...)
		return strconv.AppendInt(out, int64(n), 10)
	}
	red, green, blue, _ := c.RGB()
	out = append(out, extended...)
	out = append(out, ";2;"...)
	out = strconv.AppendInt(out, int64(red), 10)
	out = append(out, ';')
	out = strconv.AppendInt(out, int64(green), 10)
	out = append(out, ';')
	return strconv.AppendInt(out, int64(blue), 10)
}
