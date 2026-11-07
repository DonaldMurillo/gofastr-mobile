package input

import (
	"reflect"
	"strings"
	"testing"
)

func decode(t *testing.T, chunks ...string) []Event {
	t.Helper()
	var d Decoder
	var events []Event
	emit := func(e Event) { events = append(events, e) }
	for _, chunk := range chunks {
		d.Feed([]byte(chunk), emit)
	}
	d.Flush(emit)
	return events
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Event
	}{
		{"ascii", "ab", []Event{Key{Rune: 'a'}, Key{Rune: 'b'}}},
		{"utf8", "é界🙂", []Event{Key{Rune: 'é'}, Key{Rune: '界'}, Key{Rune: '🙂'}}},
		{"enter tab backspace", "\r\t\x7f", []Event{Key{Code: KeyEnter}, Key{Code: KeyTab}, Key{Code: KeyBackspace}}},
		{"ctrl letters", "\x01\x03\x1a", []Event{Key{Rune: 'a', Mods: ModCtrl}, Key{Rune: 'c', Mods: ModCtrl}, Key{Rune: 'z', Mods: ModCtrl}}},
		{"ctrl space", "\x00", []Event{Key{Rune: ' ', Mods: ModCtrl}}},
		{"ctrl backspace", "\x08", []Event{Key{Code: KeyBackspace, Mods: ModCtrl}}},
		{"lone esc", "\x1b", []Event{Key{Code: KeyEscape}}},
		{"alt letter", "\x1bx", []Event{Key{Rune: 'x', Mods: ModAlt}}},
		{"alt ctrl", "\x1b\x01", []Event{Key{Rune: 'a', Mods: ModCtrl | ModAlt}}},
		{"alt esc", "\x1b\x1b", []Event{Key{Code: KeyEscape, Mods: ModAlt}}},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", []Event{Key{Code: KeyUp}, Key{Code: KeyDown}, Key{Code: KeyRight}, Key{Code: KeyLeft}}},
		{"ss3 arrows", "\x1bOA\x1bOH", []Event{Key{Code: KeyUp}, Key{Code: KeyHome}}},
		{"modified arrow", "\x1b[1;5C", []Event{Key{Code: KeyRight, Mods: ModCtrl}}},
		{"alt arrow double escape", "\x1b\x1b[A", []Event{Key{Code: KeyUp, Mods: ModAlt}}},
		{"shift tab", "\x1b[Z", []Event{Key{Code: KeyTab, Mods: ModShift}}},
		{"tilde keys", "\x1b[3~\x1b[5;2~\x1b[24~", []Event{Key{Code: KeyDelete}, Key{Code: KeyPageUp, Mods: ModShift}, Key{Code: KeyF12}}},
		{"f1-f4 ss3", "\x1bOP\x1bOS", []Event{Key{Code: KeyF1}, Key{Code: KeyF4}}},
		{"f3 modified csi", "\x1b[1;2R", []Event{Key{Code: KeyF3, Mods: ModShift}}},
		{"cursor position", "\x1b[12;40R", []Event{CursorPosition{X: 39, Y: 11}}},
		{"kitty ctrl+a", "\x1b[97;5u", []Event{Key{Rune: 'a', Mods: ModCtrl}}},
		{"kitty esc", "\x1b[27u", []Event{Key{Code: KeyEscape}}},
		{"kitty shift enter", "\x1b[13;2u", []Event{Key{Code: KeyEnter, Mods: ModShift}}},
		{"kitty release dropped", "\x1b[97;5:3u", nil},
		{"kitty private range dropped", "\x1b[57399u", nil},
		{"modify other keys", "\x1b[27;5;13~", []Event{Key{Code: KeyEnter, Mods: ModCtrl}}},
		{"sgr press", "\x1b[<0;10;5M", []Event{Mouse{X: 9, Y: 4, Button: ButtonLeft, Action: MousePress}}},
		{"sgr release", "\x1b[<0;10;5m", []Event{Mouse{X: 9, Y: 4, Button: ButtonLeft, Action: MouseRelease}}},
		{"sgr right ctrl", "\x1b[<18;1;1M", []Event{Mouse{Button: ButtonRight, Action: MousePress, Mods: ModCtrl}}},
		{"sgr drag", "\x1b[<32;3;4M", []Event{Mouse{X: 2, Y: 3, Button: ButtonLeft, Action: MouseDrag}}},
		{"sgr move", "\x1b[<35;3;4M", []Event{Mouse{X: 2, Y: 3, Action: MouseMove}}},
		{"sgr wheel", "\x1b[<64;1;1M\x1b[<65;1;1M", []Event{Mouse{Button: ButtonWheelUp}, Mouse{Button: ButtonWheelDown}}},
		{"sgr shift wheel", "\x1b[<68;2;2M", []Event{Mouse{X: 1, Y: 1, Button: ButtonWheelUp, Mods: ModShift}}},
		{"sgr back button", "\x1b[<128;1;1M", []Event{Mouse{Button: ButtonBack}}},
		{"x10 press release", "\x1b[M !!\x1b[M#!!", []Event{Mouse{Button: ButtonLeft}, Mouse{Action: MouseRelease}}},
		{"paste", "\x1b[200~hello\r\nworld\x1b[201~", []Event{Paste{Text: "hello\nworld"}}},
		{"paste keeps escapes literal", "\x1b[200~a\x1b[Ab\x1b[201~x", []Event{Paste{Text: "a\x1b[Ab"}, Key{Rune: 'x'}}},
		{"focus", "\x1b[I\x1b[O", []Event{Focus{Focused: true}, Focus{}}},
		{"da1", "\x1b[?62;22c", []Event{DeviceAttributes{Params: []int{62, 22}}}},
		{"decrpm", "\x1b[?2026;2$y", []Event{ModeReport{Mode: 2026, Setting: 2}}},
		{"kitty flags", "\x1b[?1u", []Event{KeyboardFlags{Flags: 1}}},
		{"osc 11 bel", "\x1b]11;rgb:ffff/8080/0000\x07", []Event{ColorReport{Index: 11, R: 0xffff, G: 0x8080, B: 0}}},
		{"osc 11 st short", "\x1b]11;rgb:ff/00/80\x1b\\", []Event{ColorReport{Index: 11, R: 0xffff, G: 0, B: 0x8080}}},
		{"xtversion", "\x1bP>|kitty(0.40.0)\x1b\\", []Event{TerminalVersion{Name: "kitty(0.40.0)"}}},
		{"xtversion strips controls", "\x1bP>|a\x01b\x1b\\", []Event{TerminalVersion{Name: "ab"}}},
		{"unknown csi dropped", "\x1b[99q" + "a", []Event{Key{Rune: 'a'}}},
		{"apc dropped", "\x1b_Gi=1;OK\x1b\\a", []Event{Key{Rune: 'a'}}},
		{"malformed csi keeps next", "\x1b[12\x01", []Event{Key{Rune: 'a', Mods: ModCtrl}}},
		{"invalid utf8 dropped", "\xffa\xc2\x85", []Event{Key{Rune: 'a'}}},
		{"forced incomplete csi is alt+[", "\x1b[", []Event{Key{Rune: '[', Mods: ModAlt}}},
		{"forced incomplete osc is alt+]", "\x1b]1", []Event{Key{Rune: ']', Mods: ModAlt}, Key{Rune: '1'}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := decode(t, test.in)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("decode(%q)\n got %#v\nwant %#v", test.in, got, test.want)
			}
		})
	}
}

