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

// TabBar is a row of tab titles. Clicking one calls OnChange; while
// focused, Left and Right move between tabs.
type TabBar struct {
	Titles   []string
	Active   int
	OnChange func(i int)
}

// Tabs returns a tab bar above content, which shows the active tab.
func Tabs(titles []string, active int, onChange func(int), content view.Element) view.Element {
	return Column(
		view.E(TabBar{Titles: titles, Active: active, OnChange: onChange}),
		Grow(1, content),
	)
}

func (t TabBar) Layout(n *view.Node, c layout.Constraints) layout.Size {
	return layout.Size{W: c.MaxW, H: 1}
}

// spans returns each tab's start column and width.
func (t TabBar) spans(n *view.Node) (starts, widths []int) {
	x := 0
	for _, title := range t.Titles {
		w := n.Tree().Method.Width(title) + 2
		starts, widths = append(starts, x), append(widths, w)
		x += w + 1
	}
	return starts, widths
}

func (t TabBar) Draw(n *view.Node, c *view.Canvas) {
	starts, widths := t.spans(n)
	for i, title := range t.Titles {
		style := Theme.Tab
		if i == t.Active {
			style = Theme.TabActive
			if !n.Focused() {
				style.Attrs &^= cell.Bold
			}
		}
		c.Fill(layout.Rect{X: starts[i], W: widths[i], H: 1}, style)
		c.TextLimit(starts[i]+1, 0, widths[i]-1, title, style)
	}
}

func (t TabBar) Focusable() bool { return t.OnChange != nil }

func (t TabBar) change(i int) {
	if t.OnChange != nil && i >= 0 && i < len(t.Titles) && i != t.Active {
		t.OnChange(i)
	}
}

func (t TabBar) Handle(n *view.Node, e view.Event) bool {
	switch e := e.(type) {
	case *view.PointerEvent:
		if e.Kind != view.Press || e.Button != input.ButtonLeft {
			return e.Kind == view.Release || e.Kind == view.Drag
		}
		starts, widths := t.spans(n)
		for i := range starts {
			if e.Local.X >= starts[i] && e.Local.X < starts[i]+widths[i] {
				t.change(i)
			}
		}
		return true
	case view.KeyEvent:
		switch {
		case e.Mods != 0:
			return false
		case e.Code == input.KeyLeft:
			t.change(t.Active - 1)
		case e.Code == input.KeyRight:
			t.change(t.Active + 1)
		default:
			return false
		}
		return true
	}
	return false
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
