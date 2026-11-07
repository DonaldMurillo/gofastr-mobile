package widget

import (
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// NoticeLevel selects a notification's color and leading glyph.
type NoticeLevel int

const (
	NoticeInfo NoticeLevel = iota
	NoticeSuccess
	NoticeWarning
	NoticeError
)

// Notice is one notification, identified by ID.
type Notice struct {
	ID          int
	Level       NoticeLevel
	Title, Text string
}

// Notifications holds notifications owned by the caller. Dismiss them on
// the caller's timer or by clicking. Use it from the app loop only.
type Notifications struct {
	items  []Notice
	nextID int
	Max    int // maximum count; 5 when zero
}

// Push appends a notice and drops the oldest notices above Max.
func (n *Notifications) Push(level NoticeLevel, title, text string) int {
	n.nextID++
	n.items = append(n.items, Notice{ID: n.nextID, Level: level, Title: title, Text: text})
	limit := n.Max
	if limit <= 0 {
		limit = 5
	}
	if len(n.items) > limit {
		n.items = slices.Delete(n.items, 0, len(n.items)-limit)
	}
	return n.nextID
}

// Dismiss removes the notice with id, if present.
func (n *Notifications) Dismiss(id int) {
	for i, item := range n.items {
		if item.ID == id {
			n.items = slices.Delete(n.items, i, i+1)
			return
		}
	}
}

// Clear removes all notices. IDs are never reused.
func (n *Notifications) Clear() { n.items = nil }

// Len returns the number of notices.
func (n *Notifications) Len() int { return len(n.items) }

// Items returns a copy of the notices, oldest first.
func (n *Notifications) Items() []Notice { return slices.Clone(n.items) }

func (l NoticeLevel) appearance() (string, cell.Style) {
	switch l {
	case NoticeSuccess:
		return "✓", Theme.NoticeSuccess
	case NoticeWarning:
		return "!", Theme.NoticeWarning
	case NoticeError:
		return "✗", Theme.NoticeError
	default:
		return "i", Theme.NoticeInfo
	}
}

// Element returns a non-modal stack in the top-right corner of Layers,
// with a one-cell margin and notices at most 40 columns wide.
func (n *Notifications) Element() view.Element {
	items := make([]view.Element, len(n.items))
	for i, item := range n.items {
		glyph, style := item.Level.appearance()
		toast := view.E(surfaceBox{Box{Border: BorderRounded, Title: glyph + " " + item.Title, BorderStyle: style, NoFocusBorder: true, Padding: layout.Symmetric(0, 1), Style: Theme.Text}}, Paragraph(item.Text))
		items[i] = OnClick(func(*view.PointerEvent) { n.Dismiss(item.ID) }, toast).WithKey(strconv.Itoa(item.ID))
	}
	// Equal widths and no gaps keep the stack's whole rectangle clickable.
	// The margin belongs to positioning, outside that rectangle.
	e := Anchor(layout.End, layout.Start, view.E(noticeStack{}, items...))
	p := e.Widget.(position)
	p.margin = 1
	e.Widget = p
	return e
}

type noticeStack struct{}

func (noticeStack) Layout(n *view.Node, c layout.Constraints) layout.Size {
	if n.Len() == 0 {
		return layout.Size{}
	}
	width := min(c.MaxW, 40)
	return (Flex{Dir: Vertical, Align: layout.Stretch}).Layout(n, layout.Constraints{MinW: width, MaxW: width, MaxH: c.MaxH})
}

func (noticeStack) Draw(*view.Node, *view.Canvas) {}
