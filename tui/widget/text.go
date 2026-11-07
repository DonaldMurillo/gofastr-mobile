package widget

import (
	"strings"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Span is a run of text in one style.
type Span struct {
	Text  string
	Style cell.Style
}

// Text draws styled text. Newlines start new lines. Without Wrap, lines
// longer than the width are cut, with an ellipsis when Ellipsis is set.
type Text struct {
	Spans    []Span
	Wrap     bool
	Ellipsis bool
	Align    layout.Align // Start, Center or End within the width
	Style    cell.Style   // base style; span styles override its zero fields
}

// Label returns a single-line text element.
func Label(text string) view.Element { return view.E(Text{Spans: []Span{{Text: text}}}) }

// Styled returns a single-line text element in style s.
func Styled(text string, s cell.Style) view.Element {
	return view.E(Text{Spans: []Span{{Text: text, Style: s}}})
}

// Paragraph returns wrapping text.
func Paragraph(text string) view.Element {
	return view.E(Text{Spans: []Span{{Text: text}}, Wrap: true})
}

// Line returns a single-line text element made of spans.
func Line(spans ...Span) view.Element { return view.E(Text{Spans: spans}) }

type glyph struct {
	text  string
	width int
	style cell.Style
}

// merge fills the zero fields of s from base.
func merge(base, s cell.Style) cell.Style {
	if s.Fg.IsDefault() {
		s.Fg = base.Fg
	}
	if s.Bg.IsDefault() {
		s.Bg = base.Bg
	}
	if s.UnderlineColor.IsDefault() {
		s.UnderlineColor = base.UnderlineColor
	}
	s.Attrs |= base.Attrs
	if s.Underline == cell.UnderlineNone {
		s.Underline = base.Underline
	}
	if s.Link == "" {
		s.Link = base.Link
	}
	return s
}

// lines splits spans into lines of at most width columns (no limit when
// width < 0), wrapping at spaces when wrap is set.
func lines(m uni.Method, spans []Span, base cell.Style, width int, wrap bool) [][]glyph {
	out := [][]glyph{nil}
	used := 0
	lastSpace := -1 // index in the current line of the last space
	for _, sp := range spans {
		style := merge(base, sp.Style)
		for unit, w := range m.Units(sp.Text) {
			if unit == "\n" || unit == "\r\n" {
				out = append(out, nil)
				used, lastSpace = 0, -1
				continue
			}
			if w == 0 {
				continue
			}
			cur := &out[len(out)-1]
			if wrap && width >= 0 && used+w > width && used > 0 {
				if unit == " " {
					// The space at the break disappears.
					out = append(out, nil)
					used, lastSpace = 0, -1
					continue
				}
				var carry []glyph
				if lastSpace >= 0 {
					carry = append(carry, (*cur)[lastSpace+1:]...)
					*cur = (*cur)[:lastSpace]
				}
				out = append(out, carry)
				cur = &out[len(out)-1]
				used, lastSpace = 0, -1
				for _, g := range carry {
					used += g.width
				}
			}
			if unit == " " {
				lastSpace = len(*cur)
			}
			*cur = append(*cur, glyph{unit, w, style})
			used += w
		}
	}
	return out
}

func lineWidth(l []glyph) int {
	w := 0
	for _, g := range l {
		w += g.width
	}
	return w
}

// truncate cuts l to width columns, ending in an ellipsis when asked.
func truncate(l []glyph, width int, ellipsis bool) []glyph {
	if lineWidth(l) <= width {
		return l
	}
	limit := width
	if ellipsis && width > 0 {
		limit--
	}
	used, n := 0, 0
	for n < len(l) && used+l[n].width <= limit {
		used += l[n].width
		n++
	}
	out := append([]glyph(nil), l[:n]...)
	if ellipsis && width > 0 {
		style := cell.Style{}
		if n > 0 {
			style = l[n-1].style
		} else if len(l) > 0 {
			style = l[0].style
		}
		out = append(out, glyph{"…", 1, style})
	}
	return out
}

func (t Text) Layout(n *view.Node, c layout.Constraints) layout.Size {
	limit := -1
	if t.Wrap {
		limit = c.MaxW
	}
	ls := lines(n.Tree().Method, t.Spans, t.Style, limit, t.Wrap)
	w := 0
	for _, l := range ls {
		w = max(w, lineWidth(l))
	}
	return layout.Size{W: w, H: len(ls)}
}

func (t Text) Draw(n *view.Node, c *view.Canvas) {
	size := c.Size()
	limit := -1
	if t.Wrap {
		limit = size.W
	}
	if !t.Style.Bg.IsDefault() {
		c.Fill(layout.Rect{W: size.W, H: size.H}, t.Style)
	}
	for y, l := range lines(c.Method(), t.Spans, t.Style, limit, t.Wrap) {
		if y >= size.H {
			break
		}
		l = truncate(l, size.W, t.Ellipsis)
		x := t.Align.Offset(size.W, lineWidth(l))
		for _, g := range l {
			x += c.Set(x, y, g.text, g.width, g.style)
		}
	}
}

// Plain returns the text of spans without styles.
func Plain(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
