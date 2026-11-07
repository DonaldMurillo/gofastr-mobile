package main

import (
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func TestParseLsof(t *testing.T) {
	out := strings.Join([]string{
		"p123", "ccupsd",
		"f5", "tIPv4", "PTCP", "n127.0.0.1:631", "TST=LISTEN", "TQR=0", "TQS=0",
		"f6", "tIPv6", "PTCP", "n[::1]:5000->[::1]:6000", "TST=ESTABLISHED",
		"f7", "tIPv4", "PTCP", "n127.0.0.1:631", "TST=LISTEN", // same socket, another descriptor
		"p456", "cmDNSResponder",
		"f8", "tIPv4", "PUDP", "n*:5353",
		"f9", "tIPv4", "PTCP", "n10.0.0.2:50000->140.82.112.4:443", "TST=ESTABLISHED",
	}, "\n") + "\n"
	got, err := parseLsof(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	want := []Conn{
		{Proto: "tcp", Local: "127.0.0.1:631", Remote: "*:*", State: "LISTEN", PID: 123, Process: "cupsd"},
		{Proto: "tcp6", Local: "[::1]:5000", Remote: "[::1]:6000", State: "ESTABLISHED", PID: 123, Process: "cupsd"},
		{Proto: "udp", Local: "*:5353", Remote: "*:*", PID: 456, Process: "mDNSResponder"},
		{Proto: "tcp", Local: "10.0.0.2:50000", Remote: "140.82.112.4:443", State: "ESTABLISHED", PID: 456, Process: "mDNSResponder"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseProcNet(t *testing.T) {
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		t.Skip("fixtures are in little-endian kernel format")
	}
	tcp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:0277 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0 100 0 0 10 0\n" +
		"   1: 0200000A:C350 0470528C:01BB 01 00000000:00000000 02:000A7B2C 00000000  1000        0 67890 2 0 20 4 30 10 -1\n"
	got, err := parseProcNet(strings.NewReader(tcp), "tcp")
	if err != nil {
		t.Fatal(err)
	}
	want := []procSocket{
		{Conn{Proto: "tcp", Local: "127.0.0.1:631", Remote: "*:*", State: "LISTEN"}, 12345},
		{Conn{Proto: "tcp", Local: "10.0.0.2:50000", Remote: "140.82.112.4:443", State: "ESTABLISHED"}, 67890},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("tcp: got %+v\nwant %+v", got, want)
	}

	tcp6 := "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000000000000000000001000000:0016 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 111 1 0 100 0 0 10 0\n" +
		"   1: 0000000000000000FFFF00000100007F:1F90 0000000000000000FFFF00000100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 222 1 0 20 4 30 10 -1\n"
	got, err = parseProcNet(strings.NewReader(tcp6), "tcp6")
	if err != nil {
		t.Fatal(err)
	}
	want = []procSocket{
		{Conn{Proto: "tcp6", Local: "[::1]:22", Remote: "*:*", State: "LISTEN"}, 111},
		{Conn{Proto: "tcp6", Local: "127.0.0.1:8080", Remote: "127.0.0.1:54321", State: "ESTABLISHED"}, 222},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("tcp6: got %+v\nwant %+v", got, want)
	}

	udp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n" +
		"  10: 00000000:14E9 00000000:0000 07 00000000:00000000 00:00000000 00000000   101        0 333 2 0 0\n"
	got, err = parseProcNet(strings.NewReader(udp), "udp")
	if err != nil {
		t.Fatal(err)
	}
	want = []procSocket{{Conn{Proto: "udp", Local: "*:5353", Remote: "*:*"}, 333}}
	if !slices.Equal(got, want) {
		t.Fatalf("udp: got %+v\nwant %+v", got, want)
	}

	if _, err := parseProcNet(strings.NewReader("header\n 0: zz:0000 00000000:0000 0A 0 0 0 0 0 0 1\n"), "tcp"); err == nil {
		t.Fatal("bad hex parsed")
	}
}

func TestParseNetstat(t *testing.T) {
	out := "\r\nActive Connections\r\n\r\n" +
		"  Proto  Local Address          Foreign Address        State           PID\r\n" +
		"  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1044\r\n" +
		"  TCP    192.168.1.5:50000      140.82.112.4:443       ESTABLISHED     7720\r\n" +
		"  TCP    [::]:445               [::]:0                 LISTENING       4\r\n" +
		"  UDP    0.0.0.0:5353           *:*                                    2216\r\n" +
		"  UDP    [::1]:1900             *:*                                    5000\r\n"
	got, err := parseNetstat(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	want := []Conn{
		{Proto: "tcp", Local: "*:135", Remote: "*:*", State: "LISTEN", PID: 1044},
		{Proto: "tcp", Local: "192.168.1.5:50000", Remote: "140.82.112.4:443", State: "ESTABLISHED", PID: 7720},
		{Proto: "tcp6", Local: "*:445", Remote: "*:*", State: "LISTEN", PID: 4},
		{Proto: "udp", Local: "*:5353", Remote: "*:*", PID: 2216},
		{Proto: "udp6", Local: "[::1]:1900", Remote: "*:*", PID: 5000},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseTasklist(t *testing.T) {
	out := "\"System Idle Process\",\"0\",\"Services\",\"0\",\"8 K\"\r\n" +
		"\"chrome.exe\",\"7720\",\"Console\",\"1\",\"200,000 K\"\r\n" +
		"\"svchost.exe\",\"1044\",\"Services\",\"0\",\"12,000 K\"\r\n"
	got, err := parseTasklist(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got[7720] != "chrome" || got[1044] != "svchost" || got[0] != "System Idle Process" || len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestFakeSourceChurns(t *testing.T) {
	src := fakeSource(200)
	a, _ := src(context.Background())
	b, _ := src(context.Background())
	if len(a) < 150 || len(b) < 150 {
		t.Fatalf("sizes %d %d", len(a), len(b))
	}
	keys := map[string]bool{}
	for _, c := range a {
		keys[c.Key()] = true
	}
	kept := 0
	for _, c := range b {
		if keys[c.Key()] {
			kept++
		}
	}
	if kept == len(b) || kept < len(b)/2 {
		t.Fatalf("%d of %d kept", kept, len(b))
	}
}

// fakeHost stands in for the app loop.
type fakeHost struct {
	tree  *view.Tree
	posts chan func()
	quit  bool
}

func (h *fakeHost) Post(fn func())   { h.posts <- fn }
func (h *fakeHost) Quit()            { h.quit = true }
func (h *fakeHost) Tree() *view.Tree { return h.tree }

// screen renders the model after every event, the way the app loop does.
type screen struct {
	t    *testing.T
	m    *model
	host *fakeHost
	buf  *cell.Buffer
	now  time.Time
}

var (
	cups    = Conn{Proto: "tcp", Local: "127.0.0.1:631", Remote: "*:*", State: "LISTEN", PID: 10, Process: "cupsd"}
	firefox = Conn{Proto: "tcp", Local: "10.0.0.2:50000", Remote: "140.82.112.4:443", State: "ESTABLISHED", PID: 20, Process: "firefox"}
	mdns    = Conn{Proto: "udp", Local: "*:5353", Remote: "*:*", PID: 30, Process: "mdns"}
)

func newScreen(t *testing.T, source Source) *screen {
	t.Helper()
	s := &screen{t: t, host: &fakeHost{tree: view.NewTree(uni.Graphemes), posts: make(chan func(), 16)}, buf: cell.NewBuffer(120, 30), now: time.Unix(1000, 0)}
	s.host.tree.Now = func() time.Time { return s.now }
	if source == nil {
		source = func(context.Context) ([]Conn, error) { return nil, nil }
	}
	s.m = newModel(source, time.Second, func() time.Time { return s.now })
	s.m.host = s.host
	s.frame()
	return s
}

func (s *screen) frame() { s.host.tree.Frame(s.m.view(), s.buf) }

func (s *screen) load(conns ...Conn) {
	s.now = s.now.Add(2 * time.Second)
	s.m.apply(slices.Clone(conns), nil)
	s.frame()
}

func (s *screen) send(events ...input.Event) {
	for _, e := range events {
		s.host.tree.Dispatch(e)
		s.now = s.now.Add(time.Second)
		s.frame()
	}
}

func (s *screen) typed(text string) {
	for _, r := range text {
		s.send(input.Key{Code: input.KeyRune, Rune: r})
	}
}

func (s *screen) key(code input.KeyCode) { s.send(input.Key{Code: code}) }

func (s *screen) text() string {
	var b strings.Builder
	for y := 0; y < s.buf.Height(); y++ {
		for x := 0; x < s.buf.Width(); x++ {
			if c := s.buf.At(x, y); !c.IsContinuation() {
				b.WriteString(c.Text)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// find returns the screen position of the first occurrence of text.
func (s *screen) find(text string) (x, y int) {
	s.t.Helper()
	for y, line := range strings.Split(s.text(), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return len([]rune(line[:i])), y
		}
	}
	s.t.Fatalf("no %q on screen:\n%s", text, s.text())
	return 0, 0
}

func (s *screen) shows(text string) bool { return strings.Contains(s.text(), text) }

func (s *screen) processes() []string {
	var out []string
	for _, c := range s.m.rows {
		out = append(out, c.Process)
	}
	return out
}

func (s *screen) wantProcesses(want ...string) {
	s.t.Helper()
	if got := s.processes(); !slices.Equal(got, want) {
		s.t.Fatalf("rows %v, want %v", got, want)
	}
	// The screen lists them in the same order.
	lines := strings.Split(s.text(), "\n")
	y := 0
	for _, p := range want {
		for y < len(lines) && !strings.Contains(lines[y], " "+p) {
			y++
		}
		if y == len(lines) {
			s.t.Fatalf("%s not drawn in order:\n%s", p, s.text())
		}
		y++
	}
}

func (s *screen) selected() string {
	if s.m.index < 0 {
		return ""
	}
	return s.m.rows[s.m.index].Process
}

func TestShowsConnectionsSortedByProcess(t *testing.T) {
	s := newScreen(t, nil)
	if !s.shows("loading") {
		t.Fatalf("no loading state:\n%s", s.text())
	}
	s.load(mdns, firefox, cups)
	s.wantProcesses("cupsd", "firefox", "mdns")
	for _, want := range []string{"3 sockets · 3 shown", "140.82.112.4:443", "ESTABLISHED", "cupsd (pid 10) · 1 socket", "listen\u00a01", "udp\u00a01"} {
		if !s.shows(want) {
			t.Errorf("screen lacks %q:\n%s", want, s.text())
		}
	}
}

func TestFilterNarrowsAndEscapeClears(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox, mdns)
	s.typed("/")
	if !s.m.focusIn("filter") {
		t.Fatalf("focus not in the filter after /")
	}
	s.typed("FIRE 443")
	s.wantProcesses("firefox")
	if !s.shows("3 sockets · 1 shown") {
		t.Fatalf("header:\n%s", s.text())
	}
	s.key(input.KeyEscape)
	if !s.m.focusIn("table") {
		t.Fatalf("focus not in the table after esc in the filter")
	}
	s.wantProcesses("firefox")
	s.key(input.KeyEscape)
	if s.m.filter != "" {
		t.Fatalf("filter %q after esc in the table", s.m.filter)
	}
	s.wantProcesses("cupsd", "firefox", "mdns")
	if s.selected() != "firefox" {
		t.Fatalf("selection %q, want it kept across the filter", s.selected())
	}
}

func TestFilterEnterReturnsToTable(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox, mdns)
	s.typed("/udp")
	s.key(input.KeyEnter)
	if !s.m.focusIn("table") {
		t.Fatalf("focus not in the table after enter")
	}
	s.wantProcesses("mdns")
}

func TestNumberKeysSortAndReverse(t *testing.T) {
	s := newScreen(t, nil)
	s.load(mdns, firefox, cups)
	s.typed("5") // PID
	s.wantProcesses("cupsd", "firefox", "mdns")
	s.typed("5")
	s.wantProcesses("mdns", "firefox", "cupsd")
	if !s.shows("PID▼") && !s.shows("▼") {
		t.Fatalf("no descending marker:\n%s", s.text())
	}
	s.typed("4") // state: "" < ESTABLISHED < LISTEN
	s.wantProcesses("mdns", "firefox", "cupsd")
	s.typed("4")
	s.wantProcesses("cupsd", "firefox", "mdns")
}

func TestHeaderClickSorts(t *testing.T) {
	s := newScreen(t, nil)
	s.load(mdns, firefox, cups)
	x, y := s.find("Remote")
	s.send(press(x, y), release(x, y))
	if s.m.sortBy != colRemote || s.m.desc {
		t.Fatalf("sort %d desc %v", s.m.sortBy, s.m.desc)
	}
	// *:* sorts before digits; ties fall back to the key, tcp before udp,
	// in both directions.
	s.wantProcesses("cupsd", "mdns", "firefox")
	s.send(press(x, y), release(x, y))
	if !s.m.desc {
		t.Fatal("second click did not reverse")
	}
	s.wantProcesses("firefox", "cupsd", "mdns")
}

func TestSelectionFollowsItsConnection(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox, mdns)
	s.key(input.KeyDown)
	if s.selected() != "firefox" {
		t.Fatalf("selected %q", s.selected())
	}
	early := Conn{Proto: "tcp", Local: "10.0.0.2:40000", Remote: "1.1.1.1:53", State: "ESTABLISHED", PID: 5, Process: "aaa-dns"}
	s.load(early, cups, firefox, mdns)
	if s.selected() != "firefox" || s.m.index != 2 {
		t.Fatalf("selected %q at %d after a row appeared above it", s.selected(), s.m.index)
	}
	if !s.shows("firefox (pid 20) · 1 socket") {
		t.Fatalf("details do not follow:\n%s", s.text())
	}
	// When it goes away, the selection stays at the same position.
	s.load(early, cups, mdns)
	if s.selected() != "mdns" || s.m.index != 2 {
		t.Fatalf("selected %q at %d after it closed", s.selected(), s.m.index)
	}
	s.load(cups)
	if s.selected() != "cupsd" || s.m.index != 0 {
		t.Fatalf("selected %q at %d after the list shrank", s.selected(), s.m.index)
	}
}

func TestClickSelectsAndDoubleClickFiltersByProcess(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox, mdns, Conn{Proto: "tcp", Local: "10.0.0.2:50001", Remote: "1.2.3.4:443", State: "ESTABLISHED", PID: 20, Process: "firefox"})
	x, y := s.find(" mdns")
	s.send(press(x+1, y), release(x+1, y))
	if s.selected() != "mdns" {
		t.Fatalf("click selected %q", s.selected())
	}
	x, y = s.find(" cupsd")
	s.host.tree.Dispatch(press(x+1, y))
	s.host.tree.Dispatch(release(x+1, y))
	s.frame()
	s.host.tree.Dispatch(press(x+1, y))
	s.host.tree.Dispatch(release(x+1, y))
	s.frame()
	if s.m.filter != "cupsd" {
		t.Fatalf("double click set filter %q", s.m.filter)
	}
	s.wantProcesses("cupsd")
	s.key(input.KeyEscape)
	s.key(input.KeyDown)
	s.key(input.KeyEnter)
	if s.m.filter != "firefox" {
		t.Fatalf("enter set filter %q", s.m.filter)
	}
	s.wantProcesses("firefox", "firefox")
	if !s.shows("firefox (pid 20) · 2 sockets") {
		t.Fatalf("socket count:\n%s", s.text())
	}
}

func TestPauseStopsTicks(t *testing.T) {
	var calls atomic.Int32
	s := newScreen(t, func(context.Context) ([]Conn, error) {
		calls.Add(1)
		return []Conn{cups}, nil
	})
	s.typed("p")
	if !s.m.paused || !s.shows("PAUSED") {
		t.Fatalf("not paused:\n%s", s.text())
	}
	s.m.tick()
	select {
	case <-s.host.posts:
		t.Fatal("a paused tick refreshed")
	case <-time.After(20 * time.Millisecond):
	}
	if calls.Load() != 0 {
		t.Fatalf("%d source calls while paused", calls.Load())
	}
	s.typed("r") // refreshing by hand still works
	(<-s.host.posts)()
	s.frame()
	s.wantProcesses("cupsd")
	s.typed("p")
	s.m.tick()
	(<-s.host.posts)()
	if calls.Load() != 2 || s.m.paused {
		t.Fatalf("calls %d paused %v", calls.Load(), s.m.paused)
	}
}

func TestRefreshSkipsWhileBusy(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	s := newScreen(t, func(context.Context) ([]Conn, error) {
		calls.Add(1)
		<-release
		return []Conn{firefox}, nil
	})
	s.m.refresh()
	s.m.refresh()
	close(release)
	(<-s.host.posts)()
	if calls.Load() != 1 {
		t.Fatalf("%d source calls", calls.Load())
	}
	s.m.refresh()
	(<-s.host.posts)()
	if calls.Load() != 2 {
		t.Fatalf("%d source calls after the first finished", calls.Load())
	}
}

func TestErrorKeepsLastRows(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox)
	s.m.apply(nil, errors.New("lsof failed"))
	s.frame()
	if !s.shows("lsof failed") {
		t.Fatalf("no error shown:\n%s", s.text())
	}
	s.wantProcesses("cupsd", "firefox")
	s.load(cups)
	if s.shows("lsof failed") {
		t.Fatal("error stayed after a good refresh")
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox)
	s.typed("?")
	if !s.shows("move the selection") {
		t.Fatalf("no help:\n%s", s.text())
	}
	s.typed("q") // the modal traps keys
	if s.host.quit {
		t.Fatal("q reached the table through the modal")
	}
	s.key(input.KeyEscape)
	if s.m.help || s.shows("move the selection") {
		t.Fatalf("help still open:\n%s", s.text())
	}
	s.typed("?")
	x, y := s.find("Close")
	s.send(press(x+1, y), release(x+1, y))
	if s.m.help {
		t.Fatal("close button did not close")
	}
	if !s.m.focusIn("table") {
		t.Fatalf("focus not in the table after the modal, want the table back")
	}
	s.typed("q")
	if !s.host.quit {
		t.Fatal("q did not quit")
	}
}

func TestNewRowsAreHighlighted(t *testing.T) {
	s := newScreen(t, nil)
	s.load(cups, firefox)
	x, y := s.find(" firefox")
	if s.buf.At(x+1, y).Style == styleNew {
		t.Fatal("first load highlighted as new")
	}
	s.load(cups, firefox, mdns)
	x, y = s.find(" mdns")
	if got := s.buf.At(x+1, y).Style; got != styleNew {
		t.Fatalf("new row style %+v", got)
	}
	x, y = s.find(" firefox")
	if s.buf.At(x+1, y).Style == styleNew {
		t.Fatal("old row highlighted")
	}
	s.load(cups, firefox, mdns)
	x, y = s.find(" mdns")
	if s.buf.At(x+1, y).Style == styleNew {
		t.Fatal("row still new on the next refresh")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	s := newScreen(t, nil)
	for range historyLength + 10 {
		s.m.apply([]Conn{cups}, nil)
	}
	if len(s.m.history) != historyLength {
		t.Fatalf("history %d", len(s.m.history))
	}
}

func TestHugeTableStaysResponsive(t *testing.T) {
	conns, _ := fakeSource(50000)(context.Background())
	s := newScreen(t, nil)
	start := time.Now()
	s.load(conns...)
	s.typed("/browser")
	s.key(input.KeyEnter)
	for range 20 {
		s.key(input.KeyPageDown)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("50000 rows took %v", d)
	}
	if len(s.m.rows) == 0 || len(s.m.rows) == len(conns) {
		t.Fatalf("%d of %d rows after filtering", len(s.m.rows), len(conns))
	}
}

func press(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MousePress}
}

func release(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MouseRelease}
}

func TestDetailsFitAStandardTerminal(t *testing.T) {
	s := newScreen(t, nil)
	s.buf.Resize(80, 24)
	s.load(cups, firefox, mdns)
	for _, want := range []string{"Process", "Protocol", "Local ", "Remote ", "State ", "Seen for", "q quit"} {
		if !s.shows(want) {
			t.Errorf("80x24 lacks %q:\n%s", want, s.text())
		}
	}
}
