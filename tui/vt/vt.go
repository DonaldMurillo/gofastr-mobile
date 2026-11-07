// Package vt is a small in-memory terminal emulator for tests. It applies the
// output a program writes and exposes the resulting grid, cursor and modes, so
// tests can check what a user would see rather than which bytes were sent.
//
// It models the subset of xterm behavior a full-screen program relies on:
// cursor movement, SGR, erase, autowrap, wide characters, private modes and
// OSC 8 links. Anything else is recorded in Errors instead of being guessed.
package vt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
)

// Screen is an emulated terminal screen.
type Screen struct {
	Width, Height int
	// X and Y are the zero-based cursor position.
	X, Y          int
	CursorVisible bool
	Autowrap      bool
	// Modes records private modes set with CSI ? n h and reset with l.
	Modes map[int]bool
	// Scrolls counts line feeds that scrolled the screen.
	Scrolls int
	// Errors lists unsupported or invalid sequences, in order.
	Errors []string
	// RuneWidth returns the display width of a rune: 0, 1 or 2.
	RuneWidth func(rune) int

	cells       []cell.Cell
	pen         cell.Style
	pendingWrap bool
	lastX       int
	lastY       int
	hasLast     bool

	state  int
	seq    []byte
	utf    []byte
	strEsc bool
}

const (
	ground = iota
	escape
	csiState
	oscState
	stringState
)

// New returns a blank screen with autowrap on and the cursor visible, like a
// freshly opened terminal.
func New(width, height int) *Screen {
	s := &Screen{Width: width, Height: height, CursorVisible: true, Autowrap: true, Modes: map[int]bool{}, RuneWidth: RuneWidth}
	s.cells = make([]cell.Cell, width*height)
	for i := range s.cells {
		s.cells[i] = cell.Blank
	}
	return s
}

// Cell returns the cell at x, y.
func (s *Screen) Cell(x, y int) cell.Cell { return s.cells[y*s.Width+x] }

// Line returns row y's text, with wide cells' continuations omitted.
func (s *Screen) Line(y int) string {
	var b strings.Builder
	for x := 0; x < s.Width; x++ {
		b.WriteString(s.Cell(x, y).Text)
	}
	return b.String()
}

// Text returns every row, trimmed of trailing spaces, joined by newlines.
func (s *Screen) Text() string {
	rows := make([]string, s.Height)
	for y := range rows {
		rows[y] = strings.TrimRight(s.Line(y), " ")
	}
	return strings.Join(rows, "\n")
}

// Resize changes the size, keeping the top-left content, like a terminal
// with reflow off.
func (s *Screen) Resize(width, height int) {
	cells := make([]cell.Cell, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < s.Width && y < s.Height {
				cells[y*width+x] = s.Cell(x, y)
			} else {
				cells[y*width+x] = cell.Blank
			}
		}
		if width > 0 && width < s.Width && y < s.Height && cells[y*width+width-1].Width == 2 {
			cells[y*width+width-1] = cell.Blank
		}
	}
	s.Width, s.Height, s.cells = width, height, cells
	s.X, s.Y = min(s.X, max(width-1, 0)), min(s.Y, max(height-1, 0))
	s.pendingWrap, s.hasLast = false, false
}

// Write applies terminal output. It never fails; problems go to Errors.
func (s *Screen) Write(p []byte) (int, error) {
	for _, c := range p {
		s.feed(c)
	}
	return len(p), nil
}

func (s *Screen) errorf(format string, args ...any) {
	s.Errors = append(s.Errors, fmt.Sprintf(format, args...))
}

func (s *Screen) feed(c byte) {
	switch s.state {
	case escape:
		s.escape(c)
		return
	case csiState:
		s.seq = append(s.seq, c)
		if c >= 0x40 && c <= 0x7e {
			s.state = ground
			s.dispatchCSI(s.seq)
		}
		return
	case oscState, stringState:
		s.stringByte(c)
		return
	}
	if len(s.utf) > 0 || c >= 0x80 {
		s.utf = append(s.utf, c)
		if utf8.FullRune(s.utf) {
			r, _ := utf8.DecodeRune(s.utf)
			s.utf = s.utf[:0]
			s.print(r)
		}
		return
	}
	switch {
	case c == 0x1b:
		s.state, s.seq = escape, s.seq[:0]
	case c == '\r':
		s.X, s.pendingWrap = 0, false
	case c == '\n':
		s.lineFeed()
		s.pendingWrap = false
	case c == '\b':
		s.X, s.pendingWrap = max(s.X-1, 0), false
	case c == 0x07:
	case c < 0x20 || c == 0x7f:
		s.errorf("control byte 0x%02x", c)
	default:
		s.print(rune(c))
	}
}

