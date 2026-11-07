package view

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// log records what test widgets see.
type log struct{ entries []string }

func (l *log) add(format string, args ...any) {
	l.entries = append(l.entries, fmt.Sprintf(format, args...))
}
func (l *log) take() []string {
	e := l.entries
	l.entries = nil
	return e
}

// box is a leaf of fixed size that draws a letter and logs events.
type box struct {
	name      string
	w, h      int
	log       *log
	consume   bool
	focusable bool
	hover     bool
	trap      bool
	auto      bool
	bg        cell.Color
	cursor    *layout.Point
}

type boxState struct{ id int }

var nextID int

func (b box) NewState() any { nextID++; return &boxState{id: nextID} }
func (b box) Layout(n *Node, c layout.Constraints) layout.Size {
	// A box with children stacks them, offering them more room than the box
	// has so that clipping shows.
	y := 0
	for i := 0; i < n.Len(); i++ {
		s := n.LayoutChild(i, layout.Loose(layout.Size{W: 100, H: 100}))
		n.Place(i, layout.Point{Y: y})
		y += s.H
	}
	return layout.Size{W: b.w, H: b.h}
}
func (b box) Draw(n *Node, c *Canvas) {
	if !b.bg.IsDefault() {
		c.Fill(layout.Rect{W: c.Size().W, H: c.Size().H}, cell.Style{Bg: b.bg})
	}
	for y := 0; y < c.Size().H; y++ {
		c.Text(0, y, strings.Repeat(b.name, c.Size().W), cell.Style{})
	}
}
func (b box) Handle(n *Node, e Event) bool {
	if b.log != nil {
		switch e := e.(type) {
		case *PointerEvent:
			b.log.add("%s %s %d,%d", b.name, e.Kind, e.Local.X, e.Local.Y)
		case KeyEvent:
			b.log.add("%s key %s", b.name, e.Key)
		case FocusEvent:
			b.log.add("%s focus %v", b.name, e.Focused)
		}
	}
	_, isFocus := e.(FocusEvent)
	return b.consume && !isFocus
}
func (b box) Focusable() bool { return b.focusable }
func (b box) Hoverable() bool { return b.hover }
func (b box) TrapFocus() bool { return b.trap }
func (b box) AutoFocus() bool { return b.auto }
func (b box) Cursor(*Node) (layout.Point, bool) {
	if b.cursor == nil {
		return layout.Point{}, false
	}
	return *b.cursor, true
}

// col stacks children vertically at full width.
type col struct{}

func (col) Layout(n *Node, c layout.Constraints) layout.Size {
	y := 0
	for i := 0; i < n.Len(); i++ {
		s := n.LayoutChild(i, layout.Constraints{MaxW: c.MaxW, MaxH: max(c.MaxH-y, 0)})
		n.Place(i, layout.Point{X: 0, Y: y})
		y += s.H
	}
	return layout.Size{W: c.MaxW, H: c.MaxH}
}
func (col) Draw(*Node, *Canvas) {}

// stack draws children on top of each other.
type stack struct{}

func (stack) Layout(n *Node, c layout.Constraints) layout.Size {
	for i := 0; i < n.Len(); i++ {
		n.LayoutChild(i, layout.Loose(c.Max()))
	}
	return c.Max()
}
func (stack) Draw(*Node, *Canvas) {}

func frame(t *Tree, w, h int, root Element) *cell.Buffer {
	buf := cell.NewBuffer(w, h)
	t.Frame(root, buf)
	return buf
}

func lines(buf *cell.Buffer) []string {
	var out []string
	for y := 0; y < buf.Height(); y++ {
		var b strings.Builder
		for x := 0; x < buf.Width(); x++ {
			if c := buf.At(x, y); !c.IsContinuation() {
				b.WriteString(c.Text)
			}
		}
		out = append(out, b.String())
	}
	return out
}

func stateID(n *Node) int { return n.State().(*boxState).id }

