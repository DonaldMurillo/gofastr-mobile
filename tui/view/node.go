package view

import (
	"reflect"

	"github.com/DonaldMurillo/gofastr-native/tui/layout"
)

// Node is the retained counterpart of an Element.
type Node struct {
	tree     *Tree
	parent   *Node
	children []*Node
	widget   Widget
	kind     reflect.Type
	key      string
	state    any
	mounted  bool
	order    uint64 // mount order, for focus traps

	size   layout.Size
	offset layout.Point // relative to the parent's origin
	frame  uint64       // the frame this node was last laid out in
	rect   layout.Rect  // absolute, after resolve
	clip   layout.Rect  // visible part of rect
}

// Widget returns the node's current description.
func (n *Node) Widget() Widget { return n.widget }

// Key returns the node's key, empty for positional children.
func (n *Node) Key() string { return n.key }

// State returns the value the widget's NewState made, or nil.
func (n *Node) State() any { return n.state }

// Tree returns the tree the node belongs to.
func (n *Node) Tree() *Tree { return n.tree }

// Parent returns the parent node, nil at the root.
func (n *Node) Parent() *Node { return n.parent }

// Len returns the number of children.
func (n *Node) Len() int { return len(n.children) }

// Child returns child i.
func (n *Node) Child(i int) *Node { return n.children[i] }

// Size returns the size from the last layout.
func (n *Node) Size() layout.Size { return n.size }

// Rect returns the node's screen rectangle from the last frame.
func (n *Node) Rect() layout.Rect { return n.rect }

// Visible returns the part of Rect not clipped away by ancestors.
func (n *Node) Visible() layout.Rect { return n.clip }

// Focused reports whether the node has keyboard focus.
func (n *Node) Focused() bool { return n.tree != nil && n.tree.focus == n }

// Hovered reports whether the pointer is over the node or a descendant.
func (n *Node) Hovered() bool {
	for h := n.tree.hover; h != nil; h = h.parent {
		if h == n {
			return true
		}
	}
	return false
}

// Captured reports whether the node holds pointer capture.
func (n *Node) Captured() bool { return n.tree != nil && n.tree.captor == n }

// Focus gives the node keyboard focus.
func (n *Node) Focus() { n.tree.SetFocus(n) }

// LayoutChild lays out child i within c and returns its size. Each child
// should be laid out once per frame; children that are not are not drawn.
func (n *Node) LayoutChild(i int, c layout.Constraints) layout.Size {
	return n.children[i].layout(c)
}

// Place positions child i relative to n's top-left corner.
func (n *Node) Place(i int, at layout.Point) { n.children[i].offset = at }

// SetChildren replaces the node's children during Layout, reconciling them
// against the current ones. Builder widgets call it before laying their
// children out.
func (n *Node) SetChildren(children []Element) {
	n.tree.reconcileChildren(n, children)
}

func (n *Node) layout(c layout.Constraints) layout.Size {
	n.frame = n.tree.frame
	n.offset = layout.Point{}
	n.size = c.Constrain(n.widget.Layout(n, c))
	return n.size
}

// reconcile returns the node for e, reusing old when the widget type and key
// match.
func (t *Tree) reconcile(parent, old *Node, e Element) *Node {
	w := e.Widget
	if w == nil {
		w = emptyWidget{}
	}
	kind := reflect.TypeOf(w)
	n := old
	if n == nil || n.kind != kind || n.key != e.Key {
		if n != nil {
			t.unmount(n)
		}
		t.mountCount++
		n = &Node{tree: t, parent: parent, kind: kind, key: e.Key, mounted: true, order: t.mountCount}
		if s, ok := w.(Stateful); ok {
			n.state = s.NewState()
		}
	}
	n.widget = w
	if _, builds := w.(Builder); !builds {
		t.reconcileChildren(n, e.Children)
	}
	return n
}

type childKey struct {
	kind reflect.Type
	key  string
	pos  int // position among unkeyed siblings; -1 when keyed
}

func keyOf(e Element, pos int) childKey {
	w := e.Widget
	if w == nil {
		w = emptyWidget{}
	}
	if e.Key != "" {
		return childKey{reflect.TypeOf(w), e.Key, -1}
	}
	return childKey{reflect.TypeOf(w), "", pos}
}

func (t *Tree) reconcileChildren(n *Node, elements []Element) {
	old := make(map[childKey]*Node, len(n.children))
	unkeyed := 0
	for _, c := range n.children {
		k := childKey{c.kind, c.key, -1}
		if c.key == "" {
			k.pos = unkeyed
			unkeyed++
		}
		old[k] = c
	}
	next := make([]*Node, len(elements))
	unkeyed = 0
	for i, e := range elements {
		k := keyOf(e, unkeyed)
		if e.Key == "" {
			unkeyed++
		}
		prev := old[k]
		delete(old, k) // a duplicate key gets a fresh node
		next[i] = t.reconcile(n, prev, e)
	}
	for _, c := range old {
		t.unmount(c)
	}
	n.children = next
}

func (t *Tree) unmount(n *Node) {
	for _, c := range n.children {
		t.unmount(c)
	}
	n.mounted = false
	if u, ok := n.widget.(Unmounter); ok {
		u.Unmount(n)
	}
	if t.focus == n {
		t.focus = nil
	}
	if t.hover == n {
		t.hover = n.parent
	}
	if t.captor == n {
		t.captor = nil
	}
}
