// Package devtrace records and prints nativeui trace events for development.
// Nothing here is enabled by default: an app opts in by passing an Observer
// in nativeui.RuntimeOptions. Events are already redacted by nativeui; this
// package only stores and formats them.
package devtrace

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const (
	// DefaultSize is the ring size used when NewRecorder gets size <= 0.
	DefaultSize = 256
	// MaxSize bounds a Recorder and a Logger queue.
	MaxSize = 4096
)

// Recorder keeps the most recent events in a bounded ring. Observe copies
// each event and never blocks on a consumer.
type Recorder struct {
	mu      sync.Mutex
	ring    []nativeui.TraceEvent
	next    int
	full    bool
	dropped uint64
}

// NewRecorder returns a ring of size events, clamped to [1, MaxSize]; size
// <= 0 selects DefaultSize.
func NewRecorder(size int) *Recorder {
	if size <= 0 {
		size = DefaultSize
	}
	if size > MaxSize {
		size = MaxSize
	}
	return &Recorder{ring: make([]nativeui.TraceEvent, size)}
}

// Observe stores a deep copy of event, evicting the oldest when full.
func (recorder *Recorder) Observe(event nativeui.TraceEvent) {
	copied := event.Clone()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.full {
		recorder.dropped++
	}
	recorder.ring[recorder.next] = copied
	recorder.next++
	if recorder.next == len(recorder.ring) {
		recorder.next, recorder.full = 0, true
	}
}

// Events returns deep copies of the retained events, oldest first.
func (recorder *Recorder) Events() []nativeui.TraceEvent {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	count := recorder.next
	start := 0
	if recorder.full {
		count, start = len(recorder.ring), recorder.next
	}
	events := make([]nativeui.TraceEvent, 0, count)
	for index := 0; index < count; index++ {
		events = append(events, recorder.ring[(start+index)%len(recorder.ring)].Clone())
	}
	return events
}

// Evicted reports how many events the ring overwrote.
func (recorder *Recorder) Evicted() uint64 {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.dropped
}

// WriteJSONL writes the retained events as one JSON object per line.
func (recorder *Recorder) WriteJSONL(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	for _, event := range recorder.Events() {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

// Logger writes one Format line per event from its own goroutine through a
// bounded queue. A full queue drops the event and counts it; the command
// goroutine never waits for the writer.
type Logger struct {
	queue   chan nativeui.TraceEvent
	writer  io.Writer
	dropped atomic.Uint64
	failed  atomic.Uint64
	mu      sync.RWMutex
	closed  bool
	done    chan struct{}
}

// NewLogger starts a writer goroutine with a queue of size events, clamped
// like NewRecorder. Call Close to flush and stop it.
func NewLogger(writer io.Writer, size int) *Logger {
	if size <= 0 {
		size = DefaultSize
	}
	if size > MaxSize {
		size = MaxSize
	}
	logger := &Logger{queue: make(chan nativeui.TraceEvent, size), writer: writer, done: make(chan struct{})}
	go logger.drain()
	return logger
}

// LogObserver is NewLogger(writer, DefaultSize).Observe, for process-long logs.
func LogObserver(writer io.Writer) nativeui.Observer {
	return NewLogger(writer, DefaultSize).Observe
}

func (logger *Logger) drain() {
	defer close(logger.done)
	for event := range logger.queue {
		func() {
			defer func() {
				if recover() != nil {
					logger.failed.Add(1)
				}
			}()
			if _, err := io.WriteString(logger.writer, Format(event)+"\n"); err != nil {
				logger.failed.Add(1)
			}
		}()
	}
}

// Observe enqueues a copy of event, or drops it when the queue is full or
// the logger is closed.
func (logger *Logger) Observe(event nativeui.TraceEvent) {
	logger.mu.RLock()
	defer logger.mu.RUnlock()
	if logger.closed {
		logger.dropped.Add(1)
		return
	}
	select {
	case logger.queue <- event.Clone():
	default:
		logger.dropped.Add(1)
	}
}

// Dropped counts events lost to a full queue or a closed logger.
func (logger *Logger) Dropped() uint64 { return logger.dropped.Load() }

// WriteFailures counts lines the writer rejected.
func (logger *Logger) WriteFailures() uint64 { return logger.failed.Load() }

// Close stops accepting events, writes the queued ones and waits for the
// writer goroutine. It is safe to call more than once.
func (logger *Logger) Close() {
	logger.mu.Lock()
	if !logger.closed {
		logger.closed = true
		close(logger.queue)
	}
	logger.mu.Unlock()
	<-logger.done
}

// Format renders one stable, greppable line:
//
//	gofastr-trace seq=41 kind=action renderer=android route=/gallery/unknown action=save corr=3fa1c2d40b9e outcome=unknown cause=*errors.errorString dur=3ms editor=dirty,pending
//
// Keys appear in a fixed order and empty fields are omitted. A value with a
// space, quote, equals sign or control character is Go-quoted.
func Format(event nativeui.TraceEvent) string {
	var line strings.Builder
	line.WriteString("gofastr-trace seq=")
	line.WriteString(strconv.FormatUint(event.Seq, 10))
	add := func(key, value string) {
		if value == "" {
			return
		}
		line.WriteByte(' ')
		line.WriteString(key)
		line.WriteByte('=')
		line.WriteString(quote(value))
	}
	add("kind", event.Kind)
	add("renderer", event.Renderer)
	add("route", event.Route)
	add("action", event.Action)
	add("corr", event.Correlation)
	add("inst", event.Instance)
	add("outcome", event.Outcome)
	add("fields", strings.Join(event.FieldKeys, ","))
	if event.UndeclaredFields != 0 {
		add("undeclared-fields", strconv.Itoa(event.UndeclaredFields))
	}
	add("capability", string(event.Capability))
	if event.Definition != nil {
		add("definition", event.Definition.Route+"#"+event.Definition.Part)
	}
	add("cause", event.Cause)
	add("dur", event.Duration.Round(time.Microsecond).String())
	if editor := event.Editor; editor != nil {
		var flags []string
		for _, flag := range []struct {
			name string
			on   bool
		}{{"dirty", editor.Dirty}, {"pending", editor.Pending}, {"unknown", editor.Unknown}, {"restored", editor.Restored},
			{"draft-stored", editor.DraftStored}, {"can-leave", editor.CanLeave}, {"can-navigate", editor.CanNavigate}} {
			if flag.on {
				flags = append(flags, flag.name)
			}
		}
		if len(flags) == 0 {
			flags = []string{"clean"}
		}
		add("editor", strings.Join(flags, ","))
	}
	add("foreground", event.Foreground)
	if event.Receipt != nil {
		add("receipt", string(event.Receipt.Capability)+":"+string(event.Receipt.Status))
	}
	add("effects", strings.Join(event.Effects, ","))
	if event.Changed {
		add("changed", "true")
	}
	return line.String()
}

func quote(value string) string {
	if strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r == '"' || r == '=' || r == 0x7f }) {
		return strconv.Quote(value)
	}
	return value
}
