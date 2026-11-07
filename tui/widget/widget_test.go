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

func TestNestedFlexTakesItsContentSize(t *testing.T) {
	h := newHarness(t, 12, 5, func() view.Element {
		return Column(Label("top"), Row(NewButton("Add", nil)), view.E(Input{Placeholder: "type"}))
	})
	h.want("top", " Add", "type")
}

func TestButtonVariants(t *testing.T) {
	h := newHarness(t, 24, 1, func() view.Element {
		return Row(view.E(Button{Label: "D"}), view.E(Button{Label: "P", Variant: ButtonPrimary}),
			view.E(Button{Label: "X", Variant: ButtonDanger}), view.E(Button{Label: "Off", Variant: ButtonDanger, Disabled: true}),
			view.E(Button{Label: "Own", Variant: ButtonPrimary, Style: Theme.Accent}))
	})
	h.want(" D  P  X  Off  Own")
	for _, check := range []struct {
		x     int
		style cell.Style
	}{{0, Theme.Button}, {3, Theme.ButtonPrimary}, {6, Theme.ButtonDanger}, {9, Theme.Muted}, {14, Theme.Accent}} {
		if got := h.buf.At(check.x, 0).Style; got != check.style {
			t.Fatalf("style at %d: %+v, want %+v", check.x, got, check.style)
		}
	}
	h.send(input.Mouse{X: 6, Y: 0, Action: input.MouseMove})
	if got := h.buf.At(6, 0).Style; got != Theme.ButtonHover {
		t.Fatalf("variant hover: %+v", got)
	}
	h.send(press(6, 0))
	if got := h.buf.At(6, 0).Style; got != Theme.ButtonPress {
		t.Fatalf("variant press: %+v", got)
	}
	h.send(release(6, 0), input.Mouse{X: 23, Y: 0, Action: input.MouseMove})
	if got := h.buf.At(6, 0).Style; got != Theme.ButtonFocus {
		t.Fatalf("variant focus: %+v", got)
	}
}

func TestButtonAutoFocused(t *testing.T) {
	clicked := false
	h := newHarness(t, 15, 1, func() view.Element {
		return Row(NewButton("First", nil), view.E(Button{Label: "Auto", AutoFocused: true, OnClick: func() { clicked = true }}))
	})
	h.want(" First  Auto")
	if got := h.tree.Focused(); got == nil || got.Widget().(Button).Label != "Auto" {
		t.Fatal("AutoFocused button did not take focus")
	}
	h.send(key(input.KeyEnter))
	if !clicked {
		t.Fatal("autofocused button did not activate")
	}
}

func TestDialogRendersAndNavigates(t *testing.T) {
	open := false
	clicked, underneath := "", 0
	h := newHarness(t, 40, 9, func() view.Element {
		return OnKey(func(k input.Key) bool {
			if k.Code == input.KeyTab {
				return false
			}
			underneath++
			return true
		}, Layers(
			NewButton("Under", func() { clicked = "under" }),
			view.If(open, (Dialog{Title: "Update", Text: "Proceed with update?", Width: 30,
				OnClose: func() { open = false }, Buttons: []DialogButton{
					{Label: "Cancel", OnClick: func() { clicked = "cancel" }},
					{Label: "Save", Variant: ButtonPrimary, Default: true, OnClick: func() { clicked = "save" }},
				}}).Element()),
		))
	})
	h.send(key(input.KeyTab))
	previous := h.tree.Focused()
	open = true
	h.frame()
	h.want(" Under", "", "        ╭ Update ──────────────╮", "        │ Proceed with update? │",
		"        │                      │", "        │       Cancel   Save  │", "        ╰──────────────────────╯")
	focused := func(want string) {
		t.Helper()
		if got := h.tree.Focused(); got == nil || got.Widget().(Button).Label != want {
			t.Fatalf("focus is not on %s", want)
		}
	}
	focused("Save")
	h.send(key(input.KeyEnter))
	if clicked != "save" {
		t.Fatalf("default action: %q", clicked)
	}
	h.send(key(input.KeyRight))
	focused("Cancel")
	h.send(key(input.KeyEnter))
	if clicked != "cancel" {
		t.Fatalf("right action: %q", clicked)
	}
	h.send(key(input.KeyLeft))
	focused("Save")
	h.send(key(input.KeyTab))
	focused("Cancel")
	h.send(ch('q'), key(input.KeyF1))
	if underneath != 0 {
		t.Fatalf("keys escaped the dialog: %d", underneath)
	}
	h.send(key(input.KeyEscape))
	if open || h.tree.Focused() != previous {
		t.Fatal("escape did not close and restore focus")
	}
	h.send(key(input.KeyEnter))
	if clicked != "under" {
		t.Fatalf("restored action: %q", clicked)
	}
	open = true
	h.frame()
	h.send(click(0, 8)...)
	if open || h.tree.Focused() != previous {
		t.Fatal("backdrop did not close and restore focus")
	}
}

