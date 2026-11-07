package widget

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// harness renders a root function into a buffer after every event, the way
// the app loop does.
type harness struct {
	t    *testing.T
	tree *view.Tree
	buf  *cell.Buffer
	root func() view.Element
	now  time.Time
	cur  cell.Cursor
}

func newHarness(t *testing.T, w, h int, root func() view.Element) *harness {
	t.Helper()
	hs := &harness{t: t, tree: view.NewTree(uni.Graphemes), buf: cell.NewBuffer(w, h), root: root, now: time.Unix(0, 0)}
	hs.tree.Now = func() time.Time { return hs.now }
	hs.frame()
	return hs
}

func (h *harness) frame() { h.cur = h.tree.Frame(h.root(), h.buf) }

func (h *harness) send(events ...input.Event) {
	for _, e := range events {
		h.tree.Dispatch(e)
		h.now = h.now.Add(time.Second) // never a double click by accident
		h.frame()
	}
}

func (h *harness) lines() []string {
	var out []string
	for y := 0; y < h.buf.Height(); y++ {
		var b strings.Builder
		for x := 0; x < h.buf.Width(); x++ {
			if c := h.buf.At(x, y); !c.IsContinuation() {
				b.WriteString(c.Text)
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func (h *harness) want(want ...string) {
	h.t.Helper()
	got := h.lines()
	for len(got) > len(want) && got[len(got)-1] == "" {
		got = got[:len(got)-1]
	}
	if !slices.Equal(got, want) {
		h.t.Fatalf("screen:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func key(code input.KeyCode) input.Key { return input.Key{Code: code} }
func ch(r rune) input.Key              { return input.Key{Code: input.KeyRune, Rune: r} }
func ctrl(r rune) input.Key {
	return input.Key{Code: input.KeyRune, Rune: r, Mods: input.ModCtrl}
}
func typed(s string) []input.Event {
	var out []input.Event
	for _, r := range s {
		out = append(out, ch(r))
	}
	return out
}
func press(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MousePress}
}
func drag(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MouseDrag}
}
func release(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonLeft, Action: input.MouseRelease}
}
func click(x, y int) []input.Event { return []input.Event{press(x, y), release(x, y)} }
func wheelDown(x, y int) input.Mouse {
	return input.Mouse{X: x, Y: y, Button: input.ButtonWheelDown, Action: input.MousePress}
}

func TestTextWrapsAndTruncates(t *testing.T) {
	h := newHarness(t, 10, 4, func() view.Element {
		return Column(
			Paragraph("the quick brown fox"),
			view.E(Text{Spans: []Span{{Text: "truncated text"}}, Ellipsis: true}),
		)
	})
	h.want("the quick", "brown fox", "truncated…")
}

func TestTextWrapBreaksLongWords(t *testing.T) {
	h := newHarness(t, 4, 3, func() view.Element { return Paragraph("abcdefghij") })
	h.want("abcd", "efgh", "ij")
}

func TestFlexDistributesTheMainAxis(t *testing.T) {
	h := newHarness(t, 12, 1, func() view.Element {
		return Row(Fixed(3, Label("abcdef")), Grow(1, view.E(Text{Spans: []Span{{Text: "mid"}}, Align: layout.Center})), Label("end"))
	})
	h.want("abc mid  end")
}

func TestFlexGapAndColumn(t *testing.T) {
	h := newHarness(t, 5, 4, func() view.Element {
		return view.E(Flex{Dir: Vertical, Gap: 1}, Label("a"), Label("b"))
	})
	h.want("a", "", "b")
}

func TestPanelDrawsBorderAndTitle(t *testing.T) {
	h := newHarness(t, 12, 3, func() view.Element { return Panel("Conns", Label("hi")) })
	h.want("╭ Conns ───╮", "│ hi       │", "╰──────────╯")
}

func TestButtonClickNeedsReleaseInside(t *testing.T) {
	clicks := 0
	h := newHarness(t, 20, 1, func() view.Element {
		return Row(NewButton("Go", func() { clicks++ }))
	})
	h.want(" Go")
	h.send(click(1, 0)...)
	if clicks != 1 {
		t.Fatalf("click: %d", clicks)
	}
	h.send(press(1, 0), drag(10, 0), release(10, 0))
	h.send(press(1, 0), release(10, 0)) // terminals without drag reports
	if clicks != 1 {
		t.Fatalf("release outside fired: %d", clicks)
	}
	h.send(key(input.KeyEnter), ch(' '))
	if clicks != 3 {
		t.Fatalf("keys: %d", clicks)
	}
}

func TestButtonStylesFollowState(t *testing.T) {
	h := newHarness(t, 10, 1, func() view.Element { return Row(NewButton("Go", nil), NewButton("No", nil)) })
	if got := h.buf.At(0, 0).Style; got != Theme.Button {
		t.Fatalf("idle style %+v", got)
	}
	h.send(input.Mouse{X: 0, Y: 0, Action: input.MouseMove})
	if got := h.buf.At(0, 0).Style; got != Theme.ButtonHover {
		t.Fatalf("hover style %+v", got)
	}
	h.send(press(0, 0))
	if got := h.buf.At(0, 0).Style; got != Theme.ButtonPress {
		t.Fatalf("press style %+v", got)
	}
	h.send(release(0, 0), input.Mouse{X: 9, Y: 0, Action: input.MouseMove})
	if got := h.buf.At(0, 0).Style; got != Theme.ButtonFocus {
		t.Fatalf("focus style %+v", got)
	}
}

type field struct {
	value string
	mask  rune
}

func (f *field) element() view.Element {
	return view.E(Input{Value: f.value, OnChange: func(s string) { f.value = s }, AutoFocused: true, Mask: f.mask, Placeholder: "filter"})
}

func TestInputEditing(t *testing.T) {
	f := &field{}
	h := newHarness(t, 20, 1, f.element)
	h.want("filter")
	h.send(typed("hello world")...)
	h.want("hello world")
	if h.cur.X != 11 || !h.cur.Visible {
		t.Fatalf("cursor %+v", h.cur)
	}
	h.send(ctrl('w'))
	if f.value != "hello " {
		t.Fatalf("ctrl+w: %q", f.value)
	}
	h.send(key(input.KeyHome), ch('>'), key(input.KeyEnd), key(input.KeyBackspace))
	if f.value != ">hello" {
		t.Fatalf("home/end edit: %q", f.value)
	}
	h.send(input.Key{Code: input.KeyLeft, Mods: input.ModCtrl}, key(input.KeyDelete))
	if f.value != "hello" {
		t.Fatalf("word left + delete: %q", f.value)
	}
	h.send(input.Paste{Text: "a\nb"})
	if f.value != "a bhello" {
		t.Fatalf("paste: %q", f.value)
	}
	h.send(ctrl('u'))
	if f.value != "hello" {
		t.Fatalf("ctrl+u: %q", f.value)
	}
}

func TestInputClickPlacesCursorAndScrolls(t *testing.T) {
	f := &field{value: "0123456789abcdef"}
	h := newHarness(t, 8, 1, f.element)
	h.send(key(input.KeyEnd))
	h.want("9abcdef")
	if h.cur.X != 7 {
		t.Fatalf("cursor at end %+v", h.cur)
	}
	h.send(click(2, 0)...)
	h.send(ch('_'))
	if f.value != "0123456789a_bcdef" {
		t.Fatalf("insert at click: %q", f.value)
	}
}

func TestInputWideCharactersAndMask(t *testing.T) {
	f := &field{}
	h := newHarness(t, 10, 1, f.element)
	h.send(typed("日本")...)
	if h.cur.X != 4 {
		t.Fatalf("cursor after wide %+v", h.cur)
	}
	h.send(key(input.KeyLeft), key(input.KeyBackspace))
	if f.value != "本" {
		t.Fatalf("backspace wide: %q", f.value)
	}
	f.value, f.mask = "pass", '•'
	h.frame()
	h.want("••••")
}

type listModel struct {
	count    int
	selected int
	built    map[int]bool
	act      []int
}

func (m *listModel) element() view.Element {
	m.built = map[int]bool{}
	return view.E(List{
		Count: m.count, Selected: m.selected, AutoFocused: true,
		OnSelect:   func(i int) { m.selected = i },
		OnActivate: func(i int) { m.act = append(m.act, i) },
		Row: func(i int, _ bool) view.Element {
			m.built[i] = true
			return Label(fmt.Sprintf("row %d", i))
		},
	})
}

func TestListBuildsOnlyVisibleRows(t *testing.T) {
	m := &listModel{count: 1_000_000}
	h := newHarness(t, 12, 4, m.element)
	h.want("row 0      ┃", "row 1      │", "row 2      │", "row 3      │")
	if len(m.built) != 4 {
		t.Fatalf("built %d rows", len(m.built))
	}
	h.send(key(input.KeyEnd))
	if m.selected != 999_999 {
		t.Fatalf("end: %d", m.selected)
	}
	h.want("row 999996 │", "row 999997 │", "row 999998 │", "row 999999 ┃")
}

func TestListKeysScrollToSelection(t *testing.T) {
	m := &listModel{count: 10}
	h := newHarness(t, 10, 3, m.element)
	h.send(key(input.KeyDown), key(input.KeyDown), key(input.KeyDown))
	if m.selected != 3 {
		t.Fatalf("down: %d", m.selected)
	}
	h.want("row 1    ┃", "row 2    │", "row 3    │")
	if got := h.buf.At(0, 2).Style; got.Bg != Theme.Selected.Bg {
		t.Fatalf("selected row style %+v", got)
	}
	h.send(ch('k'), key(input.KeyPageDown))
	if m.selected != 4 {
		t.Fatalf("k then pgdown: %d", m.selected)
	}
	h.send(key(input.KeyEnter))
	if !slices.Equal(m.act, []int{4}) {
		t.Fatalf("activate: %v", m.act)
	}
}

func TestListMouse(t *testing.T) {
	m := &listModel{count: 50}
	h := newHarness(t, 10, 5, m.element)
	h.send(click(2, 3)...)
	if m.selected != 3 {
		t.Fatalf("click: %d", m.selected)
	}
	h.send(wheelDown(2, 2))
	h.want("row 3    ┃", "row 4    │", "row 5    │", "row 6    │", "row 7    │")
	if m.selected != 3 {
		t.Fatalf("wheel changed selection: %d", m.selected)
	}
	// A double click on the same cell activates.
	h.tree.Dispatch(press(2, 1))
	h.tree.Dispatch(release(2, 1))
	h.tree.Dispatch(press(2, 1))
	h.tree.Dispatch(release(2, 1))
	if !slices.Equal(m.act, []int{4}) || m.selected != 4 {
		t.Fatalf("double click: act %v selected %d", m.act, m.selected)
	}
}

func TestListScrollbarDrag(t *testing.T) {
	m := &listModel{count: 100}
	h := newHarness(t, 10, 10, m.element)
	// The thumb is one row at the top; drag it to the bottom.
	h.send(press(9, 0), drag(9, 9), release(9, 9))
	h.want("row 90   │", "row 91   │", "row 92   │", "row 93   │", "row 94   │",
		"row 95   │", "row 96   │", "row 97   │", "row 98   │", "row 99   ┃")
	// A click on the track above the thumb pages up.
	h.send(click(9, 0)...)
	if got := h.lines()[0]; got != "row 80   │" {
		t.Fatalf("track click: %q", got)
	}
}

func TestListKeepsRowStateByKey(t *testing.T) {
	items := []string{"a", "b", "c"}
	sel := 1
	h := newHarness(t, 6, 3, func() view.Element {
		return view.E(List{Count: len(items), Selected: sel, Key: func(i int) string { return items[i] },
			Row: func(i int, _ bool) view.Element { return Label(items[i]) }})
	})
	before := h.tree.Root().Child(1)
	items = []string{"z", "a", "b", "c"}
	h.frame()
	if after := h.tree.Root().Child(2); after != before {
		t.Fatal("row node did not follow its key")
	}
}

type tableModel struct {
	rows     [][]string
	sortBy   int
	desc     bool
	selected int
}

func (m *tableModel) element() view.Element {
	return view.E(Table{
		Columns: []TableColumn{{Title: "Host"}, {Title: "Port", Align: layout.End}, {Title: "State", Width: layout.Grow(1)}},
		Count:   len(m.rows),
		Cell:    func(r, c int) Span { return Span{Text: m.rows[r][c]} },
		SortBy:  m.sortBy, SortDesc: m.desc, Selected: m.selected,
		OnSelect: func(i int) { m.selected = i },
		OnSort: func(c int) {
			if m.sortBy == c {
				m.desc = !m.desc
			}
			m.sortBy = c
			slices.SortStableFunc(m.rows, func(a, b []string) int {
				if m.desc {
					return strings.Compare(b[c], a[c])
				}
				return strings.Compare(a[c], b[c])
			})
		},
	})
}

func TestTableLaysOutColumnsAndSorts(t *testing.T) {
	m := &tableModel{rows: [][]string{{"b.example", "443", "ESTABLISHED"}, {"a.example", "80", "TIME_WAIT"}}, sortBy: -1}
	h := newHarness(t, 30, 3, m.element)
	h.want("Host        Port State", "b.example    443 ESTABLISHED", "a.example     80 TIME_WAIT")
	h.send(click(1, 0)...)
	h.want("Host ▲      Port State", "a.example     80 TIME_WAIT", "b.example    443 ESTABLISHED")
	h.send(click(1, 0)...)
	h.want("Host ▼      Port State", "b.example    443 ESTABLISHED", "a.example     80 TIME_WAIT")
	h.send(click(2, 2)...)
	if m.selected != 1 {
		t.Fatalf("row click: %d", m.selected)
	}
}

func TestSplitDividerDrags(t *testing.T) {
	h := newHarness(t, 11, 1, func() view.Element {
		return SplitPanes(Horizontal, Label("left"), Label("right"))
	})
	h.want("left │right")
	h.send(press(5, 0), drag(7, 0), release(7, 0))
	h.want("left   │rig")
	if got := h.buf.At(7, 0).Style; got != Theme.DividerActive {
		t.Fatalf("hovered divider style %+v", got)
	}
	h.send(input.Mouse{X: 0, Y: 0, Action: input.MouseMove})
	if got := h.buf.At(7, 0).Style; got != Theme.Divider {
		t.Fatalf("divider style while a pane is hovered %+v", got)
	}
	// The ratio survives a resize.
	h.buf = cell.NewBuffer(21, 1)
	h.frame()
	if got := h.lines()[0]; !strings.HasPrefix(got, "left") || strings.Index(got, "│") != 14 {
		t.Fatalf("after resize: %q", got)
	}
}

func TestModalTrapsFocusAndCloses(t *testing.T) {
	open := false
	clicked := ""
	h := newHarness(t, 20, 5, func() view.Element {
		return Layers(
			Column(NewButton("Under", func() { clicked = "under" })),
			view.If(open, view.E(Modal{OnClose: func() { open = false }},
				Panel("Help", Row(NewButton("A", func() { clicked = "a" }), NewButton("B", func() { clicked = "b" })))),
			),
		)
	})
	h.send(key(input.KeyTab)) // focus Under
	open = true
	h.frame()
	if got := h.buf.At(0, 0).Style; got.Attrs&cell.Dim == 0 {
		t.Fatalf("backdrop not dimmed: %+v", got)
	}
	h.send(key(input.KeyTab), key(input.KeyEnter))
	if clicked != "b" {
		t.Fatalf("tab inside the modal, got %q", clicked)
	}
	h.send(key(input.KeyTab), key(input.KeyEnter))
	if clicked != "a" {
		t.Fatalf("tab wraps inside the modal, got %q", clicked)
	}
	h.send(click(0, 0)...)
	if open || clicked != "a" {
		t.Fatalf("backdrop click: open %v clicked %q", open, clicked)
	}
	h.send(key(input.KeyEnter))
	if clicked != "under" {
		t.Fatalf("focus after close: %q", clicked)
	}
	open = true
	h.frame()
	h.send(key(input.KeyEscape))
	if open {
		t.Fatal("escape did not close")
	}
}

func TestScrollWheelAndFollowEnd(t *testing.T) {
	n := 3
	h := newHarness(t, 6, 3, func() view.Element {
		var rows []view.Element
		for i := range n {
			rows = append(rows, Label(fmt.Sprintf("l%d", i)))
		}
		return view.E(Scroll{FollowEnd: true}, Column(rows...))
	})
	h.want("l0", "l1", "l2")
	n = 6
	h.frame()
	h.want("l3   │", "l4   │", "l5   ┃")
	h.send(input.Mouse{X: 0, Y: 0, Button: input.ButtonWheelUp, Action: input.MousePress})
	h.want("l0   ┃", "l1   │", "l2   │")
	n = 8
	h.frame()
	h.want("l0   ┃", "l1   │", "l2   │") // not at the end, so it stays put
}

func TestTextAreaEditing(t *testing.T) {
	value := ""
	h := newHarness(t, 10, 3, func() view.Element {
		return view.E(TextArea{Value: value, OnChange: func(s string) { value = s }, AutoFocused: true})
	})
	h.send(typed("abcdef")...)
	h.send(key(input.KeyEnter))
	h.send(typed("xy")...)
	h.want("abcdef", "xy")
	h.send(key(input.KeyUp))
	if h.cur.X != 2 || h.cur.Y != 0 {
		t.Fatalf("up keeps column: %+v", h.cur)
	}
	h.send(key(input.KeyEnd), key(input.KeyDown))
	if h.cur.X != 2 || h.cur.Y != 1 {
		t.Fatalf("down clamps column: %+v", h.cur)
	}
	h.send(key(input.KeyHome), key(input.KeyBackspace))
	if value != "abcdefxy" {
		t.Fatalf("backspace joins lines: %q", value)
	}
	h.send(input.Paste{Text: "1\r\n2"})
	h.want("abcdef1", "2xy")
}

func TestSparkline(t *testing.T) {
	h := newHarness(t, 6, 1, func() view.Element {
		return view.E(Sparkline{Values: []float64{0, 1, 2, 4, 8}})
	})
	h.want("  ▁▂▄█")
	h = newHarness(t, 2, 2, func() view.Element {
		return Fixed(2, view.E(Sparkline{Values: []float64{1, 2}}))
	})
	h.want(" █", "██")
}

func TestTabBar(t *testing.T) {
	active := 0
	h := newHarness(t, 30, 2, func() view.Element {
		return Tabs([]string{"Conns", "Stats"}, active, func(i int) { active = i }, Label(fmt.Sprintf("tab %d", active)))
	})
	h.want(" Conns   Stats", "tab 0")
	h.send(click(9, 0)...)
	h.want(" Conns   Stats", "tab 1")
	h.send(key(input.KeyLeft))
	if active != 0 {
		t.Fatalf("left: %d", active)
	}
}

func TestOnKeyCatchesUnusedKeys(t *testing.T) {
	var got []string
	f := &field{}
	h := newHarness(t, 10, 1, func() view.Element {
		return OnKey(func(k input.Key) bool {
			got = append(got, k.String())
			return true
		}, f.element())
	})
	h.send(ch('q'), key(input.KeyF1))
	if f.value != "q" || !slices.Equal(got, []string{"f1"}) {
		t.Fatalf("value %q, handler saw %v", f.value, got)
	}
}