// Delivering a sequence one byte per read must decode exactly like one read:
// terminals and ssh links split writes arbitrarily.
func TestSplitReadsDecodeIdentically(t *testing.T) {
	stream := "a\x1b[1;5C界\x1b[<0;10;5M\x1b[200~p\r\nq\x1b[201~\x1b]11;rgb:ffff/0000/0000\x07\x1b[?62c\x1bP>|x\x1b\\🙂\x1b[97;5u\x1b[<0;10;5m"
	whole := decode(t, stream)
	var chunks []string
	for _, b := range []byte(stream) {
		chunks = append(chunks, string([]byte{b}))
	}
	split := decode(t, chunks...)
	if !reflect.DeepEqual(whole, split) {
		t.Fatalf("split reads changed decoding\nwhole %#v\nsplit %#v", whole, split)
	}
	if len(whole) != 11 {
		t.Fatalf("expected 11 events, got %d: %#v", len(whole), whole)
	}
}

func TestPendingOnlyForAmbiguousInput(t *testing.T) {
	var d Decoder
	emit := func(Event) {}
	d.Feed([]byte("\x1b"), emit)
	if !d.Pending() {
		t.Fatal("lone ESC should be pending")
	}
	d.Feed([]byte("[A"), emit)
	if d.Pending() {
		t.Fatal("completed sequence should not be pending")
	}
	d.Feed([]byte("\x1b[200~partial"), emit)
	if d.Pending() {
		t.Fatal("an open paste must not be resolved by a timeout")
	}
	var got []Event
	d.Flush(func(e Event) { got = append(got, e) })
	if got != nil {
		t.Fatalf("flush during paste emitted %#v", got)
	}
}

