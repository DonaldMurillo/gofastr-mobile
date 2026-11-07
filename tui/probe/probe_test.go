package probe

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// script is a terminal that answers the query with fixed chunks, one per Read,
// and then stays silent.
type script struct {
	mu      sync.Mutex
	chunks  []string
	written strings.Builder
	reads   int
}

func (s *script) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written.Write(p)
	return len(p), nil
}

func (s *script) Read(p []byte, timeout time.Duration) (int, error) {
	s.mu.Lock()
	s.reads++
	if len(s.chunks) == 0 {
		s.mu.Unlock()
		time.Sleep(timeout)
		return 0, nil
	}
	n := copy(p, s.chunks[0])
	s.chunks = s.chunks[1:]
	s.mu.Unlock()
	return n, nil
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

const modernReply = "\x1b[?2026;2$y" + "\x1b[?2027;2$y" + "\x1b[?0u" +
	"\x1b]10;rgb:dddd/dddd/dddd\x1b\\" + "\x1b]11;rgb:1c1c/1c1c/2020\x1b\\" +
	"\x1bP>|ghostty 1.1.0\x1b\\" + "\x1b[?62;22c"

func TestModernTerminal(t *testing.T) {
	conn := &script{chunks: []string{modernReply}}
	var d input.Decoder
	start := time.Now()
	r, other, err := Run(conn, &d, time.Second, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("waited %v after DA1 arrived", time.Since(start))
	}
	if conn.written.String() != Query {
		t.Fatalf("wrote %q", conn.written.String())
	}
	if len(other) != 0 {
		t.Fatalf("unexpected events %v", other)
	}
	want := Result{
		Replied: true, Name: "ghostty 1.1.0",
		SyncOutput: Available, Graphemes: Available, KittyKeyboard: true,
		Foreground: RGB{0xdd, 0xdd, 0xdd}, HasForeground: true,
		Background: RGB{0x1c, 0x1c, 0x20}, HasBackground: true,
		Colors: cell.ColorTrue, StyledUnderline: true, Hyperlinks: true,
	}
	if r != want {
		t.Fatalf("result\n got  %+v\n want %+v", r, want)
	}
	if r.Method() != uni.Graphemes {
		t.Error("mode 2027 available but Method is not Graphemes")
	}
	if dark, known := r.DarkBackground(); !dark || !known {
		t.Errorf("DarkBackground = %v, %v", dark, known)
	}
	if opts := r.Options(); !opts.SyncOutput || opts.Colors != cell.ColorTrue {
		t.Errorf("options %+v", opts)
	}
}

func TestRepliesSplitAcrossReads(t *testing.T) {
	var chunks []string
	for i := 0; i < len(modernReply); i += 3 {
		chunks = append(chunks, modernReply[i:min(i+3, len(modernReply))])
	}
	conn := &script{chunks: chunks}
	var d input.Decoder
	r, other, err := Run(conn, &d, time.Second, env(nil))
	if err != nil || !r.Replied || r.Name != "ghostty 1.1.0" || r.Graphemes != Available || len(other) != 0 {
		t.Fatalf("Run = %+v, %v, %v", r, other, err)
	}
}

func TestOldTerminalAnswersOnlyDA1(t *testing.T) {
	conn := &script{chunks: []string{"\x1b[?1;2c"}}
	var d input.Decoder
	r, _, err := Run(conn, &d, time.Second, env(map[string]string{"TERM": "xterm-256color"}))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Replied || r.SyncOutput != Unsupported || r.Graphemes != Unsupported || r.KittyKeyboard || r.HasBackground {
		t.Fatalf("result %+v", r)
	}
	if r.Colors != cell.Color256 || r.Hyperlinks || r.StyledUnderline {
		t.Fatalf("capabilities %+v", r)
	}
	if r.Method() != uni.Codepoints {
		t.Error("Method should be Codepoints")
	}
	if _, known := r.DarkBackground(); known {
		t.Error("background should be unknown")
	}
}

func TestSilentTerminalTimesOut(t *testing.T) {
	conn := &script{}
	var d input.Decoder
	start := time.Now()
	r, _, err := Run(conn, &d, 50*time.Millisecond, env(map[string]string{"COLORTERM": "truecolor"}))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if r.Replied {
		t.Fatal("Replied without a DA1 reply")
	}
	if elapsed < 40*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("timeout took %v", elapsed)
	}
	if r.Colors != cell.ColorTrue {
		t.Fatalf("COLORTERM ignored: %+v", r)
	}
}