func TestDialogBodyWidthAndFirstAction(t *testing.T) {
	h := newHarness(t, 70, 9, func() view.Element {
		return Layers((Dialog{Title: "Body", Text: "ignored", Body: Paragraph(strings.Repeat("x", 80)),
			BorderStyle: Theme.NoticeError, Buttons: []DialogButton{{Label: "First"}, {Label: "Second"}}}).Element())
	})
	if got := h.tree.Focused(); got == nil || got.Widget().(Button).Label != "First" {
		t.Fatal("first action did not take focus")
	}
	lines := h.lines()
	if lines[2] != "     │ "+strings.Repeat("x", 56)+" │" || lines[3] != "     │ "+strings.Repeat("x", 24)+strings.Repeat(" ", 32)+" │" {
		t.Fatalf("body or default width:\n%s", strings.Join(lines, "\n"))
	}
	if got := h.buf.At(5, 1).Style; got != Theme.NoticeError {
		t.Fatalf("border style: %+v", got)
	}
	h.root = func() view.Element {
		return Layers((Dialog{Body: view.E(Input{AutoFocused: true}), Buttons: []DialogButton{{Label: "Action"}}}).Element())
	}
	// Reopen to check that an autofocus request in Body cannot beat actions.
	h.tree = view.NewTree(uni.Graphemes)
	h.frame()
	if got := h.tree.Focused(); got == nil {
		t.Fatal("body dialog has no focus")
	} else if b, ok := got.Widget().(Button); !ok || b.Label != "Action" {
		t.Fatal("body autofocus overrode the first action")
	}
	// A body without actions is still a modal and can close.
	closed := false
	h.root = func() view.Element {
		return Layers((Dialog{Text: "Only body", OnClose: func() { closed = true }}).Element())
	}
	h.frame()
	h.send(key(input.KeyEscape))
	if !closed {
		t.Fatal("dialog without actions did not close")
	}
}

func TestConfirmCallbacks(t *testing.T) {
	confirmed, cancelled := 0, 0
	h := newHarness(t, 32, 7, func() view.Element {
		return Layers(Confirm("Confirm", "Apply changes?", "Apply", func() { confirmed++ }, func() { cancelled++ }).Element())
	})
	if !strings.Contains(strings.Join(h.lines(), "\n"), "Cancel   Apply") {
		t.Fatalf("confirm actions: %v", h.lines())
	}
	h.send(key(input.KeyEnter), key(input.KeyLeft), key(input.KeyEnter), key(input.KeyEscape))
	if confirmed != 1 || cancelled != 2 {
		t.Fatalf("confirm %d cancel %d", confirmed, cancelled)
	}
	button := h.tree.Find(func(n *view.Node) bool {
		b, ok := n.Widget().(Button)
		return ok && b.Label == "Apply"
	})
	if button == nil || button.Widget().(Button).Variant != ButtonPrimary {
		t.Fatal("confirm action is not primary")
	}
}

func TestDialogArrowDirection(t *testing.T) {
	h := newHarness(t, 28, 7, func() view.Element {
		return Layers((Dialog{Text: "Choose", Buttons: []DialogButton{{Label: "A"}, {Label: "B", Default: true}, {Label: "C"}}}).Element())
	})
	if !strings.Contains(strings.Join(h.lines(), "\n"), " A   B   C ") {
		t.Fatalf("action row: %v", h.lines())
	}
	for _, step := range []struct {
		key  input.KeyCode
		want string
	}{{input.KeyRight, "C"}, {input.KeyLeft, "B"}, {input.KeyLeft, "A"}, {input.KeyLeft, "C"}, {input.KeyRight, "A"}} {
		h.send(key(step.key))
		if got := h.tree.Focused(); got == nil || got.Widget().(Button).Label != step.want {
			t.Fatalf("after %v focus is not %s", step.key, step.want)
		}
	}
}