func (s *Screen) escape(c byte) {
	switch c {
	case '[':
		s.state = csiState
	case ']':
		s.state, s.strEsc = oscState, false
	case 'P', '_', '^', 'X':
		s.state, s.strEsc = stringState, false
	default:
		s.state = ground
		s.errorf("unsupported escape ESC %q", c)
	}
}

func (s *Screen) stringByte(c byte) {
	if s.strEsc {
		s.strEsc = false
		if c == '\\' {
			s.endString()
			return
		}
		s.errorf("ESC inside string sequence")
	}
	switch {
	case c == 0x1b:
		s.strEsc = true
	case c == 0x07 && s.state == oscState:
		s.endString()
	default:
		s.seq = append(s.seq, c)
	}
}

func (s *Screen) endString() {
	state := s.state
	s.state = ground
	if state != oscState {
		return
	}
	body := string(s.seq)
	if rest, ok := strings.CutPrefix(body, "8;"); ok {
		_, uri, found := strings.Cut(rest, ";")
		if !found {
			s.errorf("malformed OSC 8 %q", body)
			return
		}
		s.pen.Link = uri
		return
	}
	s.errorf("unsupported OSC %q", body)
}

func (s *Screen) print(r rune) {
	width := s.RuneWidth(r)
	if width == 0 {
		if s.hasLast {
			i := s.lastY*s.Width + s.lastX
			s.cells[i].Text += string(r)
		}
		return
	}
	if s.pendingWrap && s.Autowrap {
		s.X = 0
		s.lineFeed()
	}
	s.pendingWrap = false
	if width == 2 && s.X == s.Width-1 {
		if !s.Autowrap {
			s.errorf("wide character %q at the last column with autowrap off", r)
			return
		}
		s.put(s.X, cell.Cell{Text: " ", Width: 1, Style: s.pen})
		s.X = 0
		s.lineFeed()
	}
	s.put(s.X, cell.Cell{Text: string(r), Width: uint8(width), Style: s.pen})
	if width == 2 {
		s.put(s.X+1, cell.Cell{Width: 0, Style: s.pen})
	}
	s.lastX, s.lastY, s.hasLast = s.X, s.Y, true
	s.X += width
	if s.X >= s.Width {
		s.X = s.Width - 1
		s.pendingWrap = s.Autowrap
	}
}

// put writes one cell, first blanking the other half of any wide character
// it overlaps, as xterm does.
func (s *Screen) put(x int, c cell.Cell) {
	row := s.cells[s.Y*s.Width : (s.Y+1)*s.Width]
	switch old := row[x]; {
	case old.Width == 0 && x > 0 && c.Width != 0:
		row[x-1] = s.erased()
	case old.Width == 2 && x+1 < len(row):
		row[x+1] = s.erased()
	}
	row[x] = c
}

func (s *Screen) erased() cell.Cell {
	return cell.Cell{Text: " ", Width: 1, Style: cell.Style{Bg: s.pen.Bg}}
}

func (s *Screen) lineFeed() {
	if s.Y < s.Height-1 {
		s.Y++
		return
	}
	s.Scrolls++
	copy(s.cells, s.cells[s.Width:])
	last := s.cells[(s.Height-1)*s.Width:]
	for i := range last {
		last[i] = s.erased()
	}
	s.hasLast = false
}

// erase blanks cells from..to (exclusive) on row y and repairs wide
// characters cut in half at either edge.
func (s *Screen) erase(y, from, to int) {
	from, to = max(from, 0), min(to, s.Width)
	if from >= to {
		return
	}
	row := s.cells[y*s.Width : (y+1)*s.Width]
	if row[from].Width == 0 && from > 0 {
		row[from-1] = s.erased()
	}
	if to < s.Width && row[to].Width == 0 {
		row[to] = s.erased()
	}
	for x := from; x < to; x++ {
		row[x] = s.erased()
	}
}

