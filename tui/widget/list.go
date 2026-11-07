package widget

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// List shows Count rows and builds only the visible ones, so it handles
// any number of rows. Selection is controlled: Selected is the selected
// row (-1 for none), and OnSelect asks to change it.
//
// While focused it moves the selection with arrows, j and k, page keys,
// Home and End, and activates with Enter. A click selects and a double
// click activates. When Selected changes, the list scrolls to show it.
type List struct {
	Count int
	// Row builds row i. It is called only for visible rows.
	Row func(i int, selected bool) view.Element
	// Key returns a stable identity for row i, so row state follows the
	// data when rows are inserted or removed. The index is used when nil.
	Key         func(i int) string
	Selected    int
	OnSelect    func(i int)
	OnActivate  func(i int)
	RowHeight   int // 1 when zero
	NoBar       bool
	AutoFocused bool
	// Unfocusable lists do not take focus, for example when another
	// widget drives the selection.
	Unfocusable bool
}

type listState struct {
	scroller
	lastSelected int
	first        bool
}

func (List) NewState() any   { return &listState{lastSelected: -1, first: true} }
func (List) BuildsChildren() {}

func (l List) Focusable() bool { return !l.Unfocusable }
func (l List) AutoFocus() bool { return l.AutoFocused }

func (l List) rowHeight() int { return max(l.RowHeight, 1) }

func (l List) Layout(n *view.Node, c layout.Constraints) layout.Size {
	st := n.State().(*listState)
	size := c.Max()
	h := l.rowHeight()
	st.view, st.total = max(size.H/h, 1), l.Count
	if size.H < h {
		st.view = 0
	}
	if l.Selected != st.lastSelected || st.first {
		if l.Selected >= 0 && l.Selected < l.Count {
			st.reveal(l.Selected, l.Selected)
		}
		st.lastSelected, st.first = l.Selected, false
	}
	st.clamp()

	width := size.W
	if !l.NoBar && st.overflows() && width > 1 {
		width--
	}
	visible := min(st.view, max(l.Count-st.offset, 0))
	rows := make([]view.Element, visible)
	for r := range rows {
		i := st.offset + r
		key := strconv.Itoa(i)
		if l.Key != nil {
			key = l.Key(i)
		}
		var row view.Element
		if l.Row != nil {
			row = l.Row(i, i == l.Selected)
		}
		rows[r] = view.E(listRow{}, row).WithKey(key)
	}
	n.SetChildren(rows)
	for r := range rows {
		n.LayoutChild(r, layout.Tight(layout.Size{W: width, H: h}))
		n.Place(r, layout.Point{Y: r * h})
	}
	return size
}

// listRow gives a row element its full row size.
type listRow struct{}

func (listRow) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if n.Len() > 0 {
		n.LayoutChild(0, c)
	}
	return c.Max()
}
func (listRow) Draw(*view.Node, *view.Canvas) {}

func (l List) selectedStyle(n *view.Node) cell.Style {
	if n.Focused() {
		return Theme.Selected
	}
	return Theme.SelectedBlurred
}

func (l List) Draw(*view.Node, *view.Canvas) {}

func (l List) DrawOver(n *view.Node, c *view.Canvas) {
	st := n.State().(*listState)
	h := l.rowHeight()
	if r := l.Selected - st.offset; l.Selected >= 0 && r >= 0 && r < st.view {
		sel := l.selectedStyle(n)
		width := c.Size().W
		if !l.NoBar && st.overflows() {
			width--
		}
		c.Restyle(layout.Rect{Y: r * h, W: width, H: h}, func(s cell.Style) cell.Style {
			if sel.Attrs&cell.Reverse != 0 {
				s.Attrs |= cell.Reverse
				return s
			}
			if s.Fg.IsDefault() || s.Fg == cell.BrightBlack {
				s.Fg = sel.Fg
			}
			s.Bg = sel.Bg
			s.Attrs |= sel.Attrs
			return s
		})
	}
	if !l.NoBar {
		st.draw(c, c.Size().W-1, 0, c.Size().H)
	}
}

func (l List) choose(i int) {
	if l.Count == 0 || l.OnSelect == nil {
		return
	}
	i = min(max(i, 0), l.Count-1)
	if i != l.Selected {
		l.OnSelect(i)
	}
}

func (l List) Handle(n *view.Node, e view.Event) bool {
	st := n.State().(*listState)
	switch e := e.(type) {
	case *view.PointerEvent:
		if e.Kind == view.Wheel {
			st.by(wheel(e))
			return true
		}
		if !l.NoBar && st.pointer(e, n.Size().W-1, 0, n.Size().H) {
			return true
		}
		if e.Kind == view.Press && e.Button == input.ButtonLeft {
			i := st.offset + e.Local.Y/l.rowHeight()
			if i < l.Count {
				l.choose(i)
				st.lastSelected = i // the user can see it; do not scroll
				if e.Clicks == 2 && l.OnActivate != nil {
					l.OnActivate(i)
				}
			}
			return true
		}
		return e.Kind == view.Release || e.Kind == view.Drag
	case view.KeyEvent:
		k := e.Key
		if k.Mods != 0 {
			return false
		}
		sel := l.Selected
		switch {
		case k.Code == input.KeyUp, k.Code == input.KeyRune && k.Rune == 'k':
			l.choose(sel - 1)
		case k.Code == input.KeyDown, k.Code == input.KeyRune && k.Rune == 'j':
			l.choose(sel + 1)
		case k.Code == input.KeyPageUp:
			l.choose(sel - max(st.view-1, 1))
		case k.Code == input.KeyPageDown:
			l.choose(sel + max(st.view-1, 1))
		case k.Code == input.KeyHome, k.Code == input.KeyRune && k.Rune == 'g':
			l.choose(0)
		case k.Code == input.KeyEnd, k.Code == input.KeyRune && k.Rune == 'G':
			l.choose(l.Count - 1)
		case k.Code == input.KeyEnter && l.OnActivate != nil && sel >= 0 && sel < l.Count:
			l.OnActivate(sel)
		default:
			return false
		}
		return true
	}
	return false
}
