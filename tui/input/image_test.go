package input

import (
	"reflect"
	"testing"
)

func TestImageGraphicsReplySplit(t *testing.T) {
	sequence := "\x1b_Gi=2147483647;OK\x1b\\"
	for split := 1; split < len(sequence); split++ {
		var decoder Decoder
		var events []Event
		emit := func(event Event) { events = append(events, event) }
		decoder.Feed([]byte(sequence[:split]), emit)
		if len(events) != 0 {
			t.Fatalf("premature reply at %d: %v", split, events)
		}
		decoder.Feed([]byte(sequence[split:]+"x"), emit)
		want := []Event{GraphicsReply{ID: 2147483647, Message: "OK"}, Key{Rune: 'x'}}
		if !reflect.DeepEqual(events, want) || decoder.Pending() {
			t.Fatalf("split %d: %v pending %v", split, events, decoder.Pending())
		}
	}
}

func TestImageGraphicsRepliesAndCellSize(t *testing.T) {
	var decoder Decoder
	var events []Event
	for _, character := range []byte("\x1b_Gi=7,p=2;ENOENT:no image\x1b\\\x1b[6;24;12t") {
		decoder.Feed([]byte{character}, func(event Event) { events = append(events, event) })
	}
	want := []Event{GraphicsReply{ID: 7, Message: "ENOENT:no image"}, CellSize{Width: 12, Height: 24}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("%v != %v", events, want)
	}
	for _, sequence := range []string{"\x1b_Gi=0;OK\x1b\\", "\x1b_Gi=no;OK\x1b\\", "\x1b_Gi=4294967296;OK\x1b\\", "\x1b_Gx=1;OK\x1b\\", "\x1b[6;0;12t"} {
		decoder.Feed([]byte(sequence), func(event Event) { t.Errorf("malformed %q emitted %v", sequence, event) })
	}
}
