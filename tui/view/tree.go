package view

import (
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// DoubleClick is the longest gap between presses that still counts as a
// multiple click.
const DoubleClick = 400 * time.Millisecond

// Tree holds the retained nodes and the interaction state attached to them.
// It is not safe for concurrent use; the app loop owns it.
type Tree struct {
	// Method is the width model for text. Set it before the first frame.
	Method uni.Method
	// OnCopy receives text the user selected with the mouse. The app
	// sends it to the clipboard.
	OnCopy func(text string)
	// Now returns the current time; tests replace it.
	Now func() time.Time

	root       *Node
	frame      uint64
	mountCount uint64
	size       layout.Size

	focus, hover, captor *Node
	saved                *Node // focus before a focus trap took it
	buttons              int   // pointer buttons held down

	lastPress      time.Time
	lastPressPos   layout.Point
	lastPressBtn   input.Button
	clicks         int
	selecting      bool
	selection      bool
	selFrom, selTo layout.Point
	screen         *cell.Buffer // the last drawn frame, for copying selections
}

// NewTree returns an empty tree that lays text out with method.
func NewTree(method uni.Method) *Tree {
	return &Tree{Method: method, Now: time.Now}
}

// Root returns the root node, nil before the first frame.
func (t *Tree) Root() *Node { return t.root }

// Focused returns the node with keyboard focus, or nil.
func (t *Tree) Focused() *Node { return t.focus }

// Frame reconciles root with the retained tree, lays it out to fill buf, draws
// it, and returns where the terminal cursor should be.
func (t *Tree) Frame(root Element, buf *cell.Buffer) cell.Cursor {
	t.frame++
	t.root = t.reconcile(nil, t.root, root)
	t.size = layout.Size{W: buf.Width(), H: buf.Height()}
	t.root.layout(layout.Tight(t.size))
	t.resolve(t.root, layout.Point{}, layout.Rect{W: t.size.W, H: t.size.H})
	t.autoFocus()

	buf.Clear()
	t.draw(t.root, buf)
	if t.selection {
		from, to := t.selectionRange()
		for y := from.Y; y <= to.Y; y++ {
			x0, x1 := 0, t.size.W
			if y == from.Y {
				x0 = from.X
			}
			if y == to.Y {
				x1 = to.X + 1
			}
			restyle(buf, layout.Rect{X: x0, Y: y, W: x1 - x0, H: 1}, func(s cell.Style) cell.Style {
				s.Attrs ^= cell.Reverse
				return s
			})
		}
	}
	t.screen = buf
	return t.cursor()
}

// resolve computes absolute rectangles and clips after layout.
func (t *Tree) resolve(n *Node, origin layout.Point, clip layout.Rect) {
	n.rect = layout.Rect{X: origin.X, Y: origin.Y, W: n.size.W, H: n.size.H}
	n.clip = n.rect.Intersect(clip)
	for _, c := range n.children {
		if c.frame != t.frame {
			c.rect, c.clip = layout.Rect{}, layout.Rect{}
			continue
		}
		t.resolve(c, origin.Add(c.offset), n.clip)
	}
}

func (t *Tree) draw(n *Node, buf *cell.Buffer) {
	if n.frame != t.frame || n.clip.Empty() {
		return
	}
	c := &Canvas{buf: buf, origin: n.rect.Origin(), size: n.size, clip: n.clip, method: t.Method}
	n.widget.Draw(n, c)
	for _, child := range n.children {
		t.draw(child, buf)
	}
	if o, ok := n.widget.(Overdrawer); ok {
		o.DrawOver(n, c)
	}
}

func (t *Tree) cursor() cell.Cursor {
	if t.focus == nil {
		return cell.Cursor{}
	}
	cr, ok := t.focus.widget.(Cursorer)
	if !ok {
		return cell.Cursor{}
	}
	p, ok := cr.Cursor(t.focus)
	if !ok {
		return cell.Cursor{}
	}
	abs := t.focus.rect.Origin().Add(p)
	if !t.focus.clip.Contains(abs) {
		return cell.Cursor{}
	}
	return cell.Cursor{X: abs.X, Y: abs.Y, Visible: true}
}

// walk visits laid-out nodes in tree order.
func (t *Tree) walk(n *Node, visit func(*Node)) {
	if n == nil || n.frame != t.frame {
		return
	}
	visit(n)
	for _, c := range n.children {
		t.walk(c, visit)
	}
}

func focusable(n *Node) bool {
	f, ok := n.widget.(Focusable)
	return ok && f.Focusable() && !n.clip.Empty()
}

// autoFocus keeps focus valid after a frame: on a laid-out node inside the
// current focus scope. A focus trap that appears takes focus and gives it
// back when it goes away.
func (t *Tree) autoFocus() {
	scope := t.scope()
	if t.focus != nil && (!t.focus.mounted || t.focus.frame != t.frame) {
		t.focus = nil
	}
	if t.focus != nil && !within(t.focus, scope) {
		t.saved = t.focus
		t.SetFocus(nil)
	}
	if t.focus == nil && scope == t.root && t.saved != nil {
		saved := t.saved
		t.saved = nil
		if saved.mounted && saved.frame == t.frame && focusable(saved) {
			t.SetFocus(saved)
			return
		}
	}
	if t.focus != nil {
		return
	}
	var first, auto *Node
	t.walk(scope, func(n *Node) {
		if !focusable(n) {
			return
		}
		if first == nil {
			first = n
		}
		if a, ok := n.widget.(AutoFocuser); ok && auto == nil && a.AutoFocus() {
			auto = n
		}
	})
	switch {
	case auto != nil:
		t.SetFocus(auto)
	case scope != t.root && first != nil:
		t.SetFocus(first) // a dialog always has its first control focused
	}
}

func within(n, ancestor *Node) bool {
	for ; n != nil; n = n.parent {
		if n == ancestor {
			return true
		}
	}
	return false
}

// scope returns the subtree focus traversal is limited to: the newest focus
// trap, or the root.
func (t *Tree) scope() *Node {
	scope := t.root
	var newest uint64
	t.walk(t.root, func(n *Node) {
		if f, ok := n.widget.(FocusTrap); ok && f.TrapFocus() && n.order >= newest {
			scope, newest = n, n.order
		}
	})
	return scope
}

// SetFocus moves keyboard focus to n, or clears it when n is nil, and
// sends FocusEvents to the nodes that lost and gained it.
func (t *Tree) SetFocus(n *Node) {
	if n == t.focus {
		return
	}
	old := t.focus
	t.focus = n
	if old != nil && old.mounted {
		if h, ok := old.widget.(Handler); ok {
			h.Handle(old, FocusEvent{Focused: false})
		}
	}
	if n != nil {
		if h, ok := n.widget.(Handler); ok {
			h.Handle(n, FocusEvent{Focused: true})
		}
	}
}

// FocusNext moves focus to the next focusable node in tree order, wrapping,
// or the previous one when back is true.
func (t *Tree) FocusNext(back bool) bool {
	var list []*Node
	t.walk(t.scope(), func(n *Node) {
		if focusable(n) {
			list = append(list, n)
		}
	})
	if len(list) == 0 {
		return false
	}
	at := -1
	for i, n := range list {
		if n == t.focus {
			at = i
		}
	}
	switch {
	case at < 0 && back:
		at = len(list) - 1
	case at < 0:
		at = 0
	case back:
		at = (at - 1 + len(list)) % len(list)
	default:
		at = (at + 1) % len(list)
	}
	t.SetFocus(list[at])
	return true
}

// WantsMotion reports whether a mounted node reacts to hover, so the
// program should ask the terminal for every pointer movement.
func (t *Tree) WantsMotion() bool {
	wants := false
	t.walk(t.root, func(n *Node) {
		if h, ok := n.widget.(Hoverer); ok && h.Hoverable() {
			wants = true
		}
	})
	return wants
}

// HitTest returns the deepest node drawn at p, or nil.
func (t *Tree) HitTest(p layout.Point) *Node {
	if t.root == nil {
		return nil
	}
	return t.hit(t.root, p)
}

func (t *Tree) hit(n *Node, p layout.Point) *Node {
	if n.frame != t.frame || !n.clip.Contains(p) {
		return nil
	}
	for i := len(n.children) - 1; i >= 0; i-- { // later children are on top
		if h := t.hit(n.children[i], p); h != nil {
			return h
		}
	}
	return n
}

// Dispatch routes an input event through the tree and reports whether a
// node consumed it. Unconsumed Tab and shift+Tab move focus.
func (t *Tree) Dispatch(e input.Event) bool {
	if t.root == nil {
		return false
	}
	switch e := e.(type) {
	case input.Mouse:
		return t.pointer(e)
	case input.Key:
		start := t.focus
		if start == nil {
			start = t.scope() // with nothing focused, keys go to the dialog or root
		}
		if t.bubble(start, KeyEvent{e}) {
			return true
		}
		if e.Code == input.KeyTab && e.Mods&^input.ModShift == 0 {
			return t.FocusNext(e.Mods&input.ModShift != 0)
		}
		if e.Code == input.KeyEscape && t.selection {
			t.selection = false
			return true
		}
		return false
	case input.Paste:
		start := t.focus
		if start == nil {
			start = t.scope()
		}
		return t.bubble(start, PasteEvent{e})
	}
	return false
}

// bubble offers e to n and its ancestors until one consumes it.
func (t *Tree) bubble(n *Node, e Event) bool {
	for ; n != nil; n = n.parent {
		if !n.mounted {
			continue
		}
		if h, ok := n.widget.(Handler); ok {
			if p, isPointer := e.(*PointerEvent); isPointer {
				p.Local = layout.Point{X: p.Pos.X - n.rect.X, Y: p.Pos.Y - n.rect.Y}
			}
			if h.Handle(n, e) {
				return true
			}
		}
	}
	return false
}

func (t *Tree) send(n *Node, e *PointerEvent) bool {
	h, ok := n.widget.(Handler)
	if !ok || !n.mounted {
		return false
	}
	e.Local = layout.Point{X: e.Pos.X - n.rect.X, Y: e.Pos.Y - n.rect.Y}
	return h.Handle(n, e)
}

func (t *Tree) pointer(m input.Mouse) bool {
	pos := layout.Point{X: m.X, Y: m.Y}
	ev := &PointerEvent{Button: m.Button, Mods: m.Mods, Pos: pos}
	target := t.HitTest(pos)
	changed := t.setHover(target, pos)

	switch {
	case m.Button >= input.ButtonWheelUp && m.Button <= input.ButtonWheelRight:
		if m.Action != input.MousePress {
			return changed
		}
		ev.Kind = Wheel
		return t.bubble(target, ev) || changed

	case m.Action == input.MousePress:
		t.buttons++
		now := t.Now()
		if pos == t.lastPressPos && m.Button == t.lastPressBtn && now.Sub(t.lastPress) <= DoubleClick {
			t.clicks++
		} else {
			t.clicks = 1
		}
		t.lastPress, t.lastPressPos, t.lastPressBtn = now, pos, m.Button
		ev.Kind, ev.Clicks = Press, t.clicks
		hadSelection := t.selection
		t.selection = false
		// Clicking a focusable node focuses it before it sees the press.
		for n := target; n != nil; n = n.parent {
			if focusable(n) {
				t.SetFocus(n)
				break
			}
		}
		for n := target; n != nil; n = n.parent {
			if t.send(n, ev) {
				t.captor = n
				return true
			}
		}
		if m.Button == input.ButtonLeft {
			t.selecting, t.selFrom, t.selTo = true, pos, pos
		}
		return hadSelection || changed

	case m.Action == input.MouseDrag:
		ev.Kind = Drag
		if t.captor != nil {
			return t.send(t.captor, ev) || changed
		}
		if t.selecting {
			t.selTo, t.selection = t.clamp(pos), true
			return true
		}
		return changed

	case m.Action == input.MouseRelease:
		t.buttons = max(t.buttons-1, 0)
		ev.Kind, ev.Clicks = Release, t.clicks
		if captor := t.captor; captor != nil {
			if t.buttons == 0 {
				t.captor = nil
			}
			return t.send(captor, ev) || true
		}
		if t.selecting {
			t.selecting = false
			if t.selection && t.OnCopy != nil {
				if text := t.SelectedText(); text != "" {
					t.OnCopy(text)
				}
			}
			return true
		}
		return t.bubble(target, ev) || changed

	default: // motion with no button
		ev.Kind = Move
		return t.bubble(target, ev) || changed
	}
}

// setHover moves hover to target and sends Leave and Enter to the hoverable
// nodes whose state changed.
func (t *Tree) setHover(target *Node, pos layout.Point) bool {
	if target == t.hover {
		return false
	}
	inside := map[*Node]bool{}
	for n := target; n != nil; n = n.parent {
		inside[n] = true
	}
	changed := false
	for n := t.hover; n != nil; n = n.parent {
		if !inside[n] && hoverable(n) {
			t.send(n, &PointerEvent{Kind: Leave, Pos: pos})
			changed = true
		}
	}
	before := map[*Node]bool{}
	for n := t.hover; n != nil; n = n.parent {
		before[n] = true
	}
	for n := target; n != nil; n = n.parent {
		if !before[n] && hoverable(n) {
			t.send(n, &PointerEvent{Kind: Enter, Pos: pos})
			changed = true
		}
	}
	t.hover = target
	return changed
}

func hoverable(n *Node) bool {
	h, ok := n.widget.(Hoverer)
	return ok && h.Hoverable() && n.mounted
}

func (t *Tree) clamp(p layout.Point) layout.Point {
	return layout.Point{X: min(max(p.X, 0), max(t.size.W-1, 0)), Y: min(max(p.Y, 0), max(t.size.H-1, 0))}
}

func (t *Tree) selectionRange() (from, to layout.Point) {
	from, to = t.selFrom, t.selTo
	if to.Y < from.Y || to.Y == from.Y && to.X < from.X {
		from, to = to, from
	}
	return from, to
}

// SelectedText returns the text of the current mouse selection, read from
// the last frame, with trailing spaces removed from each line.
func (t *Tree) SelectedText() string {
	if !t.selection || t.screen == nil {
		return ""
	}
	from, to := t.selectionRange()
	var lines []string
	for y := from.Y; y <= to.Y; y++ {
		x0, x1 := 0, t.size.W
		if y == from.Y {
			x0 = from.X
		}
		if y == to.Y {
			x1 = to.X + 1
		}
		var b strings.Builder
		for x := x0; x < x1; x++ {
			if c := t.screen.At(x, y); !c.IsContinuation() {
				b.WriteString(c.Text)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// ClearSelection removes the mouse selection highlight.
func (t *Tree) ClearSelection() { t.selection, t.selecting = false, false }
