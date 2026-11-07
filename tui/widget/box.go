package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Border is a set of box-drawing characters.
type Border struct {
	TopLeft, Top, TopRight, Left, Right, BottomLeft, Bottom, BottomRight string
}

var (
	NoBorder      = Border{}
	BorderSingle  = Border{"┌", "─", "┐", "│", "│", "└", "─", "┘"}
	BorderRounded = Border{"╭", "─", "╮", "│", "│", "╰", "─", "╯"}
	BorderDouble  = Border{"╔", "═", "╗", "║", "║", "╚", "═", "╝"}
	BorderHeavy   = Border{"┏", "━", "┓", "┃", "┃", "┗", "━", "┛"}
	BorderASCII   = Border{"+", "-", "+", "|", "|", "+", "-", "+"}
)

func (b Border) present() bool { return b.Top != "" }

// Box draws an optional border with a title, padding and a background
// around one child. With loose constraints it shrinks to its child.
type Box struct {
	Border  Border
	Title   string
	Padding layout.Insets
	Style   cell.Style // background and text
	// BorderStyle is the border's style; Theme.Border when zero. The
	// border uses Theme.BorderFocus while a descendant has focus, unless
	// NoFocusBorder is set.
	BorderStyle   cell.Style
	NoFocusBorder bool
}

// Panel returns a rounded box with a title around child.
func Panel(title string, child view.Element) view.Element {
	return view.E(Box{Border: BorderRounded, Title: title, Padding: layout.Insets{Left: 1, Right: 1}}, child)
}

func (b Box) insets() layout.Insets {
	in := b.Padding
	if b.Border.present() {
		in.Top++
		in.Right++
		in.Bottom++
		in.Left++
	}
	return in
}

func (b Box) Layout(n *view.Node, c layout.Constraints) layout.Size {
	in := b.insets()
	content := layout.Size{}
	if n.Len() > 0 {
		content = n.LayoutChild(0, c.Deflate(in))
		n.Place(0, layout.Point{X: in.Left, Y: in.Top})
	}
	return layout.Size{W: content.W + in.Horizontal(), H: content.H + in.Vertical()}
}

// focusWithin reports whether the focused node is n or below it.
func focusWithin(n *view.Node) bool {
	for f := n.Tree().Focused(); f != nil; f = f.Parent() {
		if f == n {
			return true
		}
	}
	return false
}

func (b Box) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	if b.Style != (cell.Style{}) {
		c.Fill(layout.Rect{W: size.W, H: size.H}, b.Style)
	}
	if !b.Border.present() || size.W < 2 || size.H < 2 {
		return
	}
	style := b.BorderStyle
	if style == (cell.Style{}) {
		style = Theme.Border
		if !b.NoFocusBorder && focusWithin(n) {
			style = Theme.BorderFocus
		}
	}
	right, bottom := size.W-1, size.H-1
	c.Set(0, 0, b.Border.TopLeft, 1, style)
	c.Set(right, 0, b.Border.TopRight, 1, style)
	c.Set(0, bottom, b.Border.BottomLeft, 1, style)
	c.Set(right, bottom, b.Border.BottomRight, 1, style)
	for x := 1; x < right; x++ {
		c.Set(x, 0, b.Border.Top, 1, style)
		c.Set(x, bottom, b.Border.Bottom, 1, style)
	}
	for y := 1; y < bottom; y++ {
		c.Set(0, y, b.Border.Left, 1, style)
		c.Set(right, y, b.Border.Right, 1, style)
	}
	if b.Title != "" && size.W > 4 {
		title := merge(style, Theme.Title)
		c.Set(1, 0, " ", 1, style)
		w := c.TextLimit(2, 0, size.W-4, b.Title, title)
		c.Set(2+w, 0, " ", 1, style)
	}
}

// Stack draws its children on top of each other, later ones above. Each
// child gets the stack's full size as a loose constraint and sits at the
// top-left, unless it is wrapped in At or Center.
type Stack struct{}

// Layers returns a stack of elements.
func Layers(children ...view.Element) view.Element { return view.E(Stack{}, children...) }

func (Stack) Layout(n *view.Node, c layout.Constraints) layout.Size {
	size := c.Max()
	for i := 0; i < n.Len(); i++ {
		child := n.LayoutChild(i, layout.Loose(size))
		if p, ok := n.Child(i).Widget().(position); ok {
			n.Place(i, p.place(size, child))
		}
	}
	return size
}
func (Stack) Draw(*view.Node, *view.Canvas) {}

