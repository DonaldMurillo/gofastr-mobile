package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func TestTabCloseInteractions(t *testing.T) {
	for _, gesture := range []string{"mark", "middle title", "middle mark", "keyboard"} {
		t.Run(gesture, func(t *testing.T) {
			active := 0
			var closed []int
			h := newHarness(t, 30, 1, func() view.Element {
				return view.E(TabBar{Titles: []string{"One", "Two"}, Active: active, OnChange: func(index int) { active = index }, OnClose: func(index int) { closed = append(closed, index) }})
			})
			h.want(" One ×   Two ×")
			want := 1
			switch gesture {
			case "mark":
				h.send(click(13, 0)...)
			case "middle title":
				h.send(input.Mouse{X: 9, Y: 0, Button: input.ButtonMiddle, Action: input.MousePress}, input.Mouse{X: 9, Y: 0, Button: input.ButtonMiddle, Action: input.MouseRelease})
			case "middle mark":
				h.send(input.Mouse{X: 13, Y: 0, Button: input.ButtonMiddle, Action: input.MousePress}, input.Mouse{X: 13, Y: 0, Button: input.ButtonMiddle, Action: input.MouseRelease})
			case "keyboard":
				want = 0
				h.send(ctrl('w'))
			}
			if len(closed) != 1 || closed[0] != want || active != 0 {
				t.Fatalf("closed=%v active=%d; want close %d without activation", closed, active, want)
			}
		})
	}
}

func TestTabCloseHoverIsSeparate(t *testing.T) {
	h := newHarness(t, 30, 1, func() view.Element {
		return view.E(TabBar{Titles: []string{"One", "Two"}, OnClose: func(int) {}})
	})
	before := h.buf.At(9, 0).Style
	h.send(input.Mouse{X: 13, Y: 0, Action: input.MouseMove})
	if h.buf.At(13, 0).Style != Theme.ButtonHover {
		t.Fatal("close mark did not highlight")
	}
	if h.buf.At(9, 0).Style != before {
		t.Fatal("close hover highlighted the title")
	}
	title := h.tree.Find(func(node *view.Node) bool {
		widget, ok := node.Widget().(tabTitle)
		return ok && widget.index == 1
	})
	if title == nil || title.Hovered() {
		t.Fatal("close mark must not be inside the title's hover ancestry")
	}
	h.send(input.Mouse{X: 9, Y: 0, Action: input.MouseMove})
	if h.buf.At(9, 0).Style != Theme.ButtonHover || h.buf.At(13, 0).Style != before {
		t.Fatal("title hover did not stay separate from close hover")
	}
}

func TestTabCloseReleaseOutside(t *testing.T) {
	closed := false
	h := newHarness(t, 30, 1, func() view.Element {
		return view.E(TabBar{Titles: []string{"One"}, OnClose: func(int) { closed = true }})
	})
	h.send(press(5, 0), drag(20, 0), release(20, 0))
	if closed {
		t.Fatal("close fired after release outside")
	}
}

func TestTabKeyboardAndWheel(t *testing.T) {
	active := 1
	h := newHarness(t, 40, 1, func() view.Element {
		return view.E(TabBar{Titles: []string{"One", "Two", "Three"}, Active: active, OnChange: func(index int) { active = index }})
	})
	for _, step := range []struct {
		event input.Event
		want  int
	}{
		{key(input.KeyHome), 0},
		{key(input.KeyLeft), 0},
		{key(input.KeyEnd), 2},
		{key(input.KeyRight), 2},
		{input.Mouse{X: 1, Button: input.ButtonWheelUp}, 1},
		{wheelDown(1, 0), 2},
		{input.Mouse{X: 1, Button: input.ButtonWheelLeft}, 1},
		{input.Mouse{X: 1, Button: input.ButtonWheelRight}, 2},
	} {
		h.send(step.event)
		if active != step.want {
			t.Fatalf("after %+v active=%d, want %d", step.event, active, step.want)
		}
	}
	if h.tree.Dispatch(ctrl('w')) {
		t.Fatal("ctrl+w was consumed without OnClose")
	}
}