func TestNotificationsStackTopRight(t *testing.T) {
	var notices Notifications
	notices.Push(NoticeInfo, "One", "first")
	notices.Push(NoticeSuccess, "Two", "second")
	notices.Push(NoticeWarning, "Three", "third")
	h := newHarness(t, 46, 12, func() view.Element { return Layers(Label("under"), notices.Element()) })
	h.want("under",
		"     ╭ i One ───────────────────────────────╮", "     │ first                                │", "     ╰──────────────────────────────────────╯",
		"     ╭ ✓ Two ───────────────────────────────╮", "     │ second                               │", "     ╰──────────────────────────────────────╯",
		"     ╭ ! Three ─────────────────────────────╮", "     │ third                                │", "     ╰──────────────────────────────────────╯")
	if h.tree.Focused() != nil {
		t.Fatal("notifications took focus")
	}
}

func TestNotificationsMaxAndItems(t *testing.T) {
	notices := Notifications{Max: 2}
	first := notices.Push(NoticeInfo, "One", "first")
	second := notices.Push(NoticeInfo, "Two", "second")
	third := notices.Push(NoticeInfo, "Three", "third")
	if notices.Len() != 2 || !slices.Equal(notices.Items(), []Notice{{second, NoticeInfo, "Two", "second"}, {third, NoticeInfo, "Three", "third"}}) {
		t.Fatalf("bounded items: %v", notices.Items())
	}
	copy := notices.Items()
	copy[0].Title = "changed"
	if notices.Items()[0].Title != "Two" {
		t.Fatal("Items exposed internal storage")
	}
	h := newHarness(t, 44, 9, func() view.Element { return Layers(notices.Element()) })
	if screen := strings.Join(h.lines(), "\n"); strings.Contains(screen, "One") || !strings.Contains(screen, "Two") || !strings.Contains(screen, "Three") {
		t.Fatalf("bounded screen: %s", screen)
	}
	notices.Dismiss(first)
	notices.Dismiss(second)
	if notices.Len() != 1 || notices.Items()[0].ID != third {
		t.Fatalf("dismiss: %v", notices.Items())
	}
	notices.Clear()
	h.frame()
	h.want()
	if notices.Len() != 0 || notices.Push(NoticeError, "Next", "") <= third {
		t.Fatal("Clear did not empty notices and preserve ID sequence")
	}
	var defaults Notifications
	for range 6 {
		defaults.Push(NoticeInfo, "", "")
	}
	if defaults.Len() != 5 || defaults.Items()[0].ID != 2 {
		t.Fatalf("default Max: %v", defaults.Items())
	}
}

func TestNotificationsDismissAndKeepFocus(t *testing.T) {
	var notices Notifications
	first := notices.Push(NoticeInfo, "One", "first")
	second := notices.Push(NoticeInfo, "Two", "second")
	third := notices.Push(NoticeInfo, "Three", "third")
	f := &field{}
	h := newHarness(t, 50, 12, func() view.Element { return Layers(f.element(), notices.Element()) })
	focused := h.tree.Focused()
	h.send(ch('a'))
	h.send(input.Mouse{X: 10, Y: 5, Action: input.MouseMove})
	retained := h.tree.Find(func(n *view.Node) bool { return n.Key() == fmt.Sprint(second) })
	if retained == nil || !retained.Hovered() {
		t.Fatal("second toast not hovered")
	}
	notices.Dismiss(first)
	h.frame()
	if got := h.tree.Find(func(n *view.Node) bool { return n.Key() == fmt.Sprint(second) }); got != retained || !got.Hovered() || got.Rect().Y != 1 {
		t.Fatal("toast lost its keyed node or hover state")
	}
	h.send(click(10, 2)...)
	if notices.Len() != 1 || notices.Items()[0].ID != third {
		t.Fatalf("click dismissed wrong notices: %v", notices.Items())
	}
	h.send(ch('b'))
	if f.value != "ab" || h.tree.Focused() != focused || !h.cur.Visible {
		t.Fatalf("toast stole focus or keys: %q", f.value)
	}
	if got := h.lines()[0]; got != "ab" {
		t.Fatalf("input screen: %q", got)
	}
}

