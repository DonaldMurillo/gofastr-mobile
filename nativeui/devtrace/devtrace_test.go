package devtrace

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestRecorderEvictsOldestAndCopies(t *testing.T) {
	recorder := NewRecorder(3)
	for seq := uint64(1); seq <= 5; seq++ {
		recorder.Observe(nativeui.TraceEvent{Seq: seq, FieldKeys: []string{"a"}, Editor: &nativeui.TraceEditor{Dirty: true}})
	}
	events := recorder.Events()
	if len(events) != 3 || events[0].Seq != 3 || events[2].Seq != 5 || recorder.Evicted() != 2 {
		t.Fatalf("ring: %+v evicted=%d", events, recorder.Evicted())
	}
	events[0].FieldKeys[0], events[0].Editor.Dirty = "changed", false
	again := recorder.Events()
	if again[0].FieldKeys[0] != "a" || !again[0].Editor.Dirty {
		t.Fatal("Events shares storage with the ring")
	}
	original := nativeui.TraceEvent{Seq: 9, Effects: []string{"share"}}
	recorder.Observe(original)
	original.Effects[0] = "mutated"
	if recorder.Events()[2].Effects[0] != "share" {
		t.Fatal("Observe kept the caller's slice")
	}
	if len(NewRecorder(0).ring) != DefaultSize || len(NewRecorder(MaxSize+1).ring) != MaxSize {
		t.Fatal("size clamp")
	}
	var lines bytes.Buffer
	if err := recorder.WriteJSONL(&lines); err != nil || strings.Count(lines.String(), "\n") != 3 {
		t.Fatalf("jsonl: %v %q", err, lines.String())
	}
}

type gate struct {
	mu      sync.Mutex
	open    chan struct{}
	lines   []string
	started chan struct{}
	once    sync.Once
}

func (writer *gate) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.started) })
	<-writer.open
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.lines = append(writer.lines, string(data))
	return len(data), nil
}

func TestLoggerDropsWhenFullAndFlushesOnClose(t *testing.T) {
	writer := &gate{open: make(chan struct{}), started: make(chan struct{})}
	logger := NewLogger(writer, 2)
	logger.Observe(nativeui.TraceEvent{Seq: 1})
	<-writer.started // the drain goroutine holds event 1 in Write
	for seq := uint64(2); seq <= 6; seq++ {
		logger.Observe(nativeui.TraceEvent{Seq: seq})
	}
	if logger.Dropped() != 3 {
		t.Fatalf("dropped = %d, want 3", logger.Dropped())
	}
	close(writer.open)
	logger.Close()
	logger.Close()
	logger.Observe(nativeui.TraceEvent{Seq: 7})
	if len(writer.lines) != 3 || !strings.HasPrefix(writer.lines[2], "gofastr-trace seq=3") || logger.Dropped() != 4 {
		t.Fatalf("lines=%q dropped=%d", writer.lines, logger.Dropped())
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("disk full") }

type panicking struct{}

func (panicking) Write([]byte) (int, error) { panic("writer") }

func TestLoggerSurvivesWriterFailures(t *testing.T) {
	for _, writer := range []interface{ Write([]byte) (int, error) }{failing{}, panicking{}} {
		logger := NewLogger(writer, 4)
		logger.Observe(nativeui.TraceEvent{Seq: 1})
		logger.Observe(nativeui.TraceEvent{Seq: 2})
		logger.Close()
		if logger.WriteFailures() != 2 {
			t.Fatalf("%T failures = %d", writer, logger.WriteFailures())
		}
	}
}

func TestFormatIsStableAndQuotes(t *testing.T) {
	line := Format(nativeui.TraceEvent{Seq: 4, Kind: "action", Renderer: "a b", Route: "/x/{id}", Action: "save", Outcome: "unknown",
		FieldKeys: []string{"title", "body"}, UndeclaredFields: 2, Definition: &nativeui.TraceDefinition{Route: "/x", Part: "form"},
		Editor: &nativeui.TraceEditor{}, Receipt: &nativeui.TraceReceipt{Capability: nativeui.CapabilityHaptics, Status: nativeui.EffectCompleted}, Changed: true})
	want := `gofastr-trace seq=4 kind=action renderer="a b" route=/x/{id} action=save outcome=unknown fields=title,body undeclared-fields=2 definition=/x#form dur=0s editor=clean receipt=haptics/v1:completed changed=true`
	if line != want {
		t.Fatalf("format:\n got %s\nwant %s", line, want)
	}
}
