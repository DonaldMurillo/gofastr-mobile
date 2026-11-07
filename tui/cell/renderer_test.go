package cell_test

import (
	"bytes"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

var graphemes = []struct {
	text  string
	width int
}{
	{"a", 1}, {"b", 1}, {"Z", 1}, {" ", 1}, {" ", 1}, {" ", 1}, {"é", 1}, {"é", 1},
	{"界", 2}, {"中", 2}, {"🙂", 2}, {"-", 1}, {"│", 1},
	{"🇯", 1}, {"🇵", 1}, {"🇯🇵", 2},
}

var styles = []cell.Style{
	{},
	{},
	{Fg: cell.Red},
	{Bg: cell.RGB(10, 20, 200)},
	{Fg: cell.BrightGreen, Bg: cell.Indexed(3)},
	{Attrs: cell.Bold},
	{Attrs: cell.Bold | cell.Dim},
	{Attrs: cell.Dim, Fg: cell.Indexed(200)},
	{Attrs: cell.Italic, Underline: cell.UnderlineCurly, UnderlineColor: cell.RGB(255, 0, 0)},
	{Underline: cell.UnderlineSingle},
	{Attrs: cell.Reverse},
	{Attrs: cell.Strike | cell.Overline},
	{Link: "https://example.com/a", Fg: cell.Blue},
	{Link: "https://example.com/b", Underline: cell.UnderlineSingle},
	{Bg: cell.RGB(30, 30, 30)},
}

func mutate(rng *rand.Rand, b *cell.Buffer) {
	switch rng.IntN(12) {
	case 0:
		b.Clear()
	case 1, 2:
		x, y := rng.IntN(b.Width()), rng.IntN(b.Height())
		b.Fill(x, y, 1+rng.IntN(b.Width()), 1+rng.IntN(b.Height()), styles[rng.IntN(len(styles))])
	}
	for n := rng.IntN(b.Width()*b.Height()/3 + 2); n > 0; n-- {
		g := graphemes[rng.IntN(len(graphemes))]
		b.Set(rng.IntN(b.Width()), rng.IntN(b.Height()), g.text, g.width, styles[rng.IntN(len(styles))])
	}
}

func erasable(s cell.Style) bool {
	return s.Attrs&(cell.Reverse|cell.Strike|cell.Overline) == 0 && s.Underline == cell.UnderlineNone && s.Link == ""
}

// equivalent compares what a user sees. Erase operations paint only the
// background, so an erasable blank matches any blank with the same background.
func equivalent(got, want cell.Cell) bool {
	if got.Text != want.Text || got.Width != want.Width {
		return false
	}
	if want.Text == " " && erasable(want.Style) && erasable(got.Style) {
		return got.Style.Bg == want.Style.Bg
	}
	return got.Style == want.Style
}

func check(t *testing.T, screen *vt.Screen, back *cell.Buffer, cursor cell.Cursor, context string) {
	t.Helper()
	if len(screen.Errors) > 0 {
		t.Fatalf("%s: emulator errors: %q", context, screen.Errors)
	}
	if screen.Scrolls > 0 {
		t.Fatalf("%s: output scrolled the screen %d times", context, screen.Scrolls)
	}
	for y := 0; y < back.Height(); y++ {
		for x := 0; x < back.Width(); x++ {
			if got, want := screen.Cell(x, y), back.At(x, y); !equivalent(got, want) {
				t.Fatalf("%s: cell %d,%d\n got %#v\nwant %#v\nscreen:\n%s", context, x, y, got, want, screen.Text())
			}
		}
	}
	if screen.CursorVisible != cursor.Visible {
		t.Fatalf("%s: cursor visible %v, want %v", context, screen.CursorVisible, cursor.Visible)
	}
	if cursor.Visible && (screen.X != cursor.X || screen.Y != cursor.Y) {
		t.Fatalf("%s: cursor at %d,%d, want %d,%d", context, screen.X, screen.Y, cursor.X, cursor.Y)
	}
}

// TestRendererMatchesEmulator is the renderer's main correctness check: for
// random frame sequences, applying the renderer's output to a terminal
// emulator must reproduce every frame exactly.
func TestRendererMatchesEmulator(t *testing.T) {
	trials := 400
	if testing.Short() {
		trials = 60
	}
	for trial := range trials {
		rng := rand.New(rand.NewPCG(uint64(trial), 0x5eed))
		w, h := 1+rng.IntN(40), 1+rng.IntN(12)
		options := cell.Options{Colors: cell.ColorTrue, SyncOutput: rng.IntN(2) == 0, StyledUnderline: true, Hyperlinks: true}
		renderer := cell.NewRenderer(w, h, options)
		screen := vt.New(w, h)
		screen.Write([]byte("\x1b[?7l\x1b[?2027h")) // grapheme-capable terminal, autowrap off
		back := cell.NewBuffer(w, h)
		for frame := range 30 {
			if rng.IntN(20) == 0 {
				w, h = 1+rng.IntN(40), 1+rng.IntN(12)
				back.Resize(w, h)
				screen.Resize(w, h)
			}
			if rng.IntN(25) == 0 {
				renderer.Invalidate()
			}
			mutate(rng, back)
			cursor := cell.Cursor{X: rng.IntN(w), Y: rng.IntN(h), Visible: rng.IntN(3) == 0}
			screen.Write(renderer.Render(back, cursor))
			check(t, screen, back, cursor, "trial "+strconv.Itoa(trial)+" frame "+strconv.Itoa(frame))
		}
	}
}

func TestRendererKeepsAdjacentRegionalIndicatorsSeparate(t *testing.T) {
	back := cell.NewBuffer(4, 1)
	back.Set(0, 0, "🇯", 1, cell.Style{})
	back.Set(1, 0, "🇵", 1, cell.Style{})
	renderer := cell.NewRenderer(4, 1, cell.Options{})
	screen := vt.New(4, 1)
	screen.Write([]byte("\x1b[?7l\x1b[?2027h"))
	screen.Write(renderer.Render(back, cell.Cursor{}))
	check(t, screen, back, cell.Cursor{}, "separate Set calls")
}

func TestRendererKeepsIndependentGraphemesSeparate(t *testing.T) {
	for _, pair := range [][2]string{{"ᄀ", "ᅡ"}, {"👩‍", "👩"}, {"a", "ा"}, {"🇯", "🇵"}} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			back := cell.NewBuffer(12, 1)
			renderer := cell.NewRenderer(12, 1, cell.Options{})
			screen := vt.New(12, 1)
			screen.Write([]byte("\x1b[?7l\x1b[?2027h"))
			for _, x := range []int{0, 3, 1, 6} {
				back.Clear()
				back.Set(x, 0, pair[0], uni.Graphemes.Width(pair[0]), cell.Style{})
				next := x + uni.Graphemes.Width(pair[0])
				back.Set(next, 0, pair[1], uni.Graphemes.Width(pair[1]), cell.Style{})
				screen.Write(renderer.Render(back, cell.Cursor{}))
				check(t, screen, back, cell.Cursor{}, "independent clusters")
			}
		})
	}
}