func TestInputDuringProbeIsKept(t *testing.T) {
	// A key typed before the replies and a mouse click between them.
	conn := &script{chunks: []string{"a\x1b[?2026;1$y\x1b[<0;3;4M", "\x1b[?62c"}}
	var d input.Decoder
	r, other, err := Run(conn, &d, time.Second, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.SyncOutput != Enabled {
		t.Fatalf("SyncOutput = %v", r.SyncOutput)
	}
	want := []input.Event{
		input.Key{Code: input.KeyRune, Rune: 'a'},
		input.Mouse{X: 2, Y: 3, Button: input.ButtonLeft, Action: input.MousePress},
	}
	if len(other) != len(want) {
		t.Fatalf("other = %#v", other)
	}
	for i := range want {
		if other[i] != want[i] {
			t.Fatalf("other[%d] = %#v, want %#v", i, other[i], want[i])
		}
	}
	// The caller's reader carries on with the same decoder.
	var rest []input.Event
	d.Feed([]byte("b\x1b["), func(e input.Event) { rest = append(rest, e) })
	d.Feed([]byte("A"), func(e input.Event) { rest = append(rest, e) })
	if len(rest) != 2 || rest[1] != (input.Key{Code: input.KeyUp}) {
		t.Fatalf("after probe: %#v", rest)
	}
}

func TestAfterSentinelInSameRead(t *testing.T) {
	conn := &script{chunks: []string{"\x1b[?62cxy\x1b"}}
	var d input.Decoder
	_, other, err := Run(conn, &d, time.Second, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 2 || other[0] != (input.Key{Code: input.KeyRune, Rune: 'x'}) {
		t.Fatalf("other = %#v", other)
	}
	if !d.Pending() {
		t.Fatal("the trailing ESC should stay in the decoder")
	}
}

func TestEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want func(Result) bool
	}{
		{"no color wins", map[string]string{"COLORTERM": "truecolor", "NO_COLOR": "1"}, func(r Result) bool { return r.Colors == cell.ColorNone }},
		{"empty NO_COLOR is ignored", map[string]string{"COLORTERM": "truecolor", "NO_COLOR": ""}, func(r Result) bool { return r.Colors == cell.ColorTrue }},
		{"linux console", map[string]string{"TERM": "linux"}, func(r Result) bool { return r.Colors == cell.Color16 }},
		{"direct color terminfo", map[string]string{"TERM": "xterm-direct"}, func(r Result) bool { return r.Colors == cell.ColorTrue }},
		{"old vte", map[string]string{"VTE_VERSION": "4600"}, func(r Result) bool { return r.Colors == cell.ColorTrue && !r.Hyperlinks }},
		{"new vte", map[string]string{"VTE_VERSION": "7600"}, func(r Result) bool { return r.Hyperlinks && r.StyledUnderline }},
		{"windows terminal", map[string]string{"WT_SESSION": "x"}, func(r Result) bool { return r.Colors == cell.ColorTrue && r.Hyperlinks }},
		{"iterm via TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "iTerm.app"}, func(r Result) bool { return r.Colors == cell.ColorTrue && r.Hyperlinks }},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal", "TERM": "xterm-256color"}, func(r Result) bool {
			return r.Colors == cell.Color256 && !r.Hyperlinks
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &script{chunks: []string{"\x1b[?1;2c"}}
			var d input.Decoder
			r, _, err := Run(conn, &d, time.Second, env(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if !tt.want(r) {
				t.Fatalf("result %+v", r)
			}
		})
	}
}

func TestSupportFromSetting(t *testing.T) {
	for setting, want := range map[int]Support{0: Unsupported, 1: Enabled, 2: Available, 3: Enabled, 4: Unsupported, 9: Unsupported} {
		if got := support(setting); got != want {
			t.Errorf("support(%d) = %v, want %v", setting, got, want)
		}
	}
}