func TestReconcileKeepsStateByKey(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	view := func(names ...string) Element {
		var kids []Element
		for _, n := range names {
			kids = append(kids, E(box{name: n, w: 1, h: 1}).WithKey(n))
		}
		return E(col{}, kids...)
	}
	frame(tr, 4, 4, view("a", "b", "c"))
	ids := map[string]int{}
	for i := 0; i < 3; i++ {
		n := tr.Root().Child(i)
		ids[n.Key()] = stateID(n)
	}
	buf := frame(tr, 4, 4, view("c", "a", "b"))
	for i, name := range []string{"c", "a", "b"} {
		n := tr.Root().Child(i)
		if n.Key() != name || stateID(n) != ids[name] {
			t.Fatalf("child %d is %q with state %d, want %q with %d", i, n.Key(), stateID(n), name, ids[name])
		}
	}
	if got := lines(buf); got[0] != "c   " || got[1] != "a   " || got[2] != "b   " {
		t.Fatalf("drawn %q", got)
	}
}

func TestReconcilePositionalAndTypeChanges(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	var unmounted []string
	frame(tr, 4, 4, E(col{}, E(box{name: "a", w: 1, h: 1}), E(box{name: "b", w: 1, h: 1})))
	first, second := stateID(tr.Root().Child(0)), stateID(tr.Root().Child(1))

	// Same types in the same positions keep their nodes, even with new props.
	frame(tr, 4, 4, E(col{}, E(box{name: "x", w: 2, h: 1}), E(box{name: "y", w: 1, h: 1})))
	if stateID(tr.Root().Child(0)) != first || stateID(tr.Root().Child(1)) != second {
		t.Fatal("positional children lost their state")
	}
	if tr.Root().Child(0).Widget().(box).name != "x" {
		t.Fatal("props not updated")
	}

	// A different widget type at a position replaces the node.
	frame(tr, 4, 4, E(col{}, E(unmountBox{box{name: "u", w: 1, h: 1}, &unmounted}), E(box{name: "y", w: 1, h: 1})))
	if tr.Root().Len() != 2 || stateID(tr.Root().Child(1)) != second {
		t.Fatal("sibling after a type change lost its state")
	}
	frame(tr, 4, 4, E(col{}))
	if !slices.Equal(unmounted, []string{"u"}) {
		t.Fatalf("unmounted %v", unmounted)
	}
}

type unmountBox struct {
	box
	out *[]string
}

func (u unmountBox) Unmount(*Node) { *u.out = append(*u.out, u.name) }

func TestDuplicateKeysGetSeparateNodes(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	el := E(col{}, E(box{name: "a", w: 1, h: 1}).WithKey("k"), E(box{name: "b", w: 1, h: 1}).WithKey("k"))
	frame(tr, 4, 4, el)
	frame(tr, 4, 4, el)
	if stateID(tr.Root().Child(0)) == stateID(tr.Root().Child(1)) {
		t.Fatal("duplicate keys shared a node")
	}
}

func TestChildrenAreClippedToParent(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	// The outer box is 3x2; its child asks for 6x3 and is clipped.
	buf := frame(tr, 6, 3, E(col{}, E(box{name: ".", w: 3, h: 2}, E(box{name: "#", w: 6, h: 3}))))
	want := []string{"###   ", "###   ", "      "}
	if got := lines(buf); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWideTextAtClipEdge(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	// The child is 4 columns wide but its parent clips it to 3, cutting
	// the wide character in half: it must become a blank.
	buf := frame(tr, 4, 1, E(col{}, E(box{name: " ", w: 3, h: 1}, E(box{name: "ab界", w: 4, h: 1}))))
	if got := lines(buf)[0]; got != "ab  " || buf.At(2, 0).Width != 1 || buf.At(3, 0).Width != 1 {
		t.Fatalf("got %q", got)
	}
}

func TestTextKeepsPanelBackground(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	buf := frame(tr, 3, 1, E(col{}, E(box{name: "a", w: 3, h: 1, bg: cell.Blue})))
	if c := buf.At(1, 0); c.Text != "a" || c.Style.Bg != cell.Blue {
		t.Fatalf("cell %+v", c)
	}
}

func press(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MousePress}
}
func drag(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MouseDrag}
}
func release(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MouseRelease}
}