func TestNotificationsClicksPassThroughOutside(t *testing.T) {
	var notices Notifications
	notices.Push(NoticeInfo, "Toast", "message")
	clicks := 0
	h := newHarness(t, 50, 8, func() view.Element {
		return Layers(Fill(OnClick(func(*view.PointerEvent) { clicks++ }, Label("under"))), notices.Element())
	})
	for _, p := range []layout.Point{{X: 0, Y: 2}, {X: 10, Y: 0}, {X: 49, Y: 2}, {X: 10, Y: 4}} {
		h.send(click(p.X, p.Y)...)
	}
	if clicks != 4 || notices.Len() != 1 {
		t.Fatalf("outside clicks: under %d notices %d", clicks, notices.Len())
	}
	h.send(click(10, 2)...)
	if clicks != 4 || notices.Len() != 0 {
		t.Fatalf("toast click: under %d notices %d", clicks, notices.Len())
	}
	h.want("under")
}

func TestNotificationsWrapAt40(t *testing.T) {
	var notices Notifications
	notices.Push(NoticeError, "Wide", strings.Repeat("x", 50))
	h := newHarness(t, 50, 8, func() view.Element { return Layers(notices.Element()) })
	h.want("", "         ╭ ✗ Wide ──────────────────────────────╮",
		"         │ "+strings.Repeat("x", 36)+" │", "         │ "+strings.Repeat("x", 14)+strings.Repeat(" ", 22)+" │",
		"         ╰──────────────────────────────────────╯")
	// Small terminals keep the margin and wrap within the available width.
	h.buf = cell.NewBuffer(14, 10)
	h.frame()
	if got := h.lines()[2]; got != " │ xxxxxxxx │" {
		t.Fatalf("narrow toast: %q", got)
	}
}

func TestNoticeLevelStylesAndGlyphs(t *testing.T) {
	for _, tc := range []struct {
		level NoticeLevel
		style cell.Style
	}{{NoticeInfo, Theme.NoticeInfo}, {NoticeSuccess, Theme.NoticeSuccess}, {NoticeWarning, Theme.NoticeWarning}, {NoticeError, Theme.NoticeError}} {
		t.Run(fmt.Sprint(tc.level), func(t *testing.T) {
			var notices Notifications
			notices.Push(tc.level, "Title", "body")
			h := newHarness(t, 44, 6, func() view.Element { return Layers(notices.Element()) })
			glyph, _ := tc.level.appearance()
			for _, method := range []uni.Method{uni.Codepoints, uni.Graphemes} {
				if method.Width(glyph) != 1 {
					t.Fatalf("glyph %q is not one column with %s", glyph, method)
				}
			}
			if got := h.buf.At(3, 1).Style; got != tc.style {
				t.Fatalf("notice border style: %+v", got)
			}
			if got := h.buf.At(5, 1).Style; got != merge(tc.style, Theme.Title) || h.buf.At(5, 1).Text != glyph {
				t.Fatalf("notice title glyph or style: %+v", got)
			}
		})
	}
}

func TestDialogAndNotificationsClearTheirSurfaces(t *testing.T) {
	for _, kind := range []string{"dialog", "notification"} {
		t.Run(kind, func(t *testing.T) {
			var notices Notifications
			notices.Push(NoticeInfo, "Toast", "body")
			h := newHarness(t, 50, 10, func() view.Element {
				overlay := notices.Element()
				if kind == "dialog" {
					overlay = (Dialog{Text: "body", Buttons: []DialogButton{{Label: "OK"}}}).Element()
				}
				return Layers(Fill(Paragraph(strings.Repeat("u", 500))), overlay)
			})
			box := h.tree.Find(func(n *view.Node) bool { _, ok := n.Widget().(surfaceBox); return ok })
			if box == nil {
				t.Fatal("overlay has no surface")
			}
			r := box.Rect()
			want := strings.Repeat("u", r.X) + "│ body" + strings.Repeat(" ", r.W-7) + "│" + strings.Repeat("u", 50-r.X-r.W)
			if got := h.lines()[r.Y+1]; got != want {
				t.Fatalf("surface body row: %q, want %q", got, want)
			}
			if kind == "dialog" {
				want = strings.Repeat("u", r.X) + "│" + strings.Repeat(" ", r.W-2) + "│" + strings.Repeat("u", 50-r.X-r.W)
				if got := h.lines()[r.Y+2]; got != want {
					t.Fatalf("dialog blank row: %q, want %q", got, want)
				}
			}
		})
	}
}
