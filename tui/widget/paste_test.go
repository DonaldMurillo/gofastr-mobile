package widget

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func TestPasteNormalization(t *testing.T) {
	for _, test := range []struct{ name, raw, single, multi string }{
		{"line endings", "a\r\nb\rc\nd\n", "a b c d", "a\nb\nc\nd\n"},
		{"tabs", "a\tb\t", "a b ", "a\tb\t"},
		{"controls", "a\x00\x01\x08\x0b\x0c\x1b\x7f\u0085b", "ab", "ab"},
		{"unicode", "é👩\u200d💻\t世界\r\n", "é👩\u200d💻 世界", "é👩\u200d💻\t世界\n"},
		{"empty lines", "a\n\nb\n\n", "a  b ", "a\n\nb\n\n"},
		{"newline", "\r\n", "", "\n"},
		{"empty", "", "", ""},
	} {
		for _, multiline := range []bool{false, true} {
			name, expected := "Input/", test.single
			if multiline {
				name, expected = "TextArea/", test.multi
			}
			t.Run(name+test.name, func(t *testing.T) {
				value := ""
				h := newHarness(t, 30, 3, func() view.Element {
					change := func(text string) { value = text }
					if multiline {
						return view.E(TextArea{Value: value, OnChange: change})
					}
					return view.E(Input{Value: value, OnChange: change})
				})
				h.send(input.Paste{Text: test.raw})
				if value != expected {
					t.Fatalf("paste = %q, want %q", value, expected)
				}
			})
		}
	}
}

func TestPasteAtCaret(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		t.Run(map[bool]string{false: "Input", true: "TextArea"}[multiline], func(t *testing.T) {
			value := "aéb"
			h := newHarness(t, 10, 2, func() view.Element {
				change := func(text string) { value = text }
				if multiline {
					return view.E(TextArea{Value: value, OnChange: change})
				}
				return view.E(Input{Value: value, OnChange: change})
			})
			h.send(key(input.KeyRight), input.Paste{Text: "中"})
			if value != "a中éb" {
				t.Fatalf("paste at caret = %q", value)
			}
		})
	}
}

func TestInputPasteReplacesSelection(t *testing.T) {
	for _, backwards := range []bool{false, true} {
		value := "aé中z"
		h := newHarness(t, 20, 1, func() view.Element {
			return view.E(Input{Value: value, MaxLength: 4, OnChange: func(text string) { value = text }})
		})
		if backwards {
			h.send(key(input.KeyEnd), key(input.KeyLeft))
		} else {
			h.send(key(input.KeyRight))
		}
		code := input.KeyRight
		if backwards {
			code = input.KeyLeft
		}
		h.send(input.Key{Code: code, Mods: input.ModShift}, input.Key{Code: code, Mods: input.ModShift}, input.Paste{Text: "猫犬鳥"})
		if value != "a猫犬z" {
			t.Fatalf("selection (backwards=%v) = %q", backwards, value)
		}
	}
}

func TestInputMouseSelectionPaste(t *testing.T) {
	value := "abcdef"
	h := newHarness(t, 20, 1, func() view.Element {
		return view.E(Input{Value: value, OnChange: func(text string) { value = text }})
	})
	h.send(press(1, 0), drag(4, 0), release(4, 0), input.Paste{Text: "X"})
	if value != "aXef" {
		t.Fatalf("mouse selection paste = %q", value)
	}
}

func TestTextMaxLength(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		t.Run(map[bool]string{false: "Input", true: "TextArea"}[multiline], func(t *testing.T) {
			value, limit := "", 3
			h := newHarness(t, 20, 3, func() view.Element {
				change := func(text string) { value = text }
				if multiline {
					return view.E(TextArea{Value: value, MaxLength: limit, OnChange: change})
				}
				return view.E(Input{Value: value, MaxLength: limit, OnChange: change})
			})
			h.send(ch('é'), ch('中'), ch('🐈'), ch('x'), input.Paste{Text: "too much"})
			if value != "é中🐈" {
				t.Fatalf("typing limit = %q", value)
			}
			h.send(key(input.KeyBackspace), input.Paste{Text: "犬鳥"})
			if value != "é中犬" {
				t.Fatalf("paste limit = %q", value)
			}
			h.send(key(input.KeyHome), input.Paste{Text: "x"})
			if value != "é中犬" {
				t.Fatalf("full insertion changed suffix: %q", value)
			}
			if multiline {
				h.send(key(input.KeyEnter))
				if value != "é中犬" {
					t.Fatalf("Enter exceeds limit: %q", value)
				}
			}
			limit = 0
			h.frame()
			h.send(input.Paste{Text: "AB"})
			if value != "ABé中犬" {
				t.Fatalf("unlimited insertion = %q", value)
			}
			value, limit = "already too long", 2
			h.frame()
			h.send(input.Paste{Text: "X"})
			if value != "already too long" {
				t.Fatalf("over-limit controlled value changed: %q", value)
			}
		})
	}
}

