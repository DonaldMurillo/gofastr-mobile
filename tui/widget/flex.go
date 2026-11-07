package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Direction is a main axis.
type Direction uint8

const (
	Horizontal Direction = iota
	Vertical
)

// Flex lays children out in a row or column. Each child's main-axis size
// comes from wrapping it in Item (Grow, Fixed, ...); unwrapped children
// size to their content. On the cross axis children stretch unless Align
// says otherwise.
type Flex struct {
	Dir     Direction
	Gap     int
	Align   layout.Align // cross axis; Stretch by default
	Justify layout.Align // main axis, when children leave space: Start, Center or End
	Style   cell.Style   // background
}

// Row lays children out left to right.
func Row(children ...view.Element) view.Element {
	return view.E(Flex{Dir: Horizontal, Align: layout.Stretch}, children...)
}

// Column lays children out top to bottom.
func Column(children ...view.Element) view.Element {
	return view.E(Flex{Dir: Vertical, Align: layout.Stretch}, children...)
}

// Item sets how a flex child is sized along the main axis.
type Item struct {
	Sizing layout.Sizing
	Align  layout.Align // overrides the container's cross alignment when set
	align  bool
}

func (it Item) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if n.Len() == 0 {
		return layout.Size{}
	}
	return n.LayoutChild(0, c)
}
func (Item) Draw(*view.Node, *view.Canvas) {}

// Grow makes e take a weighted share of the free space.
func Grow(weight int, e view.Element) view.Element {
	return view.E(Item{Sizing: layout.Grow(weight)}, e).WithKey(e.Key)
}

// Fixed gives e exactly n cells along the main axis.
func Fixed(n int, e view.Element) view.Element {
	return view.E(Item{Sizing: layout.Fixed(n)}, e).WithKey(e.Key)
}

// Percent gives e p percent of the main axis.
func Percent(p int, e view.Element) view.Element {
	return view.E(Item{Sizing: layout.Percent(p)}, e).WithKey(e.Key)
}

// Sized wraps e with any sizing.
func Sized(s layout.Sizing, e view.Element) view.Element {
	return view.E(Item{Sizing: s}, e).WithKey(e.Key)
}

// Aligned overrides the cross-axis alignment of one flex child.
func Aligned(a layout.Align, e view.Element) view.Element {
	return view.E(Item{Sizing: layout.Auto(), Align: a, align: true}, e).WithKey(e.Key)
}

// Spacer takes the free space in a row or column.
func Spacer() view.Element { return view.E(Item{Sizing: layout.Grow(1)}) }

func (f Flex) axis(s layout.Size) (main, cross int) {
	if f.Dir == Horizontal {
		return s.W, s.H
	}
	return s.H, s.W
}

func (f Flex) size(main, cross int) layout.Size {
	if f.Dir == Horizontal {
		return layout.Size{W: main, H: cross}
	}
	return layout.Size{W: cross, H: main}
}

func (f Flex) constraints(mainMin, mainMax, crossMin, crossMax int) layout.Constraints {
	if f.Dir == Horizontal {
		return layout.Constraints{MinW: mainMin, MaxW: mainMax, MinH: crossMin, MaxH: crossMax}
	}
	return layout.Constraints{MinW: crossMin, MaxW: crossMax, MinH: mainMin, MaxH: mainMax}
}

func (f Flex) Layout(n *view.Node, c layout.Constraints) layout.Size {
	mainMax, crossMax := f.axis(c.Max())
	_, crossMin := f.axis(layout.Size{W: c.MinW, H: c.MinH})
	items := make([]layout.Sizing, n.Len())
	aligns := make([]layout.Align, n.Len())
	for i := range items {
		items[i], aligns[i] = layout.Auto(), f.Align
		if it, ok := n.Child(i).Widget().(Item); ok {
			items[i] = it.Sizing
			if it.align {
				aligns[i] = it.Align
			}
		}
	}
	cross := func(i int) (lo, hi int) {
		if aligns[i] == layout.Stretch {
			return crossMax, crossMax
		}
		return 0, crossMax
	}
	sizes := layout.Distribute(mainMax, f.Gap, items, func(i, limit int) int {
		lo, hi := cross(i)
		s := n.LayoutChild(i, f.constraints(0, limit, lo, hi))
		m, _ := f.axis(s)
		return m
	})
	used := 0
	for i, s := range sizes {
		used += s
		if i > 0 {
			used += f.Gap
		}
	}
	main := used
	if c.MinW == c.MaxW && f.Dir == Horizontal || c.MinH == c.MaxH && f.Dir == Vertical {
		main = mainMax
	}
	pos := f.Justify.Offset(main, used)
	crossUsed := 0
	childCross := make([]int, len(sizes))
	for i, s := range sizes {
		lo, hi := cross(i)
		got := n.LayoutChild(i, f.constraints(s, s, lo, hi))
		_, childCross[i] = f.axis(got)
		crossUsed = max(crossUsed, childCross[i])
	}
	crossSize := max(crossUsed, crossMin)
	if f.Align == layout.Stretch {
		crossSize = crossMax
	}
	for i, s := range sizes {
		off := aligns[i].Offset(crossSize, childCross[i])
		if f.Dir == Horizontal {
			n.Place(i, layout.Point{X: pos, Y: off})
		} else {
			n.Place(i, layout.Point{X: off, Y: pos})
		}
		pos += s + f.Gap
	}
	return f.size(main, crossSize)
}

func (f Flex) Draw(_ *view.Node, c *view.Canvas) {
	if f.Style != (cell.Style{}) {
		c.Fill(layout.Rect{W: c.Size().W, H: c.Size().H}, f.Style)
	}
}
