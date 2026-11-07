// Package vt is a small in-memory terminal emulator for tests. It applies the
// output a program writes and exposes the resulting grid, cursor and modes, so
// tests can check what a user would see rather than which bytes were sent.
//
// It models the subset of xterm behavior a full-screen program relies on:
// cursor movement, SGR, erase, autowrap, wide characters, grapheme clusters
// (mode 2027), private modes and OSC 8 links. It answers DA1 and DECRQM
// queries through Replies. Anything else is recorded in Errors instead of
// being guessed.
package vt

import (
	"encoding/base64"
	"fmt"
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// Screen is an emulated terminal screen.
type Screen struct {
	Images                map[uint32]ImageData
	Placements            []ImagePlacement
	CellWidth, CellHeight int
	stringKind            byte
	graphicsChunk         []byte
	graphicsControl       map[string]int
	graphicsAction        byte
	inlineID              uint32
	mainImages            map[uint32]ImageData
	mainPlacements        []ImagePlacement
	Width, Height         int
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
	// Replies collects what the terminal sends back to queries. A test
	// harness drains it into the program's input.
	Replies []byte
	// Clipboard holds the last text a program set with OSC 52.
	Clipboard string

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
	s := &Screen{Width: width, Height: height, CursorVisible: true, Autowrap: true, Modes: map[int]bool{}, RuneWidth: uni.RuneWidth, Images: map[uint32]ImageData{}, CellWidth: 10, CellHeight: 20}
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
		s.hasLast = false
	case c == '\n':
		s.lineFeed()
		s.pendingWrap = false
		s.hasLast = false
	case c == '\b':
		s.X, s.pendingWrap = max(s.X-1, 0), false
		s.hasLast = false
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
		s.stringKind = c
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
		if s.stringKind == '_' {
			s.graphics(string(s.seq))
		}
		if s.stringKind == 'P' {
			s.sixel(string(s.seq))
		}
		return
	}
	body := string(s.seq)
	if strings.HasPrefix(body, "1337;File=") {
		s.inlineImage(body)
		return
	}
	if rest, ok := strings.CutPrefix(body, "8;"); ok {
		_, uri, found := strings.Cut(rest, ";")
		if !found {
			s.errorf("malformed OSC 8 %q", body)
			return
		}
		s.pen.Link = uri
		return
	}
	if rest, ok := strings.CutPrefix(body, "52;"); ok {
		_, data, _ := strings.Cut(rest, ";")
		text, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			s.errorf("malformed OSC 52 %q", body)
			return
		}
		s.Clipboard = string(text)
		return
	}
	if body == "10;?" || body == "11;?" {
		return // color queries: this terminal does not answer
	}
	s.errorf("unsupported OSC %q", body)
}

func (s *Screen) print(r rune) {
	if s.Modes[2027] && s.afterLast() {
		i := s.lastY*s.Width + s.lastX
		joined := s.cells[i].Text + string(r)
		if cluster, _ := uni.FirstGrapheme(joined); len(cluster) == len(joined) {
			s.extend(joined)
			return
		}
	}
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

// afterLast reports whether the cursor sits just after the last printed
// cell, where a terminal would continue its grapheme cluster.
func (s *Screen) afterLast() bool {
	if !s.hasLast || s.Y != s.lastY {
		return false
	}
	end := s.lastX + max(int(s.cells[s.lastY*s.Width+s.lastX].Width), 1)
	return s.X == end || s.pendingWrap && end == s.Width
}

// extend adds to the last printed cell's grapheme cluster, widening it when
// the cluster becomes wide, as terminals with mode 2027 do.
func (s *Screen) extend(cluster string) {
	row := s.cells[s.lastY*s.Width : (s.lastY+1)*s.Width]
	last := &row[s.lastX]
	_, width := uni.Graphemes.Next(cluster)
	switch {
	case width == int(last.Width) || width == 0:
		last.Text = cluster
	case width == 2 && last.Width == 1:
		if s.lastX+1 >= s.Width {
			s.errorf("cluster %q widened at the last column", cluster)
			return
		}
		last.Text, last.Width = cluster, 2
		s.put(s.lastX+1, cell.Cell{Width: 0, Style: last.Style})
		if s.Y == s.lastY && s.X == s.lastX+1 && !s.pendingWrap {
			s.X++
			if s.X >= s.Width {
				s.X = s.Width - 1
				s.pendingWrap = s.Autowrap
			}
		}
	default:
		s.errorf("cluster %q narrowed from %d to %d columns", cluster, last.Width, width)
	}
}

// put writes one cell, first blanking the other half of any wide character
// it overlaps, as xterm does.
func (s *Screen) put(x int, c cell.Cell) {
	s.removeInline(image.Rect(x, s.Y, x+max(int(c.Width), 1), s.Y+1))
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
	s.removeInline(image.Rect(from, y, to, y+1))
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
	if marker == '?' && final == 'p' && strings.HasSuffix(body, "$") { // DECRQM
		mode, err := strconv.Atoi(strings.TrimSuffix(body, "$"))
		if err != nil {
			s.errorf("bad DECRQM %q", seq)
			return
		}
		setting := 0
		if recognized[mode] {
			setting = 2
			if s.Modes[mode] {
				setting = 1
			}
		}
		s.Replies = fmt.Appendf(s.Replies, "\x1b[?%d;%d$y", mode, setting)
		return
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
	if marker == '?' && final == 'u' || marker == '>' && final == 'q' {
		return // kitty flags and XTVERSION queries: this terminal does not answer
	}
	if marker != 0 {
		s.errorf("unsupported CSI %q", seq)
		return
	}
	s.pendingWrap = final == 'm' && s.pendingWrap || final == 'K' && s.pendingWrap || final == 'X' && s.pendingWrap
	// Explicit positioning ends the preceding cluster even when the cursor
	// stays at the same coordinates. SGR alone does not create that boundary.
	if strings.ContainsRune("HfABCDGd", rune(final)) {
		s.hasLast = false
	}
	switch final {
	case 't':
		if p.get(0, 0) == 16 {
			s.Replies = fmt.Appendf(s.Replies, "\x1b[6;%d;%dt", s.CellHeight, s.CellWidth)
		} else {
			s.errorf("unsupported window query")
		}
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
	case 'c': // DA1: a VT220 with ANSI color
		if p.get(0, 0) == 0 {
			s.Replies = append(s.Replies, "\x1b[?62;22c"...)
		}
	default:
		s.errorf("unsupported CSI %q", seq)
	}
}

// recognized lists the private modes DECRQM reports as known.
var recognized = map[int]bool{
	7: true, 25: true, 1000: true, 1002: true, 1003: true, 1004: true,
	1006: true, 1049: true, 2004: true, 2026: true, 2027: true,
}

func clamp(v, size int) int { return min(max(v, 0), max(size-1, 0)) }

func (s *Screen) setMode(mode int, on bool) {
	if mode == 1049 && s.Modes[mode] == on {
		return
	}
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
		if on {
			s.mainImages, s.mainPlacements = s.Images, s.Placements
			s.Images, s.Placements = map[uint32]ImageData{}, nil
		}
		for y := 0; y < s.Height; y++ {
			s.erase(y, 0, s.Width)
		}
		if !on {
			s.Images, s.Placements = s.mainImages, s.mainPlacements
			if s.Images == nil {
				s.Images = map[uint32]ImageData{}
			}
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
