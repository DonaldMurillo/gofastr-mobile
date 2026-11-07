package widget

import (
	"strings"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// TextArea is a multi-line text field without soft wrap: long lines
// scroll sideways. Like Input it is controlled through Value and
// OnChange. Enter inserts a newline; Tab is left for focus traversal.
type TextArea struct {
	Value       string
	Placeholder string
	OnChange    func(string)
	// MaxLength limits the value in runes; zero means unlimited.
	MaxLength int
	// OnPaste receives raw text first; true consumes the paste.
	OnPaste     func(text string) bool
	AutoFocused bool
	Style       cell.Style
	FocusStyle  cell.Style
}

type textAreaState struct {
	scroller
	cursor int // byte offset
	left   int // first visible column
	want   int // column Up and Down aim for, -1 to use the cursor's
	at     layout.Point
}

func (TextArea) NewState() any                                           { return &textAreaState{want: -1} }
func (t TextArea) Focusable() bool                                       { return true }
func (t TextArea) AutoFocus() bool                                       { return t.AutoFocused }
func (t TextArea) Layout(_ *view.Node, c layout.Constraints) layout.Size { return c.Max() }

// textLine is one line of the value: its byte range and units.
type textLine struct {
	start, end int
	units      []unit
}

func splitLines(m uni.Method, s string) []textLine {
	var out []textLine
	start := 0
	for {
		end := strings.IndexByte(s[start:], '\n')
		if end < 0 {
			end = len(s)
		} else {
			end += start
		}
		us := units(m, s[start:end])
		for i := range us {
			us[i].start += start
			us[i].end += start
		}
		out = append(out, textLine{start, end, us})
		if end == len(s) {
			return out
		}
		start = end + 1
	}
}

// locate returns the line holding offset and the column of offset in it.
func locate(lines []textLine, offset int) (row, col int) {
	for row = len(lines) - 1; row > 0 && lines[row].start > offset; row-- {
	}
	for _, u := range lines[row].units {
		if u.start >= offset {
			break
		}
		col += u.width
	}
	return row, col
}

// offsetAt returns the offset nearest column col on line l.
func offsetAt(l textLine, col int) int {
	x := 0
	for _, u := range l.units {
		if col < x+(u.width+1)/2 {
			return u.start
		}
		x += u.width
	}
	return l.end
}

func (t TextArea) Draw(n *view.Node, c *view.Canvas) {
	st := n.State().(*textAreaState)
	size := c.Size()
	style := pick(t.Style, Theme.Input)
	if n.Focused() {
		style = pick(t.FocusStyle, Theme.InputFocus)
	}
	c.Fill(layout.Rect{W: size.W, H: size.H}, style)
	lines := splitLines(c.Method(), t.Value)
	st.cursor = min(st.cursor, len(t.Value))
	row, col := locate(lines, st.cursor)
	st.view, st.total = size.H, len(lines)
	st.reveal(row, row)
	if col < st.left {
		st.left = col
	}
	if col >= st.left+size.W {
		st.left = col - size.W + 1
	}
	if t.Value == "" && t.Placeholder != "" {
		c.Text(0, 0, t.Placeholder, merge(style, Theme.Placeholder))
	}
	for r := 0; r < size.H && st.offset+r < len(lines); r++ {
		x := -st.left
		for _, u := range lines[st.offset+r].units {
			if u.width > 0 && x >= 0 && x+u.width <= size.W {
				c.Set(x, r, t.Value[u.start:u.end], u.width, style)
			}
			x += u.width
		}
	}
	st.at = layout.Point{X: col - st.left, Y: row - st.offset}
}

func (t TextArea) Cursor(n *view.Node) (layout.Point, bool) {
	return n.State().(*textAreaState).at, true
}

func (t TextArea) Handle(n *view.Node, e view.Event) bool {
	if paste, ok := e.(view.PasteEvent); ok && t.OnPaste != nil && t.OnPaste(paste.Text) {
		return true
	}
	st := n.State().(*textAreaState)
	value := t.Value
	lines := splitLines(n.Tree().Method, value)
	st.cursor = min(st.cursor, len(value))
	cur := st.cursor
	row, col := locate(lines, cur)
	line := lines[row]

	edit := func(v string, at int) {
		st.cursor, st.want = at, -1
		if t.OnChange != nil && v != value {
			t.OnChange(v)
		}
	}
	insert := func(text string) {
		if t.OnChange != nil {
			text = limitInsert(text, value, t.MaxLength)
			edit(value[:cur]+text+value[cur:], cur+len(text))
		}
	}
	vertical := func(to int) {
		if st.want < 0 {
			st.want = col
		}
		to = min(max(to, 0), len(lines)-1)
		st.cursor = offsetAt(lines[to], st.want)
	}
	prev := func() int {
		for i := len(line.units) - 1; i >= 0; i-- {
			if line.units[i].start < cur {
				return line.units[i].start
			}
		}
		return max(line.start-1, 0) // join with the line above
	}
	next := func() int {
		for _, u := range line.units {
			if u.end > cur {
				return u.end
			}
		}
		return min(line.end+1, len(value))
	}

	switch e := e.(type) {
	case view.PasteEvent:
		if t.OnChange == nil {
			return false
		}
		insert(pasteText(e.Text, true))
		return true
	case *view.PointerEvent:
		switch {
		case e.Kind == view.Wheel:
			st.by(wheel(e))
		case (e.Kind == view.Press || e.Kind == view.Drag) && e.Button == input.ButtonLeft:
			r := min(max(st.offset+e.Local.Y, 0), len(lines)-1)
			st.cursor, st.want = offsetAt(lines[r], e.Local.X+st.left), -1
		default:
			return e.Kind == view.Release
		}
		return true
	case view.KeyEvent:
		k := e.Key
		ctrl, alt := k.Mods&input.ModCtrl != 0, k.Mods&input.ModAlt != 0
		switch {
		case k.Code == input.KeyUp:
			vertical(row - 1)
			return true
		case k.Code == input.KeyDown:
			vertical(row + 1)
			return true
		case k.Code == input.KeyPageUp:
			vertical(row - max(st.view-1, 1))
			return true
		case k.Code == input.KeyPageDown:
			vertical(row + max(st.view-1, 1))
			return true
		}
		st.want = -1
		switch {
		case k.Code == input.KeyLeft, k.Code == input.KeyRune && ctrl && k.Rune == 'b':
			st.cursor = prev()
		case k.Code == input.KeyRight, k.Code == input.KeyRune && ctrl && k.Rune == 'f':
			st.cursor = next()
		case k.Code == input.KeyHome && ctrl:
			st.cursor = 0
		case k.Code == input.KeyEnd && ctrl:
			st.cursor = len(value)
		case k.Code == input.KeyHome, k.Code == input.KeyRune && ctrl && k.Rune == 'a':
			st.cursor = line.start
		case k.Code == input.KeyEnd, k.Code == input.KeyRune && ctrl && k.Rune == 'e':
			st.cursor = line.end
		case k.Code == input.KeyBackspace:
			at := prev()
			edit(value[:at]+value[cur:], at)
		case k.Code == input.KeyDelete, k.Code == input.KeyRune && ctrl && k.Rune == 'd':
			edit(value[:cur]+value[next():], cur)
		case k.Code == input.KeyRune && ctrl && k.Rune == 'k':
			end := line.end
			if cur == end {
				end = next() // at the end of a line, join the next one
			}
			edit(value[:cur]+value[end:], cur)
		case k.Code == input.KeyEnter && k.Mods == 0:
			insert("\n")
		case k.Code == input.KeyRune && !ctrl && !alt:
			insert(string(k.Rune))
		default:
			return false
		}
		return true
	}
	return false
}