type params [][]int

func (p params) get(i, fallback int) int {
	if i < len(p) && len(p[i]) > 0 && p[i][0] >= 0 {
		return p[i][0]
	}
	return fallback
}

func (s *Screen) dispatchCSI(seq []byte) {
	final := seq[len(seq)-1]
	body := string(seq[:len(seq)-1])
	marker := byte(0)
	if body != "" && body[0] >= '<' && body[0] <= '?' {
		marker, body = body[0], body[1:]
	}
	if strings.ContainsAny(body, " !\"#$%&'()*+,-./") {
		s.errorf("unsupported CSI %q", seq)
		return
	}
	var p params
	if body != "" {
		for field := range strings.SplitSeq(body, ";") {
			var values []int
			for part := range strings.SplitSeq(field, ":") {
				n, err := strconv.Atoi(part)
				if part == "" {
					n, err = -1, nil
				}
				if err != nil {
					s.errorf("bad CSI parameter %q", seq)
					return
				}
				values = append(values, n)
			}
			p = append(p, values)
		}
	}
	if marker == '?' && (final == 'h' || final == 'l') {
		for i := range p {
			s.setMode(p.get(i, 0), final == 'h')
		}
		return
	}
	if marker == '>' && final == 'u' || marker == '<' && final == 'u' {
		return // kitty keyboard push/pop: no effect on the grid
	}
	if marker != 0 {
		s.errorf("unsupported CSI %q", seq)
		return
	}
	s.pendingWrap = final == 'm' && s.pendingWrap || final == 'K' && s.pendingWrap || final == 'X' && s.pendingWrap
	switch final {
	case 'H', 'f':
		s.Y = clamp(p.get(0, 1)-1, s.Height)
		s.X = clamp(p.get(1, 1)-1, s.Width)
	case 'A':
		s.Y = clamp(s.Y-max(p.get(0, 1), 1), s.Height)
	case 'B':
		s.Y = clamp(s.Y+max(p.get(0, 1), 1), s.Height)
	case 'C':
		s.X = clamp(s.X+max(p.get(0, 1), 1), s.Width)
	case 'D':
		s.X = clamp(s.X-max(p.get(0, 1), 1), s.Width)
	case 'G':
		s.X = clamp(p.get(0, 1)-1, s.Width)
	case 'd':
		s.Y = clamp(p.get(0, 1)-1, s.Height)
	case 'K':
		switch p.get(0, 0) {
		case 0:
			s.erase(s.Y, s.X, s.Width)
		case 1:
			s.erase(s.Y, 0, s.X+1)
		case 2:
			s.erase(s.Y, 0, s.Width)
		}
	case 'J':
		switch p.get(0, 0) {
		case 0:
			s.erase(s.Y, s.X, s.Width)
			for y := s.Y + 1; y < s.Height; y++ {
				s.erase(y, 0, s.Width)
			}
		case 1:
			for y := 0; y < s.Y; y++ {
				s.erase(y, 0, s.Width)
			}
			s.erase(s.Y, 0, s.X+1)
		case 2, 3:
			for y := 0; y < s.Height; y++ {
				s.erase(y, 0, s.Width)
			}
		}
	case 'X':
		s.erase(s.Y, s.X, s.X+max(p.get(0, 1), 1))
	case 'm':
		s.sgr(p)
	default:
		s.errorf("unsupported CSI %q", seq)
	}
}

func clamp(v, size int) int { return min(max(v, 0), max(size-1, 0)) }

func (s *Screen) setMode(mode int, on bool) {
	s.Modes[mode] = on
	switch mode {
	case 25:
		s.CursorVisible = on
	case 7:
		s.Autowrap = on
		if !on {
			s.pendingWrap = false
		}
	case 1049:
		for y := 0; y < s.Height; y++ {
			s.erase(y, 0, s.Width)
		}
	}
}

