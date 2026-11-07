// Package view keeps a retained tree of nodes behind a declarative
// description of the screen.
//
// Every frame the program describes the whole screen as an Element tree:
// plain values, cheap to build and thrown away afterwards. The Tree
// reconciles that description with the nodes it kept from the previous
// frame. A node survives when its widget type and key match, and so does
// everything attached to it: the widget's state (scroll offsets, carets),
// focus, hover and pointer capture. Then the tree lays nodes out, draws them
// into a cell.Buffer, and routes input to them using the layout the user
// is looking at.
//
// Keys only need to be unique among siblings. Children without a key are
// matched by position.
package view

import (
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
)

// Widget is the description of one node: its configuration, rebuilt every
// frame. Behavior beyond layout and drawing is opted into through the
// optional interfaces below.
type Widget interface {
	// Layout returns the node's size within c, laying out and placing its
	// children with n.LayoutChild and n.Place. The result is clamped to c.
	Layout(n *Node, c layout.Constraints) layout.Size
	// Draw paints the node in its own coordinates. Children are drawn
	// afterwards, on top, clipped to the node.
	Draw(n *Node, c *Canvas)
}

// Stateful widgets keep state across frames. NewState runs when the node
// is created; n.State returns the value afterwards.
type Stateful interface {
	NewState() any
}

// Handler widgets receive events. Handle returns true when it consumed the
// event; unconsumed events go to the parent.
type Handler interface {
	Handle(n *Node, e Event) bool
}

// Focusable widgets can hold keyboard focus.
type Focusable interface {
	Focusable() bool
}

// AutoFocuser widgets take focus when they appear while nothing else has it.
type AutoFocuser interface {
	AutoFocus() bool
}

// FocusTrap widgets keep Tab traversal inside their subtree while they are
// mounted, as a modal dialog does. The most recently mounted trap wins.
type FocusTrap interface {
	TrapFocus() bool
}

// Hoverer widgets want pointer Enter and Leave events. While any is mounted
// the program should enable mouse motion tracking (see Tree.WantsMotion).
type Hoverer interface {
	Hoverable() bool
}

// Cursorer widgets place the terminal cursor while focused, in node
// coordinates.
type Cursorer interface {
	Cursor(n *Node) (layout.Point, bool)
}

// Overdrawer widgets draw again after their children, for scrollbars and
// borders that must stay on top.
type Overdrawer interface {
	DrawOver(n *Node, c *Canvas)
}

// Builder widgets create their children during Layout with
// Node.SetChildren, for example to build only the rows that are visible.
// The Children of their Element are ignored, and children built in an
// earlier frame are kept until the next SetChildren.
type Builder interface {
	BuildsChildren()
}

// Unmounter widgets are told when their node leaves the tree.
type Unmounter interface {
	Unmount(n *Node)
}

// Element describes one node and its children.
type Element struct {
	Widget   Widget
	Key      string
	Children []Element
}

// E returns an element for w with the given children.
func E(w Widget, children ...Element) Element {
	return Element{Widget: w, Children: children}
}

// WithKey returns e with a key that identifies it among its siblings.
func (e Element) WithKey(key string) Element {
	e.Key = key
	return e
}

// Empty is an element that takes no space and draws nothing.
var Empty = Element{}

// If returns e when cond is true and Empty otherwise.
func If(cond bool, e Element) Element {
	if cond {
		return e
	}
	return Empty
}

type emptyWidget struct{}

func (emptyWidget) Layout(*Node, layout.Constraints) layout.Size { return layout.Size{} }
func (emptyWidget) Draw(*Node, *Canvas)                          {}
