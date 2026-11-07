package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// TableColumn describes a table column. Width defaults to Auto: the widest of
// the title and the visible cells.
type TableColumn struct {
	Title string
	Width layout.Sizing
	Align layout.Align
}

// Table is a List with columns and a header. Clicking a header calls
// OnSort with the column; SortBy and SortDesc mark the sorted column.
type Table struct {
	Columns []TableColumn
	Count   int
	// Cell returns the text of a cell. It is called only for visible rows.
	Cell        func(row, col int) Span
	Key         func(row int) string
	Selected    int
	OnSelect    func(row int)
	OnActivate  func(row int)
	SortBy      int // -1 for none
	SortDesc    bool
	OnSort      func(col int)
	AutoFocused bool
}

func (Table) BuildsChildren() {}

const columnGap = 1

func (t Table) widths(m uni.Method, total int, first, last int) []int {
	items := make([]layout.Sizing, len(t.Columns))
	for i, col := range t.Columns {
		items[i] = col.Width
		if items[i] == (layout.Sizing{}) {
			items[i] = layout.Auto()
		}
	}
	return layout.Distribute(total, columnGap, items, func(col, limit int) int {
		w := m.Width(t.Columns[col].Title) + 2 // room for the sort marker
		if t.Cell != nil {
			for row := first; row < last; row++ {
				w = max(w, m.Width(t.Cell(row, col).Text))
			}
		}
		return min(w, limit)
	})
}

func (t Table) Layout(n *view.Node, c layout.Constraints) layout.Size {
	size := c.Max()
	// Auto columns measure the rows the list shows; ask it where it is.
	first, rows := 0, max(size.H-1, 0)
	if n.Len() == 2 {
		if st, ok := n.Child(1).State().(*listState); ok {
			first = st.offset
		}
	}
	last := min(first+rows, t.Count)
	width := size.W
	if t.Count > rows && width > 1 {
		width-- // the list's scrollbar
	}
	widths := t.widths(n.Tree().Method, width, first, last)
	list := List{
		Count: t.Count, Key: t.Key, Selected: t.Selected, OnSelect: t.OnSelect,
		OnActivate: t.OnActivate, AutoFocused: t.AutoFocused,
		Row: func(i int, selected bool) view.Element {
			return view.E(tableRow{table: t, row: i, widths: widths})
		},
	}
	n.SetChildren([]view.Element{
		view.E(tableHeader{table: t, widths: widths}),
		view.E(list),
	})
	n.LayoutChild(0, layout.Tight(layout.Size{W: size.W, H: min(1, size.H)}))
	n.LayoutChild(1, layout.Tight(layout.Size{W: size.W, H: max(size.H-1, 0)}))
	n.Place(1, layout.Point{Y: 1})
	return size
}

func (Table) Draw(*view.Node, *view.Canvas) {}

func drawCells(c *view.Canvas, widths []int, cols []TableColumn, cell func(col int) Span, base cell.Style) {
	x := 0
	for i, w := range widths {
		if w > 0 {
			sp := cell(i)
			ls := lines(c.Method(), []Span{sp}, base, -1, false)
			l := truncate(ls[0], w, true)
			off := cols[i].Align.Offset(w, lineWidth(l))
			at := x + off
			for _, g := range l {
				at += c.Set(at, 0, g.text, g.width, g.style)
			}
		}
		x += w + columnGap
	}
}

type tableRow struct {
	table  Table
	row    int
	widths []int
}

func (r tableRow) Layout(_ *view.Node, c layout.Constraints) layout.Size { return c.Max() }
func (r tableRow) Draw(_ *view.Node, c *view.Canvas) {
	if r.table.Cell == nil {
		return
	}
	drawCells(c, r.widths, r.table.Columns, func(col int) Span { return r.table.Cell(r.row, col) }, cell.Style{})
}

type tableHeader struct {
	table  Table
	widths []int
}

func (h tableHeader) Layout(_ *view.Node, c layout.Constraints) layout.Size { return c.Max() }
func (h tableHeader) Draw(_ *view.Node, c *view.Canvas) {
	drawCells(c, h.widths, h.table.Columns, func(col int) Span {
		title := h.table.Columns[col].Title
		if col == h.table.SortBy {
			if h.table.SortDesc {
				title += " ▼"
			} else {
				title += " ▲"
			}
		}
		return Span{Text: title, Style: Theme.Header}
	}, Theme.Header)
}

func (h tableHeader) Hoverable() bool { return h.table.OnSort != nil }

func (h tableHeader) Handle(n *view.Node, e view.Event) bool {
	p, ok := e.(*view.PointerEvent)
	if !ok || h.table.OnSort == nil {
		return false
	}
	switch p.Kind {
	case view.Press:
		return p.Button == input.ButtonLeft
	case view.Release:
		x := 0
		for i, w := range h.widths {
			if p.Local.X >= x && p.Local.X < x+w {
				h.table.OnSort(i)
				break
			}
			x += w + columnGap
		}
		return true
	}
	return p.Kind == view.Drag
}