func TestStandaloneZeroWidthCellUsesReplacement(t *testing.T) {
	for _, text := range []string{"\u0301", "\u200d", "\ufe0f"} {
		t.Run(text, func(t *testing.T) {
			back := cell.NewBuffer(4, 1)
			back.Set(0, 0, "a", 1, cell.Style{})
			if n := back.Set(1, 0, text, 0, cell.Style{}); n != 0 {
				t.Fatalf("zero-width write used %d cells", n)
			}
			back.Set(1, 0, text, 1, cell.Style{})
			if got := back.At(1, 0).Text; got != cell.Replacement {
				t.Fatalf("standalone zero-width text = %q", got)
			}
			renderer := cell.NewRenderer(4, 1, cell.Options{})
			screen := vt.New(4, 1)
			screen.Write([]byte("\x1b[?7l\x1b[?2027h"))
			screen.Write(renderer.Render(back, cell.Cursor{}))
			check(t, screen, back, cell.Cursor{}, "zero-width cell after base")
		})
	}
}

func TestRendererGraphemeBoundariesAllocateNothing(t *testing.T) {
	back := cell.NewBuffer(6, 1)
	back.Set(0, 0, "🇯", 1, cell.Style{})
	back.Set(1, 0, "🇵", 1, cell.Style{})
	renderer := cell.NewRenderer(6, 1, cell.Options{})
	if allocs := testing.AllocsPerRun(100, func() {
		renderer.Invalidate()
		renderer.Render(back, cell.Cursor{})
	}); allocs != 0 {
		t.Fatalf("renderer allocated %g times", allocs)
	}
}