// position places a stack child.
type position struct {
	at       layout.Point
	center   bool
	fill     bool
	alignX   layout.Align
	alignY   layout.Align
	explicit bool
}

func (p position) place(space, child layout.Size) layout.Point {
	if p.center {
		return layout.Point{X: layout.Center.Offset(space.W, child.W), Y: layout.Center.Offset(space.H, child.H)}
	}
	if p.explicit {
		return layout.Point{X: p.alignX.Offset(space.W, child.W), Y: p.alignY.Offset(space.H, child.H)}
	}
	return p.at
}

func (p position) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if n.Len() == 0 {
		return layout.Size{}
	}
	if p.fill {
		return n.LayoutChild(0, layout.Tight(c.Max()))
	}
	inner := c
	if !p.center && !p.explicit {
		inner = layout.Loose(layout.Size{W: max(c.MaxW-p.at.X, 0), H: max(c.MaxH-p.at.Y, 0)})
	}
	return n.LayoutChild(0, inner)
}
func (position) Draw(*view.Node, *view.Canvas) {}

// At places a stack child at x, y.
func At(x, y int, e view.Element) view.Element {
	return view.E(position{at: layout.Point{X: x, Y: y}}, e).WithKey(e.Key)
}

// Center centers a stack child.
func Center(e view.Element) view.Element {
	return view.E(position{center: true}, e).WithKey(e.Key)
}

// Anchor aligns a stack child horizontally and vertically, for example
// End, End for the bottom-right corner.
func Anchor(x, y layout.Align, e view.Element) view.Element {
	return view.E(position{alignX: x, alignY: y, explicit: true}, e).WithKey(e.Key)
}

// Fill makes a stack child cover the whole stack.
func Fill(e view.Element) view.Element {
	return view.E(position{fill: true}, e).WithKey(e.Key)
}

// Size limits its child to at most W by H cells (0 means no limit) and
// at least MinW by MinH.
type Size struct{ W, H, MinW, MinH int }

func (s Size) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if s.W > 0 {
		c.MaxW = max(min(c.MaxW, s.W), c.MinW)
	}
	if s.H > 0 {
		c.MaxH = max(min(c.MaxH, s.H), c.MinH)
	}
	c.MinW = min(max(c.MinW, s.MinW), c.MaxW)
	c.MinH = min(max(c.MinH, s.MinH), c.MaxH)
	if n.Len() == 0 {
		return layout.Size{W: c.MinW, H: c.MinH}
	}
	return c.Constrain(n.LayoutChild(0, c))
}
func (Size) Draw(*view.Node, *view.Canvas) {}

// Limit returns e limited to w by h cells.
func Limit(w, h int, e view.Element) view.Element {
	return view.E(Size{W: w, H: h}, e).WithKey(e.Key)
}

// Modal covers the screen, dims what is under it, keeps focus inside, and
// shows its child centered. Escape or a click on the backdrop calls
// OnClose. Put it last in a Stack.
type Modal struct {
	OnClose func()
}

func (Modal) Layout(n *view.Node, c layout.Constraints) layout.Size {
	size := c.Max()
	if n.Len() > 0 {
		child := n.LayoutChild(0, layout.Loose(size))
		n.Place(0, layout.Point{X: layout.Center.Offset(size.W, child.W), Y: layout.Center.Offset(size.H, child.H)})
	}
	return size
}

func (Modal) Draw(_ *view.Node, c *view.Canvas) {
	c.Restyle(layout.Rect{W: c.Size().W, H: c.Size().H}, func(s cell.Style) cell.Style {
		return merge(s, Theme.Backdrop)
	})
}

func (Modal) TrapFocus() bool { return true }

func (m Modal) Handle(n *view.Node, e view.Event) bool {
	switch e := e.(type) {
	case view.KeyEvent:
		switch {
		case e.Code == input.KeyEscape && m.OnClose != nil:
			m.OnClose()
		case e.Code == input.KeyTab:
			return false // the tree moves focus within the modal
		}
		return true // keys never reach what is under the modal
	case *view.PointerEvent:
		if e.Kind == view.Press && m.OnClose != nil && n.Len() > 0 && !n.Child(0).Rect().Contains(e.Pos) {
			m.OnClose()
		}
		return true
	}
	return false
}
