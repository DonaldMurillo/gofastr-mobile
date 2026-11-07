package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// ButtonVariant selects a button's base style.
type ButtonVariant int

const (
	ButtonDefault ButtonVariant = iota
	ButtonPrimary
	ButtonDanger
)

// Button runs OnClick when clicked, or on Enter or Space while focused. A
// click counts only if the pointer is released over the button.
type Button struct {
	Label       string
	OnClick     func()
	Disabled    bool
	Variant     ButtonVariant
	AutoFocused bool
	// Styles override the theme when non-zero.
	Style, HoverStyle, FocusStyle, PressStyle cell.Style
}

type buttonState struct{ pressed bool }

// NewButton returns a button element.
func NewButton(label string, onClick func()) view.Element {
	return view.E(Button{Label: label, OnClick: onClick})
}

func (Button) NewState() any { return &buttonState{} }

func (b Button) Layout(n *view.Node, c layout.Constraints) layout.Size {
	return layout.Size{W: n.Tree().Method.Width(b.Label) + 2, H: 1}
}

func pick(override, fallback cell.Style) cell.Style {
	if override != (cell.Style{}) {
		return override
	}
	return fallback
}

func (b Button) Draw(n *view.Node, c *view.Canvas) {
	st := n.State().(*buttonState)
	base := Theme.Button
	switch b.Variant {
	case ButtonPrimary:
		base = Theme.ButtonPrimary
	case ButtonDanger:
		base = Theme.ButtonDanger
	}
	style := pick(b.Style, base)
	switch {
	case b.Disabled:
		style = Theme.Muted
	case st.pressed:
		style = pick(b.PressStyle, Theme.ButtonPress)
	case n.Focused():
		style = pick(b.FocusStyle, Theme.ButtonFocus)
	case n.Hovered():
		style = pick(b.HoverStyle, Theme.ButtonHover)
	}
	size := c.Size()
	c.Fill(layout.Rect{W: size.W, H: size.H}, style)
	w := c.Method().Width(b.Label)
	c.TextLimit(layout.Center.Offset(size.W, w), size.H/2, size.W, b.Label, style)
}

func (b Button) Focusable() bool { return !b.Disabled }
func (b Button) Hoverable() bool { return !b.Disabled }
func (b Button) AutoFocus() bool { return b.AutoFocused }

func (b Button) Handle(n *view.Node, e view.Event) bool {
	if b.Disabled {
		return false
	}
	st := n.State().(*buttonState)
	switch e := e.(type) {
	case *view.PointerEvent:
		inside := n.Rect().Contains(e.Pos)
		switch e.Kind {
		case view.Press:
			if e.Button != input.ButtonLeft {
				return false
			}
			st.pressed = true
		case view.Drag:
			st.pressed = inside
		case view.Release:
			fire := st.pressed && inside
			st.pressed = false
			if fire && b.OnClick != nil {
				b.OnClick()
			}
		case view.Enter, view.Leave:
		default:
			return false
		}
		return true
	case view.KeyEvent:
		if e.Mods == 0 && (e.Code == input.KeyEnter || e.Code == input.KeyRune && e.Rune == ' ') {
			if b.OnClick != nil {
				b.OnClick()
			}
			return true
		}
	}
	return false
}

// handlerWrap passes layout through to its only child and handles events
// for it.
type handlerWrap struct {
	key   func(input.Key) bool
	click func(*view.PointerEvent)
	hover bool
}

func (h handlerWrap) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if n.Len() == 0 {
		return layout.Size{}
	}
	return n.LayoutChild(0, c)
}
func (handlerWrap) Draw(*view.Node, *view.Canvas) {}
func (h handlerWrap) Hoverable() bool             { return h.hover }

func (h handlerWrap) Handle(n *view.Node, e view.Event) bool {
	switch e := e.(type) {
	case view.KeyEvent:
		return h.key != nil && h.key(e.Key)
	case *view.PointerEvent:
		if h.click == nil {
			return false
		}
		switch e.Kind {
		case view.Press:
			return e.Button == input.ButtonLeft
		case view.Release:
			if n.Rect().Contains(e.Pos) {
				h.click(e)
			}
			return true
		case view.Drag:
			return true
		}
	}
	return false
}

// OnKey handles keys that the focused node inside e, or nothing focused,
// left unconsumed. fn returns true when it used the key.
func OnKey(fn func(input.Key) bool, e view.Element) view.Element {
	return view.E(handlerWrap{key: fn}, e).WithKey(e.Key)
}

// OnClick runs fn when e is clicked with the left button.
func OnClick(fn func(*view.PointerEvent), e view.Element) view.Element {
	return view.E(handlerWrap{click: fn, hover: true}, e).WithKey(e.Key)
}