func TestUnchangedFrameWritesNothing(t *testing.T) {
	back := cell.NewBuffer(80, 24)
	back.Set(3, 3, "x", 1, cell.Style{Fg: cell.Red})
	renderer := cell.NewRenderer(80, 24, cell.Options{Colors: cell.ColorTrue})
	cursor := cell.Cursor{X: 5, Y: 5, Visible: true}
	renderer.Render(back, cursor)
	if out := renderer.Render(back, cursor); len(out) != 0 {
		t.Fatalf("unchanged frame wrote %q", out)
	}
}

func TestInvalidateRestoresCursorVisibility(t *testing.T) {
	for _, visible := range []bool{false, true} {
		back := cell.NewBuffer(4, 1)
		renderer := cell.NewRenderer(4, 1, cell.Options{})
		screen := vt.New(4, 1)
		cursor := cell.Cursor{Visible: visible}
		screen.Write(renderer.Render(back, cursor))
		if visible {
			screen.Write([]byte("\x1b[?25l"))
		} else {
			screen.Write([]byte("\x1b[?25h"))
		}
		renderer.Invalidate()
		screen.Write(renderer.Render(back, cursor))
		check(t, screen, back, cursor, "external cursor change")
	}
}

func TestSmallChangeWritesLittle(t *testing.T) {
	back := cell.NewBuffer(200, 60)
	for y := range 60 {
		for x := range 200 {
			back.Set(x, y, "a", 1, cell.Style{})
		}
	}
	renderer := cell.NewRenderer(200, 60, cell.Options{Colors: cell.ColorTrue})
	renderer.Render(back, cell.Cursor{})
	back.Set(120, 40, "b", 1, cell.Style{Fg: cell.Red})
	out := renderer.Render(back, cell.Cursor{})
	if len(out) > 20 {
		t.Fatalf("one changed cell wrote %d bytes: %q", len(out), out)
	}
}

func TestBlankRunsUseErase(t *testing.T) {
	back := cell.NewBuffer(100, 1)
	for x := range 100 {
		back.Set(x, 0, "a", 1, cell.Style{})
	}
	renderer := cell.NewRenderer(100, 1, cell.Options{})
	renderer.Render(back, cell.Cursor{})
	back.Clear()
	out := renderer.Render(back, cell.Cursor{})
	if !bytes.Contains(out, []byte("\x1b[K")) || len(out) > 16 {
		t.Fatalf("clearing a row wrote %q", out)
	}
}

func TestNoColorKeepsAttributes(t *testing.T) {
	back := cell.NewBuffer(4, 1)
	back.Set(0, 0, "a", 1, cell.Style{Fg: cell.RGB(1, 2, 3), Bg: cell.Red, Attrs: cell.Bold})
	renderer := cell.NewRenderer(4, 1, cell.Options{Colors: cell.ColorNone})
	screen := vt.New(4, 1)
	screen.Write(renderer.Render(back, cell.Cursor{}))
	got := screen.Cell(0, 0).Style
	if got.Fg != cell.Default || got.Bg != cell.Default || got.Attrs != cell.Bold {
		t.Fatalf("NO_COLOR style = %#v", got)
	}
}

func TestDowngrade(t *testing.T) {
	tests := []struct {
		in   cell.Color
		mode cell.ColorMode
		want cell.Color
	}{
		{cell.RGB(255, 0, 0), cell.Color256, cell.Indexed(196)},
		{cell.RGB(128, 128, 128), cell.Color256, cell.Indexed(244)},
		{cell.RGB(250, 0, 0), cell.Color16, cell.BrightRed},
		{cell.Indexed(196), cell.Color16, cell.BrightRed},
		{cell.Indexed(4), cell.Color16, cell.Blue},
		{cell.RGB(1, 2, 3), cell.ColorTrue, cell.RGB(1, 2, 3)},
		{cell.Red, cell.ColorNone, cell.Default},
	}
	for _, test := range tests {
		if got := test.in.Downgrade(test.mode); got != test.want {
			t.Errorf("Downgrade(%#x, %d) = %#x, want %#x", test.in, test.mode, got, test.want)
		}
	}
}