func TestTabOverflowAndActiveVisibility(t *testing.T) {
	active := 0
	h := newHarness(t, 15, 1, func() view.Element {
		return view.E(TabBar{Titles: []string{"Alpha", "Beta", "Gamma", "Delta"}, Active: active, OnChange: func(index int) { active = index }})
	})
	if !strings.HasSuffix(h.lines()[0], "›") || strings.HasPrefix(h.lines()[0], "‹") {
		t.Fatalf("initial markers: %q", h.lines()[0])
	}
	h.send(click(14, 0)...)
	if !strings.HasPrefix(h.lines()[0], "‹") || !strings.HasSuffix(h.lines()[0], "›") {
		t.Fatalf("scrolled markers: %q", h.lines()[0])
	}
	h.send(click(0, 0)...)
	if strings.HasPrefix(h.lines()[0], "‹") || !strings.Contains(h.lines()[0], "Alpha") {
		t.Fatalf("left marker did not scroll back: %q", h.lines()[0])
	}
	h.send(key(input.KeyEnd))
	if active != 3 || !strings.HasPrefix(h.lines()[0], "‹") || strings.HasSuffix(h.lines()[0], "›") || !strings.Contains(h.lines()[0], "Delta") {
		t.Fatalf("active last tab not revealed: %q, active=%d", h.lines()[0], active)
	}
	for _, index := range []int{2, 1, 0, 3} {
		active = index
		h.frame()
		node := h.tree.Find(func(node *view.Node) bool {
			title, ok := node.Widget().(tabTitle)
			return ok && title.index == active
		})
		if node == nil || node.Visible() != node.Rect() {
			t.Fatalf("active tab %d is clipped", active)
		}
	}
	h.buf = cell.NewBuffer(50, 1)
	h.frame()
	if strings.ContainsAny(h.lines()[0], "‹›") || !strings.HasPrefix(h.lines()[0], " Alpha") {
		t.Fatalf("resize did not reset overflow: %q", h.lines()[0])
	}
}

func TestTabNarrowAndEmpty(t *testing.T) {
	for _, width := range []int{0, 1, 2, 3} {
		for _, titles := range [][]string{nil, {"A very long title", "中"}} {
			newHarness(t, width, 1, func() view.Element {
				return view.E(TabBar{Titles: titles, Active: 10, OnClose: func(int) { t.Fatal("closed invalid tab") }})
			}).send(ctrl('w'))
		}
	}
}

func TestTabsRetainIndependentListState(t *testing.T) {
	active := 0
	h := newHarness(t, 20, 4, func() view.Element {
		content := view.E(List{Count: 20, Selected: -1, NoBar: true, Row: func(index int, _ bool) view.Element {
			return Label(fmt.Sprintf("tab %d row %d", active, index))
		}})
		return Tabs([]string{"One", "Two"}, active, func(index int) { active = index }, content)
	})
	h.send(wheelDown(1, 2))
	if h.lines()[1] != "tab 0 row 3" {
		t.Fatalf("first tab did not scroll: %v", h.lines())
	}
	h.send(click(7, 0)...)
	if h.lines()[1] != "tab 1 row 0" {
		t.Fatalf("second tab inherited scroll: %v", h.lines())
	}
	h.send(wheelDown(1, 2), wheelDown(1, 2))
	h.send(click(1, 0)...)
	if h.lines()[1] != "tab 0 row 3" {
		t.Fatalf("first tab lost scroll: %v", h.lines())
	}
	h.send(click(7, 0)...)
	if h.lines()[1] != "tab 1 row 6" {
		t.Fatalf("second tab lost scroll: %v", h.lines())
	}
}

func TestTabsRetainIndependentCarets(t *testing.T) {
	active := 0
	values := []string{"alpha", "bravo"}
	h := newHarness(t, 20, 2, func() view.Element {
		return Tabs([]string{"One", "Two"}, active, func(index int) { active = index }, view.E(Input{Value: values[active], AutoFocused: true, OnChange: func(text string) { values[active] = text }}))
	})
	h.send(key(input.KeyEnd))
	active = 1
	h.frame()
	h.send(ch('1'))
	active = 0
	h.frame()
	h.send(ch('0'))
	if values[0] != "alpha0" || values[1] != "1bravo" {
		t.Fatalf("carets leaked between tabs: %v", values)
	}
}
