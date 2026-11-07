package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// WheelStep is how many rows one wheel notch scrolls.
var WheelStep = 3

// scroller is the vertical scroll position shared by Scroll, List and
// TextArea, with scrollbar drawing and dragging.
type scroller struct {
	offset   int // first visible row
	total    int // content rows
	view     int // visible rows
	dragging bool
	grab     int // row within the thumb where the drag started
}

func (s *scroller) max() int { return max(s.total-s.view, 0) }

func (s *scroller) clamp() { s.offset = min(max(s.offset, 0), s.max()) }

func (s *scroller) by(rows int) bool {
	before := s.offset
	s.offset += rows
	s.clamp()
	return s.offset != before
}

// reveal scrolls so rows first..last are visible, preferring first.
func (s *scroller) reveal(first, last int) {
	if last >= s.offset+s.view {
		s.offset = last - s.view + 1
	}
	if first < s.offset {
		s.offset = first
	}
	s.clamp()
}

func (s *scroller) overflows() bool { return s.total > s.view }

// thumb returns the thumb's first row and length on a bar of height h.
func (s *scroller) thumb(h int) (start, length int) {
	if !s.overflows() || h <= 0 {
		return 0, h
	}
	length = max(h*s.view/s.total, 1)
	span := h - length
	if m := s.max(); m > 0 {
		start = (s.offset*span + m/2) / m
	}
	return start, length
}

func (s *scroller) draw(c *view.Canvas, x, y, h int) {
	if !s.overflows() {
		return
	}
	start, length := s.thumb(h)
	for row := 0; row < h; row++ {
		if row >= start && row < start+length {
			c.Set(x, y+row, "┃", 1, Theme.ScrollThumb)
		} else {
			c.Set(x, y+row, "│", 1, Theme.ScrollTrack)
		}
	}
}

// pointer handles events on a bar at column x of height h (node
// coordinates). It returns whether it used the event.
func (s *scroller) pointer(e *view.PointerEvent, x, y, h int) bool {
	row := e.Local.Y - y
	switch e.Kind {
	case view.Press:
		if !s.overflows() || e.Local.X != x || e.Button != input.ButtonLeft {
			return false
		}
		start, length := s.thumb(h)
		switch {
		case row < start:
			s.by(-s.view)
		case row >= start+length:
			s.by(s.view)
		default:
			s.dragging, s.grab = true, row-start
		}
		return true
	case view.Drag:
		if !s.dragging {
			return false
		}
		_, length := s.thumb(h)
		if span := h - length; span > 0 {
			s.offset = ((row-s.grab)*s.max() + span/2) / span
			s.clamp()
		}
		return true
	case view.Release:
		if !s.dragging {
			return false
		}
		s.dragging = false
		return true
	}
	return false
}

// keys handles scrolling keys; it returns whether it used the key.
func (s *scroller) keys(k input.Key) bool {
	if k.Mods != 0 {
		return false
	}
	switch k.Code {
	case input.KeyUp:
		s.by(-1)
	case input.KeyDown:
		s.by(1)
	case input.KeyPageUp:
		s.by(-max(s.view-1, 1))
	case input.KeyPageDown:
		s.by(max(s.view-1, 1))
	case input.KeyHome:
		s.offset = 0
	case input.KeyEnd:
		s.offset = s.max()
	default:
		return false
	}
	return true
}

func wheel(e *view.PointerEvent) int {
	switch e.Button {
	case input.ButtonWheelUp:
		return -WheelStep
	case input.ButtonWheelDown:
		return WheelStep
	}
	return 0
}

// tallest is the height content inside a Scroll may take.
const tallest = 1 << 20

// Scroll shows a vertically scrollable view of its child, which must size
// itself to its content. It scrolls with the wheel, the scrollbar, and
// arrow and page keys while focused.
type Scroll struct {
	// FollowEnd keeps the view at the bottom as the content grows, while
	// it is already scrolled to the bottom, as a log view does.
	FollowEnd bool
	NoBar     bool
}

type scrollState struct {
	scroller
	atEnd bool
}

// Scrolling returns a scroll view of e.
func Scrolling(e view.Element) view.Element { return view.E(Scroll{}, e) }

func (Scroll) NewState() any { return &scrollState{atEnd: true} }

func (sc Scroll) Layout(n *view.Node, c layout.Constraints) layout.Size {
	st := n.State().(*scrollState)
	size := c.Max()
	st.view = size.H
	if n.Len() == 0 {
		st.total = 0
		return size
	}
	inner := func(w int) layout.Size {
		return n.LayoutChild(0, layout.Constraints{MinW: w, MaxW: w, MaxH: tallest})
	}
	content := inner(size.W)
	if !sc.NoBar && content.H > size.H && size.W > 1 {
		content = inner(size.W - 1)
	}
	st.total = content.H
	if sc.FollowEnd && st.atEnd {
		st.offset = st.max()
	}
	st.clamp()
	n.Place(0, layout.Point{Y: -st.offset})
	return size
}

func (Scroll) Draw(*view.Node, *view.Canvas) {}

func (sc Scroll) DrawOver(n *view.Node, c *view.Canvas) {
	if !sc.NoBar {
		st := n.State().(*scrollState)
		st.draw(c, c.Size().W-1, 0, c.Size().H)
	}
}

func (sc Scroll) Focusable() bool { return false }

func (sc Scroll) Handle(n *view.Node, e view.Event) bool {
	st := n.State().(*scrollState)
	used := false
	switch e := e.(type) {
	case *view.PointerEvent:
		if e.Kind == view.Wheel {
			used = st.by(wheel(e)) || st.overflows()
		} else if !sc.NoBar {
			used = st.pointer(e, n.Size().W-1, 0, n.Size().H)
		}
	case view.KeyEvent:
		used = st.overflows() && st.keys(e.Key)
	}
	if used {
		st.atEnd = st.offset >= st.max()
	}
	return used
}