func (s *Screen) sgr(p params) {
	if len(p) == 0 {
		s.pen = cell.Style{Link: s.pen.Link}
		return
	}
	for i := 0; i < len(p); i++ {
		code := p.get(i, 0)
		sub := p[i][1:]
		switch {
		case code == 0:
			s.pen = cell.Style{Link: s.pen.Link}
		case code == 1:
			s.pen.Attrs |= cell.Bold
		case code == 2:
			s.pen.Attrs |= cell.Dim
		case code == 3:
			s.pen.Attrs |= cell.Italic
		case code == 4:
			s.pen.Underline = cell.UnderlineSingle
			if len(sub) > 0 && sub[0] >= 0 && sub[0] <= 5 {
				s.pen.Underline = cell.Underline(sub[0])
			}
		case code == 5:
			s.pen.Attrs |= cell.Blink
		case code == 7:
			s.pen.Attrs |= cell.Reverse
		case code == 8:
			s.pen.Attrs |= cell.Conceal
		case code == 9:
			s.pen.Attrs |= cell.Strike
		case code == 22:
			s.pen.Attrs &^= cell.Bold | cell.Dim
		case code == 23:
			s.pen.Attrs &^= cell.Italic
		case code == 24:
			s.pen.Underline = cell.UnderlineNone
		case code == 25:
			s.pen.Attrs &^= cell.Blink
		case code == 27:
			s.pen.Attrs &^= cell.Reverse
		case code == 28:
			s.pen.Attrs &^= cell.Conceal
		case code == 29:
			s.pen.Attrs &^= cell.Strike
		case code == 53:
			s.pen.Attrs |= cell.Overline
		case code == 55:
			s.pen.Attrs &^= cell.Overline
		case code >= 30 && code <= 37:
			s.pen.Fg = cell.Indexed(uint8(code - 30))
		case code >= 90 && code <= 97:
			s.pen.Fg = cell.Indexed(uint8(code - 90 + 8))
		case code >= 40 && code <= 47:
			s.pen.Bg = cell.Indexed(uint8(code - 40))
		case code >= 100 && code <= 107:
			s.pen.Bg = cell.Indexed(uint8(code - 100 + 8))
		case code == 39:
			s.pen.Fg = cell.Default
		case code == 49:
			s.pen.Bg = cell.Default
		case code == 59:
			s.pen.UnderlineColor = cell.Default
		case code == 38 || code == 48 || code == 58:
			color, used, ok := extendedColor(p, i)
			if !ok {
				s.errorf("bad extended color %v", p)
				return
			}
			i += used
			switch code {
			case 38:
				s.pen.Fg = color
			case 48:
				s.pen.Bg = color
			default:
				s.pen.UnderlineColor = color
			}
		default:
			s.errorf("unsupported SGR %d", code)
		}
	}
}

// extendedColor parses 38/48/58 in either form: 38;5;n / 38;2;r;g;b with
// semicolons, or 38:5:n / 38:2::r:g:b with colons. used counts extra
// semicolon parameters consumed.
func extendedColor(p params, i int) (cell.Color, int, bool) {
	sub := p[i][1:]
	if len(sub) == 0 {
		var values []int
		for j := i + 1; j < len(p) && len(values) < 4; j++ {
			values = append(values, p.get(j, 0))
			if len(values) == 2 && values[0] == 5 {
				break
			}
		}
		sub = values
		if len(sub) >= 2 && sub[0] == 5 {
			return cell.Indexed(uint8(sub[1])), 2, sub[1] >= 0 && sub[1] < 256
		}
		if len(sub) == 4 && sub[0] == 2 {
			return cell.RGB(uint8(sub[1]), uint8(sub[2]), uint8(sub[3])), 4, true
		}
		return 0, 0, false
	}
	switch {
	case len(sub) == 2 && sub[0] == 5:
		return cell.Indexed(uint8(sub[1])), 0, true
	case len(sub) == 5 && sub[0] == 2:
		return cell.RGB(uint8(sub[2]), uint8(sub[3]), uint8(sub[4])), 0, true
	case len(sub) == 4 && sub[0] == 2:
		return cell.RGB(uint8(sub[1]), uint8(sub[2]), uint8(sub[3])), 0, true
	}
	return 0, 0, false
}

// RuneWidth is a minimal width function: 0 for combining marks and joiners,
// 2 for common East Asian wide and emoji ranges, 1 otherwise.
func RuneWidth(r rune) int {
	switch {
	case r == 0x200d || r >= 0xfe00 && r <= 0xfe0f || unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1f64f, r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}
