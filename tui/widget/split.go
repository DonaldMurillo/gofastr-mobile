package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Split shows two panes with a one-cell divider the user can drag.
// Horizontal puts them side by side; Vertical stacks them. The split
// keeps its ratio when the space resizes.
type Split struct {
	Dir           Direction
	First, Second view.Element
	// Percent is the first pane's starting share; 50 when zero.
	Percent int
	// Min is the smallest size of either pane; 1 when zero.
	Min int
}

type splitState struct {
	ratio    float64 // first pane's share of the space without the divider
	set      bool
	first    int // first pane's size after the last layout
	dragging bool
}

// SplitPanes returns a split of a and b.
func SplitPanes(dir Direction, a, b view.Element) view.Element {
	return view.E(Split{Dir: dir, First: a, Second: b})
}

func (Split) NewState() any   { return &splitState{} }
func (Split) BuildsChildren() {}

func (s Split) main(sz layout.Size) int {
	if s.Dir == Horizontal {
		return sz.W
	}
	return sz.H
}

func (s Split) Layout(n *view.Node, c layout.Constraints) layout.Size {
	st := n.State().(*splitState)
	size := c.Max()
	if !st.set {
		p := s.Percent
		if p <= 0 || p >= 100 {
			p = 50
		}
		st.ratio, st.set = float64(p)/100, true
	}
	avail := max(s.main(size)-1, 0)
	lo := min(max(s.Min, 1), avail/2)
	st.first = min(max(int(st.ratio*float64(avail)+0.5), lo), avail-lo)
	n.SetChildren([]view.Element{s.First, view.E(divider{dir: s.Dir}), s.Second})
	parts := [3]int{st.first, 1, avail - st.first}
	at := 0
	for i, part := range parts {
		if s.Dir == Horizontal {
			n.LayoutChild(i, layout.Tight(layout.Size{W: part, H: size.H}))
			n.Place(i, layout.Point{X: at})
		} else {
			n.LayoutChild(i, layout.Tight(layout.Size{W: size.W, H: part}))
			n.Place(i, layout.Point{Y: at})
		}
		at += part
	}
	return size
}

func (Split) Draw(*view.Node, *view.Canvas) {}

// divider is the draggable line between a split's panes.
type divider struct{ dir Direction }

func (d divider) Layout(_ *view.Node, c layout.Constraints) layout.Size { return c.Max() }

func (d divider) Draw(n *view.Node, c *view.Canvas) {
	style := Theme.Divider
	if n.Parent().State().(*splitState).dragging || n.Hovered() {
		style = Theme.DividerActive
	}
	size := c.Size()
	if d.dir == Horizontal {
		for y := 0; y < size.H; y++ {
			c.Set(0, y, "│", 1, style)
		}
		return
	}
	for x := 0; x < size.W; x++ {
		c.Set(x, 0, "─", 1, style)
	}
}

func (divider) Hoverable() bool { return true }

func (d divider) Handle(n *view.Node, e view.Event) bool {
	split := n.Parent()
	st := split.State().(*splitState)
	p, ok := e.(*view.PointerEvent)
	if !ok {
		return false
	}
	switch p.Kind {
	case view.Press:
		if p.Button != input.ButtonLeft {
			return false
		}
		st.dragging = true
	case view.Drag:
		if !st.dragging {
			return false
		}
		at, avail := p.Pos.Y-split.Rect().Y, split.Size().H-1
		if d.dir == Horizontal {
			at, avail = p.Pos.X-split.Rect().X, split.Size().W-1
		}
		if avail > 0 {
			st.ratio = min(max(float64(at)/float64(avail), 0), 1)
		}
	case view.Release:
		st.dragging = false
	case view.Enter, view.Leave:
	default:
		return false
	}
	return true
}

// Sparkline draws the last values that fit as vertical bars, newest on
// the right. It is as wide as it is allowed and one row tall unless its
// constraints ask for more.
type Sparkline struct {
	Values []float64
	// Max is the value of a full bar; the largest value when zero.
	Max   float64
	Style cell.Style
}

var bars = [...]string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

func (s Sparkline) Layout(_ *view.Node, c layout.Constraints) layout.Size {
	return layout.Size{W: c.MaxW, H: max(c.MinH, min(1, c.MaxH))}
}

func (s Sparkline) Draw(_ *view.Node, c *view.Canvas) {
	size := c.Size()
	if size.W <= 0 || size.H <= 0 {
		return
	}
	values := s.Values
	if len(values) > size.W {
		values = values[len(values)-size.W:]
	}
	top := s.Max
	if top <= 0 {
		for _, v := range values {
			top = max(top, v)
		}
	}
	style := pick(s.Style, Theme.Accent)
	levels := size.H * 8
	x := size.W - len(values)
	for i, v := range values {
		level := 0
		if top > 0 && v > 0 {
			level = max(int(v/top*float64(levels)+0.5), 1)
		}
		level = min(level, levels)
		for row := 0; row < size.H; row++ {
			fill := min(max(level-row*8, 0), 8)
			if fill > 0 {
				c.Set(x+i, size.H-1-row, bars[fill], 1, style)
			}
		}
	}
}
