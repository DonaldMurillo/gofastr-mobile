package widget

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// TabBar shows scrollable titles. Arrows, Home, End and the wheel select tabs.
type TabBar struct {
	Titles   []string
	Active   int
	OnChange func(i int)
	// OnClose enables close marks, middle-click and ctrl+w.
	OnClose func(i int)
}

type tabBarState struct {
	offset, viewport, total int
	active, width           int
	first                   bool
}

// Tabs shows a bar above content and retains each tab's state by index.
func Tabs(titles []string, active int, onChange func(int), content view.Element) view.Element {
	return Column(
		view.E(TabBar{Titles: titles, Active: active, OnChange: onChange}),
		Grow(1, view.E(tabContents{count: len(titles), active: active, content: content})),
	)
}

func (TabBar) NewState() any     { return &tabBarState{first: true} }
func (TabBar) BuildsChildren()   {}
func (t TabBar) Focusable() bool { return t.OnChange != nil || t.OnClose != nil }

func (t TabBar) spans(n *view.Node) (starts, widths []int) {
	column := 0
	for _, title := range t.Titles {
		width := n.Tree().Method.Width(title) + 2
		if t.OnClose != nil {
			width += 2
		}
		starts, widths = append(starts, column), append(widths, width)
		column += width + 1
	}
	return starts, widths
}

func (t TabBar) Layout(n *view.Node, constraints layout.Constraints) layout.Size {
	state := n.State().(*tabBarState)
	starts, widths := t.spans(n)
	total := 0
	if len(starts) > 0 {
		total = starts[len(starts)-1] + widths[len(widths)-1]
	}
	width := constraints.MaxW
	viewport, inset := width, 0
	if total > width {
		viewport, inset = max(width-2, 0), min(width, 1)
	}
	if state.first || state.active != t.Active || state.width != width || state.total != total {
		if t.Active >= 0 && t.Active < len(starts) {
			start, end := starts[t.Active], starts[t.Active]+widths[t.Active]
			if start < state.offset {
				state.offset = start
			}
			if end > state.offset+viewport {
				state.offset = min(start, end-viewport)
			}
		}
	}
	state.offset = min(max(state.offset, 0), max(total-viewport, 0))
	state.first, state.active, state.width = false, t.Active, width
	state.viewport, state.total = viewport, total
	children := []view.Element{view.E(tabStrip{bar: t, offset: state.offset}).WithKey("strip")}
	positions := []int{inset}
	childWidths := []int{viewport}
	if state.offset > 0 && width > 0 {
		children = append(children, view.E(tabMarker{direction: -1}).WithKey("left"))
		positions, childWidths = append(positions, 0), append(childWidths, 1)
	}
	if state.offset+viewport < total && width > 1 {
		children = append(children, view.E(tabMarker{direction: 1}).WithKey("right"))
		positions, childWidths = append(positions, width-1), append(childWidths, 1)
	}
	n.SetChildren(children)
	for index := range children {
		n.LayoutChild(index, layout.Tight(layout.Size{W: childWidths[index], H: 1}))
		n.Place(index, layout.Point{X: positions[index]})
	}
	return layout.Size{W: width, H: 1}
}

func (TabBar) Draw(*view.Node, *view.Canvas) {}

func (t TabBar) change(index int) {
	if t.OnChange != nil && index >= 0 && index < len(t.Titles) && index != t.Active {
		t.OnChange(index)
	}
}

func (t TabBar) Handle(_ *view.Node, event view.Event) bool {
	switch event := event.(type) {
	case *view.PointerEvent:
		if event.Kind != view.Wheel || t.OnChange == nil {
			return false
		}
		switch event.Button {
		case input.ButtonWheelUp, input.ButtonWheelLeft:
			t.change(t.Active - 1)
		case input.ButtonWheelDown, input.ButtonWheelRight:
			t.change(t.Active + 1)
		}
		return true
	case view.KeyEvent:
		if event.Code == input.KeyRune && event.Rune == 'w' && event.Mods == input.ModCtrl && t.OnClose != nil && t.Active >= 0 && t.Active < len(t.Titles) {
			t.OnClose(t.Active)
			return true
		}
		if event.Mods != 0 || t.OnChange == nil {
			return false
		}
		switch event.Code {
		case input.KeyLeft:
			t.change(t.Active - 1)
		case input.KeyRight:
			t.change(t.Active + 1)
		case input.KeyHome:
			t.change(0)
		case input.KeyEnd:
			t.change(len(t.Titles) - 1)
		default:
			return false
		}
		return true
	}
	return false
}

type tabStrip struct {
	bar    TabBar
	offset int
}

func (tabStrip) BuildsChildren() {}
func (strip tabStrip) Layout(n *view.Node, constraints layout.Constraints) layout.Size {
	starts, widths := strip.bar.spans(n)
	var children []view.Element
	var positions, sizes []int
	for index, title := range strip.bar.Titles {
		key := strconv.Itoa(index)
		children = append(children, view.E(tabTitle{bar: strip.bar, index: index, title: title}).WithKey("title:"+key))
		positions, sizes = append(positions, starts[index]-strip.offset), append(sizes, widths[index])
		if strip.bar.OnClose != nil {
			children = append(children, view.E(tabClose{bar: strip.bar, index: index}).WithKey("close:"+key))
			positions, sizes = append(positions, starts[index]+widths[index]-3-strip.offset), append(sizes, 2)
		}
	}
	n.SetChildren(children)
	for index := range children {
		n.LayoutChild(index, layout.Tight(layout.Size{W: sizes[index], H: 1}))
		n.Place(index, layout.Point{X: positions[index]})
	}
	return constraints.Max()
}
func (tabStrip) Draw(*view.Node, *view.Canvas) {}

