// Package input decodes the byte stream a terminal sends into typed events:
// keys, mouse, paste, focus, and replies to capability queries.
//
// The decoder is a pure function of its input bytes. It never blocks, never
// panics on malformed input, and bounds every buffer it keeps.
package input

import "fmt"

// Event is one decoded input event. The concrete types are Key, Mouse, Paste,
// Focus, and the reply types (DeviceAttributes, ModeReport, KeyboardFlags,
// ColorReport, TerminalVersion, CursorPosition).
type Event interface{ isEvent() }

// Mods is a set of modifier keys.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

func (m Mods) String() string {
	s := ""
	if m&ModCtrl != 0 {
		s += "ctrl+"
	}
	if m&ModAlt != 0 {
		s += "alt+"
	}
	if m&ModShift != 0 {
		s += "shift+"
	}
	if m&ModSuper != 0 {
		s += "super+"
	}
	return s
}

// KeyCode names a key. KeyRune means the key produced text in Key.Rune.
type KeyCode uint8

const (
	KeyRune KeyCode = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

var keyNames = [...]string{
	KeyRune: "rune", KeyEnter: "enter", KeyTab: "tab", KeyBackspace: "backspace",
	KeyEscape: "esc", KeyUp: "up", KeyDown: "down", KeyLeft: "left", KeyRight: "right",
	KeyHome: "home", KeyEnd: "end", KeyPageUp: "pgup", KeyPageDown: "pgdown",
	KeyInsert: "insert", KeyDelete: "delete", KeyF1: "f1", KeyF2: "f2", KeyF3: "f3",
	KeyF4: "f4", KeyF5: "f5", KeyF6: "f6", KeyF7: "f7", KeyF8: "f8", KeyF9: "f9",
	KeyF10: "f10", KeyF11: "f11", KeyF12: "f12",
}

func (k KeyCode) String() string {
	if int(k) < len(keyNames) {
		return keyNames[k]
	}
	return fmt.Sprintf("key(%d)", k)
}

// Key is a key press. For KeyRune, Rune holds the character; ctrl+letter
// arrives as KeyRune with the lowercase letter and ModCtrl.
type Key struct {
	Code KeyCode
	Rune rune
	Mods Mods
}

func (k Key) String() string {
	if k.Code == KeyRune {
		if k.Rune == ' ' {
			return k.Mods.String() + "space"
		}
		return k.Mods.String() + string(k.Rune)
	}
	return k.Mods.String() + k.Code.String()
}

// Button identifies the mouse button of a Mouse event.
type Button uint8

const (
	ButtonNone Button = iota
	ButtonLeft
	ButtonMiddle
	ButtonRight
	ButtonWheelUp
	ButtonWheelDown
	ButtonWheelLeft
	ButtonWheelRight
	ButtonBack
	ButtonForward
)

var buttonNames = [...]string{"none", "left", "middle", "right", "wheel-up", "wheel-down", "wheel-left", "wheel-right", "back", "forward"}

func (b Button) String() string {
	if int(b) < len(buttonNames) {
		return buttonNames[b]
	}
	return fmt.Sprintf("button(%d)", b)
}

// IsWheel reports whether b is a scroll wheel direction.
func (b Button) IsWheel() bool { return b >= ButtonWheelUp && b <= ButtonWheelRight }

// MouseAction says what happened. Wheel events always use MousePress.
type MouseAction uint8

const (
	MousePress MouseAction = iota
	MouseRelease
	// MouseDrag is motion with Button held.
	MouseDrag
	// MouseMove is motion with no button held (needs any-motion tracking).
	MouseMove
)

var actionNames = [...]string{"press", "release", "drag", "move"}

func (a MouseAction) String() string {
	if int(a) < len(actionNames) {
		return actionNames[a]
	}
	return fmt.Sprintf("action(%d)", a)
}

// Mouse is a mouse event. X and Y are zero-based cell coordinates. On release
// from legacy (non-SGR) encodings the button is unknown and reported as
// ButtonNone.
type Mouse struct {
	X, Y   int
	Button Button
	Action MouseAction
	Mods   Mods
}

// Paste is bracketed-paste text. Truncated reports that the paste exceeded
// the decoder's limit and the excess was dropped.
type Paste struct {
	Text      string
	Truncated bool
}

// Focus reports the terminal window gaining or losing focus.
type Focus struct{ Focused bool }

// DeviceAttributes is the primary device attributes reply (DA1). Every VT
// terminal answers DA1, so it doubles as the end marker of a probe batch.
type DeviceAttributes struct{ Params []int }

// ModeReport is a DECRPM reply to a DECRQM query for a private mode.
// Setting: 0 not recognized, 1 set, 2 reset, 3 permanently set, 4 permanently reset.
type ModeReport struct {
	Mode    int
	Setting int
}

// Supported reports whether the terminal recognizes the mode.
func (r ModeReport) Supported() bool { return r.Setting >= 1 && r.Setting <= 4 }

// KeyboardFlags is the reply to the kitty keyboard protocol query.
type KeyboardFlags struct{ Flags int }

// ColorReport answers an OSC 10 (foreground) or 11 (background) query.
// Components are scaled to 16 bits.
type ColorReport struct {
	Index   int
	R, G, B uint16
}

// TerminalVersion is the XTVERSION reply, for example "kitty(0.40.0)".
type TerminalVersion struct{ Name string }

// CursorPosition is a cursor position report, zero-based. A report with
// row 0 is indistinguishable from a modified F3 and is decoded as F3.
type CursorPosition struct{ X, Y int }

func (Key) isEvent()              {}
func (Mouse) isEvent()            {}
func (Paste) isEvent()            {}
func (Focus) isEvent()            {}
func (DeviceAttributes) isEvent() {}
func (ModeReport) isEvent()       {}
func (KeyboardFlags) isEvent()    {}
func (ColorReport) isEvent()      {}
func (TerminalVersion) isEvent()  {}
func (CursorPosition) isEvent()   {}
