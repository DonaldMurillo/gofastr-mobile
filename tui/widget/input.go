package widget

import (
	"strings"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Input is a single-line text field. It is controlled: Value is the text,
// and edits call OnChange with the new text, which the program stores and
// passes back as Value. Without OnChange the field is read-only.
//
// Keys: arrows, Home and End, ctrl or alt with arrows for words, readline
// bindings (ctrl+a, ctrl+e, ctrl+b, ctrl+f, ctrl+d, ctrl+k, ctrl+u, ctrl+w,
// alt+b, alt+f), Backspace and Delete, and Enter for OnSubmit.
type Input struct {
	Value       string
	Placeholder string
	OnChange    func(string)
	OnSubmit    func(string)
	AutoFocused bool
	// Mask replaces each character on screen, for passwords.
	Mask rune
	// Styles override the theme when non-zero.
	Style, FocusStyle cell.Style
}

type inputState struct {
	cursor int // byte offset into the value
	scroll int // first visible column
	col    int // cursor column on screen, after the last draw
}

func (Input) NewState() any { return &inputState{} }

func (in Input) Focusable() bool { return true }
func (in Input) AutoFocus() bool { return in.AutoFocused }

func (in Input) Layout(n *view.Node, c layout.Constraints) layout.Size {
	return layout.Size{W: c.MaxW, H: 1}
}

// unit is one drawable unit of the value.
type unit struct{ start, end, width int }

func units(m uni.Method, s string) []unit {
	var out []unit
	at := 0
	for u, w := range m.Units(s) {
		out = append(out, unit{at, at + len(u), w})
		at += len(u)
	}
	return out
}

// snap moves offset back to the nearest unit boundary.
func snap(us []unit, offset int) int {
	for i := len(us) - 1; i >= 0; i-- {
		if us[i].start <= offset {
			if offset >= us[i].end {
				return us[i].end
			}
			return us[i].start
		}
	}
	return 0
}

// look returns what unit u draws as and its width.
func (in Input) look(value string, u unit) (string, int) {
	if in.Mask == 0 {
		return value[u.start:u.end], u.width
	}
	return string(in.Mask), uni.RuneWidth(in.Mask)
}

func (in Input) Draw(n *view.Node, c *view.Canvas) {
	st := n.State().(*inputState)
	size := c.Size()
	style := pick(in.Style, Theme.Input)
	if n.Focused() {
		style = pick(in.FocusStyle, Theme.InputFocus)
	}
	c.Fill(layout.Rect{W: size.W, H: size.H}, style)
	if in.Value == "" {
		st.cursor, st.scroll, st.col = 0, 0, 0
		if in.Placeholder != "" {
			c.Text(0, 0, in.Placeholder, merge(style, Theme.Placeholder))
		}
		return
	}
	us := units(c.Method(), in.Value)
	st.cursor = snap(us, min(st.cursor, len(in.Value)))
	col := 0
	for _, u := range us {
		if u.start >= st.cursor {
			break
		}
		_, w := in.look(in.Value, u)
		col += w
	}
	// Keep the cursor visible, with one column for it past the end.
	if col < st.scroll {
		st.scroll = col
	}
	if col >= st.scroll+size.W {
		st.scroll = col - size.W + 1
	}
	st.scroll = max(st.scroll, 0)
	x := -st.scroll
	for _, u := range us {
		text, w := in.look(in.Value, u)
		if w > 0 && x >= 0 && x+w <= size.W {
			c.Set(x, 0, text, w, style)
		}
		x += w
	}
	st.col = col - st.scroll
}

func (in Input) Cursor(n *view.Node) (layout.Point, bool) {
	return layout.Point{X: n.State().(*inputState).col}, true
}

func isSpace(s string) bool { return s == " " || s == "\t" }

// wordBack returns the offset of the start of the word before offset.
func wordBack(s string, us []unit, offset int) int {
	i := len(us) - 1
	for i >= 0 && us[i].start >= offset {
		i--
	}
	for i >= 0 && isSpace(s[us[i].start:us[i].end]) {
		i--
	}
	for i >= 0 && !isSpace(s[us[i].start:us[i].end]) {
		i--
	}
	if i < 0 {
		return 0
	}
	return us[i].end
}

// wordForward returns the offset of the end of the word after offset.
func wordForward(s string, us []unit, offset int) int {
	i := 0
	for i < len(us) && us[i].end <= offset {
		i++
	}
	for i < len(us) && isSpace(s[us[i].start:us[i].end]) {
		i++
	}
	for i < len(us) && !isSpace(s[us[i].start:us[i].end]) {
		i++
	}
	if i >= len(us) {
		return len(s)
	}
	return us[i].start
}

func (in Input) Handle(n *view.Node, e view.Event) bool {
	st := n.State().(*inputState)
	m := n.Tree().Method
	value := in.Value
	us := units(m, value)
	st.cursor = snap(us, min(st.cursor, len(value)))
	cur := st.cursor

	prev := func() int {
		for i := len(us) - 1; i >= 0; i-- {
			if us[i].start < cur {
				return us[i].start
			}
		}
		return 0
	}
	next := func() int {
		for _, u := range us {
			if u.end > cur {
				return u.end
			}
		}
		return len(value)
	}
	edit := func(v string, at int) {
		st.cursor = at
		if in.OnChange != nil && v != value {
			in.OnChange(v)
		}
	}
	insert := func(text string) {
		if in.OnChange == nil {
			return
		}
		edit(value[:cur]+text+value[cur:], cur+len(text))
	}

	switch e := e.(type) {
	case view.PasteEvent:
		insert(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(e.Text))
		return true
	case *view.PointerEvent:
		if e.Kind != view.Press && e.Kind != view.Drag || e.Button != input.ButtonLeft {
			return e.Kind == view.Release
		}
		target := e.Local.X + st.scroll
		col := 0
		st.cursor = len(value)
		for _, u := range us {
			_, w := in.look(value, u)
			if target < col+(w+1)/2 {
				st.cursor = u.start
				break
			}
			col += w
		}
		return true
	case view.KeyEvent:
		k := e.Key
		ctrl, alt := k.Mods&input.ModCtrl != 0, k.Mods&input.ModAlt != 0
		switch {
		case k.Code == input.KeyLeft && (ctrl || alt), k.Code == input.KeyRune && alt && k.Rune == 'b':
			st.cursor = wordBack(value, us, cur)
		case k.Code == input.KeyRight && (ctrl || alt), k.Code == input.KeyRune && alt && k.Rune == 'f':
			st.cursor = wordForward(value, us, cur)
		case k.Code == input.KeyLeft, k.Code == input.KeyRune && ctrl && k.Rune == 'b':
			st.cursor = prev()
		case k.Code == input.KeyRight, k.Code == input.KeyRune && ctrl && k.Rune == 'f':
			st.cursor = next()
		case k.Code == input.KeyHome, k.Code == input.KeyRune && ctrl && k.Rune == 'a':
			st.cursor = 0
		case k.Code == input.KeyEnd, k.Code == input.KeyRune && ctrl && k.Rune == 'e':
			st.cursor = len(value)
		case k.Code == input.KeyBackspace && (ctrl || alt), k.Code == input.KeyRune && ctrl && k.Rune == 'w':
			at := wordBack(value, us, cur)
			edit(value[:at]+value[cur:], at)
		case k.Code == input.KeyBackspace:
			at := prev()
			edit(value[:at]+value[cur:], at)
		case k.Code == input.KeyDelete, k.Code == input.KeyRune && ctrl && k.Rune == 'd':
			edit(value[:cur]+value[next():], cur)
		case k.Code == input.KeyRune && ctrl && k.Rune == 'k':
			edit(value[:cur], cur)
		case k.Code == input.KeyRune && ctrl && k.Rune == 'u':
			edit(value[cur:], 0)
		case k.Code == input.KeyEnter:
			if in.OnSubmit == nil {
				return false
			}
			in.OnSubmit(value)
		case k.Code == input.KeyRune && !ctrl && !alt:
			insert(string(k.Rune))
		default:
			return false
		}
		return true
	}
	return false
}