func tabStyle(bar TabBar, index int, focused bool) cell.Style {
	style := Theme.Tab
	if index == bar.Active {
		style = Theme.TabActive
		if !focused {
			style.Attrs &^= cell.Bold
		}
	}
	return style
}

type tabTitle struct {
	bar   TabBar
	index int
	title string
}

func (tabTitle) Layout(_ *view.Node, constraints layout.Constraints) layout.Size {
	return constraints.Max()
}
func (tabTitle) Hoverable() bool { return true }
func (title tabTitle) Draw(n *view.Node, canvas *view.Canvas) {
	style := tabStyle(title.bar, title.index, n.Parent().Parent().Focused())
	if n.Hovered() {
		style = Theme.ButtonHover
	}
	canvas.Fill(layout.Rect{W: canvas.Size().W, H: 1}, style)
	canvas.TextLimit(1, 0, n.Tree().Method.Width(title.title), title.title, style)
}
func (title tabTitle) Handle(_ *view.Node, event view.Event) bool {
	pointer, ok := event.(*view.PointerEvent)
	if !ok {
		return false
	}
	if pointer.Kind == view.Press {
		switch pointer.Button {
		case input.ButtonLeft:
			title.bar.change(title.index)
			return true
		case input.ButtonMiddle:
			if title.bar.OnClose != nil {
				title.bar.OnClose(title.index)
				return true
			}
		}
	}
	return pointer.Kind == view.Enter || pointer.Kind == view.Leave || pointer.Kind == view.Release || pointer.Kind == view.Drag
}

type tabClose struct {
	bar   TabBar
	index int
}

func (tabClose) NewState() any   { return &buttonState{} }
func (tabClose) Hoverable() bool { return true }
func (tabClose) Layout(_ *view.Node, constraints layout.Constraints) layout.Size {
	return constraints.Max()
}
func (close tabClose) Draw(n *view.Node, canvas *view.Canvas) {
	style := tabStyle(close.bar, close.index, n.Parent().Parent().Focused())
	if n.Hovered() {
		style = Theme.ButtonHover
	}
	canvas.Restyle(layout.Rect{W: 2, H: 1}, func(cell.Style) cell.Style { return style })
	canvas.Text(0, 0, " ×", style)
}
func (close tabClose) Handle(n *view.Node, event view.Event) bool {
	pointer, ok := event.(*view.PointerEvent)
	if !ok {
		return false
	}
	state := n.State().(*buttonState)
	switch pointer.Kind {
	case view.Press:
		if pointer.Button == input.ButtonMiddle {
			close.bar.OnClose(close.index)
			return true
		}
		if pointer.Button != input.ButtonLeft {
			return false
		}
		state.pressed = true
	case view.Release:
		fire := state.pressed && n.Visible().Contains(pointer.Pos)
		state.pressed = false
		if fire {
			close.bar.OnClose(close.index)
		}
	case view.Enter, view.Leave, view.Drag:
	default:
		return false
	}
	return true
}

type tabMarker struct{ direction int }

func (tabMarker) Layout(_ *view.Node, constraints layout.Constraints) layout.Size {
	return constraints.Max()
}
func (tabMarker) Hoverable() bool { return true }
func (marker tabMarker) Draw(n *view.Node, canvas *view.Canvas) {
	text, style := "‹", Theme.Tab
	if marker.direction > 0 {
		text = "›"
	}
	if n.Hovered() {
		style = Theme.ButtonHover
	}
	canvas.Text(0, 0, text, style)
}
func (marker tabMarker) Handle(n *view.Node, event view.Event) bool {
	pointer, ok := event.(*view.PointerEvent)
	if !ok {
		return false
	}
	if pointer.Kind == view.Press && pointer.Button == input.ButtonLeft {
		state := n.Parent().State().(*tabBarState)
		state.offset += marker.direction * max(state.viewport, 1)
		return true
	}
	return pointer.Kind == view.Enter || pointer.Kind == view.Leave || pointer.Kind == view.Release || pointer.Kind == view.Drag
}

type tabContents struct {
	count, active int
	content       view.Element
}

func (tabContents) NewState() any   { return &[]view.Element{} }
func (tabContents) BuildsChildren() {}
func (tabs tabContents) Layout(n *view.Node, constraints layout.Constraints) layout.Size {
	saved := n.State().(*[]view.Element)
	for len(*saved) < tabs.count {
		*saved = append(*saved, view.Empty)
	}
	*saved = (*saved)[:max(tabs.count, 0)]
	if tabs.active >= 0 && tabs.active < len(*saved) {
		(*saved)[tabs.active] = view.E(tabContent{}, tabs.content).WithKey(strconv.Itoa(tabs.active))
	}
	n.SetChildren(*saved)
	if tabs.active >= 0 && tabs.active < n.Len() {
		n.LayoutChild(tabs.active, constraints)
	}
	return constraints.Max()
}
func (tabContents) Draw(*view.Node, *view.Canvas) {}

type tabContent struct{}

func (tabContent) Layout(n *view.Node, constraints layout.Constraints) layout.Size {
	return n.LayoutChild(0, constraints)
}
func (tabContent) Draw(*view.Node, *view.Canvas) {}