func TestPasteEndMarkerSplitAcrossReads(t *testing.T) {
	got := decode(t, "\x1b[200~abc\x1b[2", "01~")
	want := []Event{Paste{Text: "abc"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestPasteTruncatesAtLimitWithoutSplittingRunes(t *testing.T) {
	d := Decoder{MaxPaste: 5}
	var got []Event
	d.Feed([]byte("\x1b[200~ab界cd\x1b[201~"), func(e Event) { got = append(got, e) })
	want := []Event{Paste{Text: "ab界", Truncated: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	d = Decoder{MaxPaste: 4}
	got = nil
	d.Feed([]byte("\x1b[200~ab界\x1b[201~"), func(e Event) { got = append(got, e) })
	want = []Event{Paste{Text: "ab", Truncated: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestOverlongSequenceIsDroppedAndBounded(t *testing.T) {
	var d Decoder
	var got []Event
	emit := func(e Event) { got = append(got, e) }
	d.Feed([]byte("\x1b["+strings.Repeat("1", maxSequence+10)), emit)
	d.Feed([]byte("x"), emit)
	if len(d.buf) != 0 {
		t.Fatalf("decoder kept %d bytes", len(d.buf))
	}
	if want := []Event{Key{Rune: 'x'}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func FuzzDecoder(f *testing.F) {
	for _, seed := range []string{"a", "\x1b", "\x1b[<0;1;1M", "\x1b[200~x\x1b[201~", "\x1b]11;rgb:1/2/3\x07", "\x1bP>|v\x1b\\", "\x1b[1;5A", "\x1b[M !!"} {
		f.Add([]byte(seed), uint8(3))
	}
	f.Fuzz(func(t *testing.T, data []byte, split uint8) {
		d := Decoder{MaxPaste: 1 << 12}
		emit := func(e Event) {
			if m, ok := e.(Mouse); ok && (m.X < 0 || m.Y < 0) {
				t.Fatalf("negative mouse coordinates %#v", m)
			}
			if p, ok := e.(Paste); ok && len(p.Text) > 1<<12 {
				t.Fatalf("paste exceeded limit: %d", len(p.Text))
			}
		}
		step := int(split)%7 + 1
		for i := 0; i < len(data); i += step {
			d.Feed(data[i:min(i+step, len(data))], emit)
			if len(d.buf) > maxSequence+step+16 {
				t.Fatalf("decoder buffer grew to %d", len(d.buf))
			}
		}
		d.Flush(emit)
		if d.Pending() {
			t.Fatalf("flush left %q pending", d.buf)
		}
	})
}
