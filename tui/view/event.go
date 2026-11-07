package view

import (
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
)

// Event is what handlers receive: *PointerEvent, KeyEvent, PasteEvent or
// FocusEvent.
type Event interface{ viewEvent() }

// PointerKind says what a pointer event is.
type PointerKind uint8

const (
	Press PointerKind = iota
	Release
	Drag
	Move
	Wheel
	Enter
	Leave
)

var pointerNames = [...]string{"press", "release", "drag", "move", "wheel", "enter", "leave"}

func (k PointerKind) String() string { return pointerNames[k] }

// PointerEvent is a mouse event addressed to a node.
type PointerEvent struct {
	Kind   PointerKind
	Button input.Button
	Mods   input.Mods
	// Pos is the screen cell; Local is the same cell relative to the node
	// receiving the event. During capture Local may be outside the node.
	Pos, Local layout.Point
	// Clicks counts consecutive presses on the same cell: 2 for a double
	// click. It is set on Press and Release.
	Clicks int
}

// KeyEvent is a key press sent to the focused node and its ancestors.
type KeyEvent struct{ input.Key }

// PasteEvent is bracketed paste sent like a key.
type PasteEvent struct{ input.Paste }

// FocusEvent tells a node it gained or lost keyboard focus.
type FocusEvent struct{ Focused bool }

func (*PointerEvent) viewEvent() {}
func (KeyEvent) viewEvent()      {}
func (PasteEvent) viewEvent()    {}
func (FocusEvent) viewEvent()    {}