func TestTextOnPaste(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		t.Run(map[bool]string{false: "Input", true: "TextArea"}[multiline], func(t *testing.T) {
			value, raw, consume := "before", "", true
			changes := 0
			h := newHarness(t, 20, 3, func() view.Element {
				change := func(text string) { value = text; changes++ }
				paste := func(text string) bool { raw = text; return consume }
				if multiline {
					return view.E(TextArea{Value: value, OnChange: change, OnPaste: paste})
				}
				return view.E(Input{Value: value, OnChange: change, OnPaste: paste})
			})
			text := "image\r\n\x1bpath"
			h.send(input.Paste{Text: text})
			if raw != text || value != "before" || changes != 0 {
				t.Fatalf("interception: raw=%q, value=%q, changes=%d", raw, value, changes)
			}
			consume = false
			h.send(input.Paste{Text: text})
			expected := "image pathbefore"
			if multiline {
				expected = "image\npathbefore"
			}
			if value != expected || changes != 1 {
				t.Fatalf("unconsumed paste = %q, changes=%d", value, changes)
			}
		})
	}
}

type pasteRoot struct{ handle func(string) bool }

func (pasteRoot) Layout(n *view.Node, constraints layout.Constraints) layout.Size {
	if n.Len() > 0 {
		n.LayoutChild(0, constraints)
	}
	return constraints.Max()
}
func (pasteRoot) Draw(*view.Node, *view.Canvas) {}
func (root pasteRoot) Handle(_ *view.Node, event view.Event) bool {
	paste, ok := event.(view.PasteEvent)
	return ok && root.handle(paste.Text)
}

func TestPasteBubblesToRootWithoutFocus(t *testing.T) {
	got := ""
	h := newHarness(t, 10, 1, func() view.Element {
		return view.E(pasteRoot{handle: func(text string) bool { got = text; return true }}, Label("no focus"))
	})
	if h.tree.Focused() != nil {
		t.Fatal("test unexpectedly has focus")
	}
	if !h.tree.Dispatch(input.Paste{Text: "raw\r\ntext"}) || got != "raw\r\ntext" {
		t.Fatalf("root did not consume raw paste: %q", got)
	}
}

func TestReadOnlyPasteBubbles(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		got := ""
		h := newHarness(t, 10, 2, func() view.Element {
			child := view.E(Input{AutoFocused: true})
			if multiline {
				child = view.E(TextArea{AutoFocused: true})
			}
			return view.E(pasteRoot{handle: func(text string) bool { got = text; return false }}, child)
		})
		if h.tree.Focused() == nil {
			t.Fatal("read-only widget is not focused")
		}
		if h.tree.Dispatch(input.Paste{Text: "unhandled"}) || got != "unhandled" {
			t.Fatalf("read-only paste did not bubble: %q", got)
		}
	}
}

func TestTextAreaLargePaste(t *testing.T) {
	value := ""
	h := newHarness(t, 80, 10, func() view.Element {
		return view.E(TextArea{Value: value, OnChange: func(text string) { value = text }})
	})
	paste := strings.Repeat("a", 1<<20)
	start := time.Now()
	h.send(input.Paste{Text: paste})
	elapsed := time.Since(start)
	if value != paste {
		t.Fatal("large paste was truncated or changed")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("1 MiB paste took %s (limit 2s)", elapsed)
	}
	t.Logf("1 MiB paste and frame: %s", elapsed)
}
