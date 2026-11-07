package cell

import (
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// Cell is one terminal cell. Text is one grapheme cluster of display Width 1
// or 2. A width-2 cell is followed by a continuation cell with empty Text and
// Width 0, which carries the same style.
type Cell struct {
	Text  string
	Width uint8
	Style Style
}

// Blank is an empty cell in the default style.
var Blank = Cell{Text: " ", Width: 1}

// IsContinuation reports whether c is the right half of a wide cell.
func (c Cell) IsContinuation() bool { return c.Width == 0 }

// Replacement is drawn in place of text that may not reach the terminal.
const Replacement = "�"

var asciiText [128]string

func init() {
	for i := 0x20; i < 0x7f; i++ {
		asciiText[i] = string(rune(i))
	}
}

// Buffer is a grid of cells. Its methods keep wide cells consistent: a write
// over either half of a wide cell blanks the other half.
type Buffer struct {
	width, height int
	cells         []Cell
}

// NewBuffer returns a width by height buffer of blank cells.
func NewBuffer(width, height int) *Buffer {
	b := &Buffer{}
	b.Resize(width, height)
	return b
}

// Width returns the number of columns.
func (b *Buffer) Width() int { return b.width }

// Height returns the number of rows.
func (b *Buffer) Height() int { return b.height }

// Resize sets the size and blanks every cell.
func (b *Buffer) Resize(width, height int) {
	width, height = max(width, 0), max(height, 0)
	b.width, b.height = width, height
	if cap(b.cells) >= width*height {
		b.cells = b.cells[:width*height]
	} else {
		b.cells = make([]Cell, width*height)
	}
	b.Clear()
}

// Clear blanks every cell in the default style.
func (b *Buffer) Clear() {
	for i := range b.cells {
		b.cells[i] = Blank
	}
}

// At returns the cell at x, y, or Blank outside the buffer.
func (b *Buffer) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		return Blank
	}
	return b.cells[y*b.width+x]
}

// Fill sets a rectangle to blanks in style s, clipped to the buffer.
func (b *Buffer) Fill(x, y, width, height int, s Style) {
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := min(x+width, b.width), min(y+height, b.height)
	for row := y0; row < y1; row++ {
		for col := x0; col < x1; col++ {
			b.Set(col, row, " ", 1, s)
		}
	}
}

// Set writes one grapheme of the given display width at x, y and returns the
// number of columns it used: width, or 0 when nothing was written.
//
// Text containing control characters, invalid UTF-8, or only zero-width code
// points (with a positive width supplied) is drawn as U+FFFD.
// A width-2 grapheme that does not fit in the last column becomes a blank.
func (b *Buffer) Set(x, y int, text string, width int, s Style) int {
	if x < 0 || y < 0 || x >= b.width || y >= b.height || width <= 0 {
		return 0
	}
	width = min(width, 2)
	text = safeText(text)
	if width == 2 && x == b.width-1 {
		text, width = " ", 1
	}
	row := b.cells[y*b.width : (y+1)*b.width]
	b.release(row, x)
	if width == 2 {
		b.release(row, x+1)
		row[x] = Cell{Text: text, Width: 2, Style: s}
		row[x+1] = Cell{Width: 0, Style: s}
		return 2
	}
	row[x] = Cell{Text: text, Width: 1, Style: s}
	return 1
}

// release blanks the other half of any wide cell occupying column x, so x
// can be overwritten.
func (b *Buffer) release(row []Cell, x int) {
	switch c := row[x]; {
	case c.Width == 0 && x > 0:
		row[x-1] = Cell{Text: " ", Width: 1, Style: row[x-1].Style}
	case c.Width == 2 && x+1 < len(row):
		row[x+1] = Cell{Text: " ", Width: 1, Style: c.Style}
	}
}

// safeText returns text if it is valid UTF-8 with a printable base, else
// Replacement. Marks and selectors with a base remain part of its cluster.
func safeText(text string) string {
	if len(text) == 1 {
		if c := text[0]; c >= 0x20 && c < 0x7f {
			return asciiText[c]
		}
		return Replacement
	}
	if text == "" {
		return " "
	}
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && n == 1 || r < 0x20 || r >= 0x7f && r <= 0x9f {
			return Replacement
		}
		i += n
	}
	if uni.Graphemes.Width(text) == 0 {
		return Replacement
	}
	return text
}