func TestPointerCaptureFollowsTheDrag(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	frame(tr, 10, 4, E(col{},
		E(box{name: "a", w: 10, h: 2, log: l, consume: true}),
		E(box{name: "b", w: 10, h: 2, log: l, consume: true}),
	))
	tr.Dispatch(press(1, 1))
	tr.Dispatch(drag(4, 3)) // over b
	tr.Dispatch(release(5, 3))
	tr.Dispatch(press(2, 2))
	want := []string{"a press 1,1", "a drag 4,3", "a release 5,3", "b press 2,0"}
	if got := l.take(); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnhandledEventsBubble(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	frame(tr, 10, 4, E(col{}, E(box{name: "p", w: 10, h: 4, log: l, consume: true},
		E(box{name: "c", w: 4, h: 1, log: l}))))
	tr.Dispatch(press(6, 2)) // below the child: the parent only
	tr.Dispatch(release(6, 2))
	tr.Dispatch(press(2, 0)) // on the child, which does not consume
	want := []string{"p press 6,2", "p release 6,2", "c press 2,0", "p press 2,0"}
	if got := l.take(); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTopmostChildIsHit(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	frame(tr, 10, 4, E(stack{},
		E(box{name: "under", w: 10, h: 4, log: l, consume: true}),
		E(box{name: "over", w: 4, h: 2, log: l, consume: true}),
	))
	tr.Dispatch(press(1, 1))
	tr.Dispatch(release(1, 1))
	tr.Dispatch(press(6, 3))
	want := []string{"over press 1,1", "over release 1,1", "under press 6,3"}
	if got := l.take(); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFocusTraversal(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	el := E(col{},
		E(box{name: "a", w: 1, h: 1, focusable: true, log: l}),
		E(box{name: "-", w: 1, h: 1}),
		E(box{name: "b", w: 1, h: 1, focusable: true, log: l}),
		E(box{name: "c", w: 1, h: 1, focusable: true, log: l, consume: true}),
	)
	frame(tr, 4, 4, el)
	tab := input.Key{Code: input.KeyTab}
	backtab := input.Key{Code: input.KeyTab, Mods: input.ModShift}
	for _, k := range []input.Key{tab, tab, tab} {
		tr.Dispatch(k)
	}
	if tr.Focused().Widget().(box).name != "c" {
		t.Fatalf("focus on %v", tr.Focused().Widget())
	}
	tr.Dispatch(tab) // c consumes keys, so Tab does not move on
	if tr.Focused().Widget().(box).name != "c" {
		t.Fatal("a consuming node lost focus to Tab")
	}
	l.take()
	frame(tr, 4, 4, E(col{}, el.Children[:3]...)) // c disappears
	if tr.Focused() != nil {
		t.Fatal("focus kept on an unmounted node")
	}
	tr.Dispatch(backtab)
	if tr.Focused().Widget().(box).name != "b" {
		t.Fatalf("shift+tab from nothing focused %v", tr.Focused().Widget())
	}
	tr.Dispatch(tab)
	if tr.Focused().Widget().(box).name != "a" {
		t.Fatal("tab did not wrap")
	}
	if got := l.take(); !slices.Equal(got, []string{"b focus true", "b key tab", "b focus false", "a focus true"}) {
		t.Fatalf("events %q", got)
	}
}

func TestFocusTrapAndAutoFocus(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	base := E(box{name: "base", w: 4, h: 1, focusable: true}).WithKey("base")
	frame(tr, 6, 4, E(col{}, base))
	tr.Dispatch(input.Key{Code: input.KeyTab})
	dialog := E(box{name: "d", w: 6, h: 3, trap: true},
		E(box{name: "x", w: 1, h: 1, focusable: true, auto: true}),
		E(box{name: "y", w: 1, h: 1, focusable: true}),
	).WithKey("dialog")
	frame(tr, 6, 4, E(col{}, base, dialog))
	// The dialog takes focus from base, on its autofocus control, and Tab
	// stays inside it.
	if name := tr.Focused().Widget().(box).name; name != "x" {
		t.Fatalf("dialog opened with focus on %q", name)
	}
	for range 3 {
		tr.Dispatch(input.Key{Code: input.KeyTab})
		if name := tr.Focused().Widget().(box).name; name != "x" && name != "y" {
			t.Fatalf("Tab left the dialog for %q", name)
		}
	}
	// Closing the dialog gives focus back to base.
	frame(tr, 6, 4, E(col{}, base))
	if tr.Focused() == nil || tr.Focused().Widget().(box).name != "base" {
		t.Fatalf("focus after the dialog closed: %v", tr.Focused())
	}
	// A fresh tree with the dialog autofocuses x.
	tr2 := NewTree(uni.Graphemes)
	frame(tr2, 6, 4, E(col{}, base, dialog))
	if tr2.Focused() == nil || tr2.Focused().Widget().(box).name != "x" {
		t.Fatal("autofocus inside the trap did not happen")
	}
}

func TestKeysWithoutFocusReachTheRoot(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	frame(tr, 4, 2, E(box{name: "r", w: 4, h: 2, log: l}, E(box{name: "c", w: 1, h: 1})))
	tr.Dispatch(input.Key{Code: input.KeyRune, Rune: 'q'})
	if got := l.take(); !slices.Equal(got, []string{"r key q"}) {
		t.Fatalf("got %q", got)
	}
}

func TestClickFocusesAndPlacesCursor(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	at := layout.Point{X: 2, Y: 0}
	el := E(col{}, E(box{name: "-", w: 5, h: 1}), E(box{name: "i", w: 5, h: 1, focusable: true, cursor: &at}))
	buf := cell.NewBuffer(6, 3)
	if c := tr.Frame(el, buf); c.Visible {
		t.Fatal("cursor shown with nothing focused")
	}
	tr.Dispatch(press(3, 1))
	tr.Dispatch(release(3, 1))
	if c := tr.Frame(el, buf); c != (cell.Cursor{X: 2, Y: 1, Visible: true}) {
		t.Fatalf("cursor %+v", c)
	}
}

func TestHoverEnterLeave(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	el := E(col{}, E(box{name: "a", w: 6, h: 1, hover: true, log: l}), E(box{name: "b", w: 6, h: 1, log: l}))
	frame(tr, 6, 2, el)
	if !tr.WantsMotion() {
		t.Fatal("hoverable node mounted but WantsMotion is false")
	}
	move := func(x, y int) { tr.Dispatch(input.Mouse{X: x, Y: y, Action: input.MouseMove}) }
	move(1, 0)
	move(2, 0)
	if !tr.Root().Child(0).Hovered() {
		t.Fatal("Hovered false")
	}
	move(1, 1)
	want := []string{"a enter 1,0", "a move 1,0", "a move 2,0", "a leave 1,1", "b move 1,0"}
	if got := l.take(); !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	frame(tr, 6, 2, E(col{}, el.Children[1]))
	if tr.WantsMotion() {
		t.Fatal("WantsMotion with no hoverable node")
	}
}

func TestDoubleClick(t *testing.T) {
	now := time.Unix(0, 0)
	clicks := []int{}
	tr := NewTree(uni.Graphemes)
	tr.Now = func() time.Time { return now }
	frame(tr, 4, 1, E(col{}, E(clickBox{&clicks})))
	click := func(x int, after time.Duration) {
		now = now.Add(after)
		tr.Dispatch(press(x, 0))
		tr.Dispatch(release(x, 0))
	}
	click(1, 0)
	click(1, 100*time.Millisecond)
	click(1, 100*time.Millisecond)
	click(1, time.Second) // too late
	click(2, 10*time.Millisecond)
	if !slices.Equal(clicks, []int{1, 2, 3, 1, 1}) {
		t.Fatalf("clicks %v", clicks)
	}
}

type clickBox struct{ out *[]int }

func (clickBox) Layout(*Node, layout.Constraints) layout.Size { return layout.Size{W: 4, H: 1} }
func (clickBox) Draw(*Node, *Canvas)                          {}
func (c clickBox) Handle(n *Node, e Event) bool {
	if p, ok := e.(*PointerEvent); ok && p.Kind == Press {
		*c.out = append(*c.out, p.Clicks)
	}
	return true
}

func TestSelectionCopiesScreenText(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	var copied []string
	tr.OnCopy = func(s string) { copied = append(copied, s) }
	el := E(col{}, E(textBox{"hello world"}), E(textBox{"second 界 line"}))
	buf := frame(tr, 16, 2, el)
	tr.Dispatch(press(6, 0))
	tr.Dispatch(drag(8, 1))
	buf = frame(tr, 16, 2, el)
	if buf.At(6, 0).Style.Attrs&cell.Reverse == 0 || buf.At(5, 0).Style.Attrs&cell.Reverse != 0 {
		t.Fatal("selection not highlighted")
	}
	tr.Dispatch(release(8, 1))
	if !slices.Equal(copied, []string{"world\nsecond 界"}) {
		t.Fatalf("copied %q", copied)
	}
	tr.Dispatch(input.Key{Code: input.KeyEscape})
	buf = frame(tr, 16, 2, el)
	if buf.At(6, 0).Style.Attrs&cell.Reverse != 0 {
		t.Fatal("Escape did not clear the selection")
	}
}

type textBox struct{ text string }

func (textBox) Layout(_ *Node, c layout.Constraints) layout.Size { return layout.Size{W: c.MaxW, H: 1} }
func (b textBox) Draw(_ *Node, c *Canvas)                        { c.Text(0, 0, b.text, cell.Style{}) }

// lazy builds only as many children as fit, during layout.
type lazy struct{ count int }

func (l lazy) Layout(n *Node, c layout.Constraints) layout.Size {
	var kids []Element
	for i := 0; i < min(l.count, c.MaxH); i++ {
		kids = append(kids, E(box{name: fmt.Sprint(i), w: 1, h: 1}).WithKey(fmt.Sprint(i)))
	}
	n.SetChildren(kids)
	for i := range kids {
		n.LayoutChild(i, layout.Loose(c.Max()))
		n.Place(i, layout.Point{Y: i})
	}
	return c.Max()
}
func (lazy) Draw(*Node, *Canvas) {}
func (lazy) BuildsChildren()     {}

func TestChildrenBuiltDuringLayout(t *testing.T) {
	tr := NewTree(uni.Graphemes)
	buf := frame(tr, 2, 3, E(lazy{count: 100}))
	if got := lines(buf); !slices.Equal(got, []string{"0 ", "1 ", "2 "}) {
		t.Fatalf("got %q", got)
	}
	if tr.Root().Len() != 3 {
		t.Fatalf("%d children built", tr.Root().Len())
	}
	keep := stateID(tr.Root().Child(1))
	frame(tr, 2, 5, E(lazy{count: 100}))
	if tr.Root().Len() != 5 || stateID(tr.Root().Child(1)) != keep {
		t.Fatal("lazy children not retained across frames")
	}
}

func TestWheelBubblesToHandler(t *testing.T) {
	l := &log{}
	tr := NewTree(uni.Graphemes)
	frame(tr, 6, 3, E(col{}, E(box{name: "s", w: 6, h: 3, log: l, consume: true}, E(box{name: "c", w: 6, h: 1}))))
	tr.Dispatch(input.Mouse{X: 1, Y: 0, Button: input.ButtonWheelDown, Action: input.MousePress})
	if got := l.take(); !slices.Equal(got, []string{"s wheel 1,0"}) {
		t.Fatalf("got %q", got)
	}
}