func TestControlTextNeverReachesTheTerminal(t *testing.T) {
	back := cell.NewBuffer(10, 1)
	back.Set(0, 0, "\x1b[2J", 1, cell.Style{})
	back.Set(1, 0, "\x9b", 1, cell.Style{})
	back.Set(2, 0, "a\nb", 1, cell.Style{})
	back.Set(3, 0, "ok", 1, cell.Style{Link: "https://x.example/\x1b]8;;evil\x07"})
	renderer := cell.NewRenderer(10, 1, cell.Options{Hyperlinks: true})
	out := renderer.Render(back, cell.Cursor{})
	// The only clear is the renderer's own repaint; no raw controls appear.
	if bytes.Count(out, []byte("\x1b[2J")) != 1 || bytes.ContainsAny(out, "\n\x07") || bytes.Contains(out, []byte{0x9b}) {
		t.Fatalf("control text reached output: %q", out)
	}
	for x, want := range []string{cell.Replacement, cell.Replacement, cell.Replacement, "ok"} {
		if got := back.At(x, 0).Text; got != want {
			t.Fatalf("cell %d = %q, want %q", x, got, want)
		}
	}
	if !bytes.Contains(out, []byte("\x1b]8;;https://x.example/]8;;evil\x1b\\")) {
		t.Fatalf("link was not sanitized: %q", out)
	}
}

func TestWideCellRepair(t *testing.T) {
	b := cell.NewBuffer(4, 1)
	b.Set(0, 0, "界", 2, cell.Style{})
	b.Set(1, 0, "x", 1, cell.Style{})
	if got := b.At(0, 0); got.Text != " " || got.Width != 1 {
		t.Fatalf("overwriting a right half left %#v", got)
	}
	b.Set(2, 0, "中", 2, cell.Style{})
	b.Set(2, 0, "y", 1, cell.Style{})
	if got := b.At(3, 0); got.Text != " " || got.Width != 1 {
		t.Fatalf("overwriting a left half left %#v", got)
	}
	if n := b.Set(3, 0, "界", 2, cell.Style{}); n != 1 || b.At(3, 0).Text != " " {
		t.Fatalf("wide cell in the last column: n=%d cell=%#v", n, b.At(3, 0))
	}
}

func BenchmarkRenderFullFrame(b *testing.B) {
	back := cell.NewBuffer(250, 80)
	alt := cell.NewBuffer(250, 80)
	for y := range 80 {
		for x := range 250 {
			back.Set(x, y, "a", 1, cell.Style{Fg: cell.RGB(uint8(x), uint8(y), 9)})
			alt.Set(x, y, "b", 1, cell.Style{Bg: cell.RGB(uint8(y), 3, uint8(x))})
		}
	}
	renderer := cell.NewRenderer(250, 80, cell.Options{Colors: cell.ColorTrue, SyncOutput: true})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if i%2 == 0 {
			renderer.Render(back, cell.Cursor{})
		} else {
			renderer.Render(alt, cell.Cursor{})
		}
	}
}

func BenchmarkRenderOneCellChange(b *testing.B) {
	back := cell.NewBuffer(250, 80)
	renderer := cell.NewRenderer(250, 80, cell.Options{Colors: cell.ColorTrue})
	renderer.Render(back, cell.Cursor{})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		back.Set(i%250, 40, string(rune('a'+i%26)), 1, cell.Style{})
		renderer.Render(back, cell.Cursor{})
	}
}

func TestRendererWritesNonASCIIRowsWithoutExtraMoves(t *testing.T) {
	b := cell.NewBuffer(80, 1)
	for x := 0; x < 80; x++ {
		b.Set(x, 0, "─", 1, cell.Style{})
	}
	r := cell.NewRenderer(80, 1, cell.Options{})
	out := string(r.Render(b, cell.Cursor{}))
	if n := bytes.Count([]byte(out), []byte("─")); n != 80 {
		t.Fatalf("%d of 80 cells written", n)
	}
	if n := bytes.Count([]byte(out), []byte("H")); n > 2 {
		t.Fatalf("%d cursor positions for one row of box drawing:\n%q", n, out)
	}
}
