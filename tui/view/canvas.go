package view

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"image"
)

// Canvas draws one node. Coordinates are relative to the node's top-left
// corner and everything is clipped to the node's visible area.
//
// A style whose background is the default color keeps the background
// already in the cell, so text drawn over a filled panel stays on the
// panel. Fill with an explicit style to reset it.
type Canvas struct {
	buf                   *cell.Buffer
	origin                layout.Point
	size                  layout.Size
	clip                  layout.Rect // absolute
	method                uni.Method
	images                cell.ImageProtocol
	cellWidth, cellHeight int
}

// ImageProtocol returns the tree's terminal graphics capability.
func (c *Canvas) ImageProtocol() cell.ImageProtocol { return c.images }

// CellPixels returns the tree's pixel geometry, defaulting to 10 by 20.
func (c *Canvas) CellPixels() (int, int) {
	return (cell.Options{CellWidth: c.cellWidth, CellHeight: c.cellHeight}).CellPixels()
}

// PlaceImage draws a pixel-protocol placement in node coordinates, clipping
// both its cell rectangle and source pixels against all ancestor clips.
func (c *Canvas) PlaceImage(img *cell.Image, rect layout.Rect, crop image.Rectangle) {
	if c.images == cell.ImageNone || img == nil || img.Pixels == nil {
		return
	}
	if crop.Empty() {
		crop = img.Pixels.Bounds()
	}
	abs := image.Rect(c.origin.X+rect.X, c.origin.Y+rect.Y, c.origin.X+rect.X+rect.W, c.origin.Y+rect.Y+rect.H)
	clip := image.Rect(c.clip.X, c.clip.Y, c.clip.X+c.clip.W, c.clip.Y+c.clip.H)
	abs, crop = cell.ClipImage(abs, crop, clip)
	if !abs.Empty() && !crop.Empty() {
		c.buf.PlaceImage(img, abs, crop)
	}
}

// Size returns the node's size.
func (c *Canvas) Size() layout.Size { return c.size }

// Method returns the width model text is laid out with.
func (c *Canvas) Method() uni.Method { return c.method }

func (c *Canvas) inside(x, y int) bool {
	return c.clip.Contains(layout.Point{X: x, Y: y})
}

// Set draws one unit of text (a grapheme or code point unit from
// uni.Method.Next) at x, y and returns the columns it advanced: width, or
// 0 outside the node. A wide unit cut by the clip edge is drawn as a blank.
func (c *Canvas) Set(x, y int, text string, width int, s cell.Style) int {
	ax, ay := c.origin.X+x, c.origin.Y+y
	if width <= 0 || !c.inside(ax, ay) {
		return max(width, 0)
	}
	if s.Bg.IsDefault() {
		s.Bg = c.buf.At(ax, ay).Style.Bg
	}
	if width == 2 && !c.inside(ax+1, ay) {
		c.buf.Set(ax, ay, " ", 1, s)
		return 2
	}
	c.buf.Set(ax, ay, text, width, s)
	return width
}

// Fill paints a rectangle with blanks in style s.
func (c *Canvas) Fill(r layout.Rect, s cell.Style) {
	abs := layout.Rect{X: c.origin.X + r.X, Y: c.origin.Y + r.Y, W: r.W, H: r.H}.Intersect(c.clip)
	if abs.Empty() {
		return
	}
	if s.Bg.IsDefault() {
		for y := abs.Y; y < abs.Y+abs.H; y++ {
			for x := abs.X; x < abs.X+abs.W; x++ {
				t := s
				t.Bg = c.buf.At(x, y).Style.Bg
				c.buf.Set(x, y, " ", 1, t)
			}
		}
		return
	}
	c.buf.Fill(abs.X, abs.Y, abs.W, abs.H, s)
}

// Text draws a single line starting at x, y, cut at the node's right edge,
// and returns the columns it used.
func (c *Canvas) Text(x, y int, text string, s cell.Style) int {
	return c.TextLimit(x, y, c.size.W-x, text, s)
}

// TextLimit draws a single line in at most limit columns and returns the
// columns used. Zero-width units are skipped.
func (c *Canvas) TextLimit(x, y, limit int, text string, s cell.Style) int {
	used := 0
	for unit, w := range c.method.Units(text) {
		if w == 0 {
			continue
		}
		if used+w > limit {
			break
		}
		c.Set(x+used, y, unit, w, s)
		used += w
	}
	return used
}

// Restyle changes the style of the cells in r without changing their text,
// for highlights such as selection.
func (c *Canvas) Restyle(r layout.Rect, change func(cell.Style) cell.Style) {
	abs := layout.Rect{X: c.origin.X + r.X, Y: c.origin.Y + r.Y, W: r.W, H: r.H}.Intersect(c.clip)
	restyle(c.buf, abs, change)
}

func restyle(buf *cell.Buffer, abs layout.Rect, change func(cell.Style) cell.Style) {
	for y := abs.Y; y < abs.Y+abs.H; y++ {
		for x := abs.X; x < abs.X+abs.W; x++ {
			cl := buf.At(x, y)
			if cl.IsContinuation() {
				continue
			}
			buf.Set(x, y, cl.Text, int(cl.Width), change(cl.Style))
		}
	}
}
