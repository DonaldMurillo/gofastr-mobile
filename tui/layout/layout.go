// Package layout has the geometry types and the space distribution rules the
// view package lays elements out with. Everything here is pure arithmetic on
// integer cells.
package layout

// Point is a cell position.
type Point struct{ X, Y int }

// Add returns p moved by q.
func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }

// Size is a width and height in cells.
type Size struct{ W, H int }

// Rect is a rectangle of cells. The zero Rect is empty.
type Rect struct{ X, Y, W, H int }

// Empty reports whether r has no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether the cell at p is inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.W && p.Y < r.Y+r.H
}

// Intersect returns the cells in both r and s.
func (r Rect) Intersect(s Rect) Rect {
	x0, y0 := max(r.X, s.X), max(r.Y, s.Y)
	x1, y1 := min(r.X+r.W, s.X+s.W), min(r.Y+r.H, s.Y+s.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Inset shrinks r by the given insets, never below zero size.
func (r Rect) Inset(in Insets) Rect {
	w := max(r.W-in.Left-in.Right, 0)
	h := max(r.H-in.Top-in.Bottom, 0)
	return Rect{r.X + in.Left, r.Y + in.Top, w, h}
}

// Origin returns the top-left corner.
func (r Rect) Origin() Point { return Point{r.X, r.Y} }

// Size returns the width and height.
func (r Rect) Size() Size { return Size{r.W, r.H} }

// Insets are distances from each edge.
type Insets struct{ Top, Right, Bottom, Left int }

// Uniform returns n on every side.
func Uniform(n int) Insets { return Insets{n, n, n, n} }

// Symmetric returns v on top and bottom and h on left and right.
func Symmetric(v, h int) Insets { return Insets{v, h, v, h} }

// Horizontal returns the left plus right insets.
func (in Insets) Horizontal() int { return in.Left + in.Right }

// Vertical returns the top plus bottom insets.
func (in Insets) Vertical() int { return in.Top + in.Bottom }

// Constraints bound the size a child may take. Max values are always
// finite: a terminal has a size.
type Constraints struct{ MinW, MaxW, MinH, MaxH int }

// Tight returns constraints that allow exactly s.
func Tight(s Size) Constraints { return Constraints{s.W, s.W, s.H, s.H} }

// Loose returns constraints that allow anything up to s.
func Loose(s Size) Constraints { return Constraints{0, s.W, 0, s.H} }

// Max returns the largest allowed size.
func (c Constraints) Max() Size { return Size{c.MaxW, c.MaxH} }

// Constrain clamps s into c.
func (c Constraints) Constrain(s Size) Size {
	return Size{clamp(s.W, c.MinW, c.MaxW), clamp(s.H, c.MinH, c.MaxH)}
}

// Deflate shrinks c by insets, as for content inside padding.
func (c Constraints) Deflate(in Insets) Constraints {
	h, v := in.Horizontal(), in.Vertical()
	return Constraints{
		MinW: max(c.MinW-h, 0), MaxW: max(c.MaxW-h, 0),
		MinH: max(c.MinH-v, 0), MaxH: max(c.MaxH-v, 0),
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return min(max(v, lo), hi)
}

// Sizing says how a flex child's main-axis size is chosen.
type Sizing struct {
	kind   uint8
	value  int
	Min    int // lower bound; 0 means none
	MaxLen int // upper bound; 0 means none
}

const (
	sizeAuto = iota
	sizeFixed
	sizeGrow
	sizePercent
)

// Auto sizes the child to its content.
func Auto() Sizing { return Sizing{kind: sizeAuto} }

// Fixed gives the child exactly n cells.
func Fixed(n int) Sizing { return Sizing{kind: sizeFixed, value: max(n, 0)} }

// Grow gives the child a share of the space left after fixed, percent and
// auto children, in proportion to weight.
func Grow(weight int) Sizing { return Sizing{kind: sizeGrow, value: max(weight, 1)} }

// Percent gives the child p percent of the container's main axis.
func Percent(p int) Sizing { return Sizing{kind: sizePercent, value: clamp(p, 0, 100)} }

// Between returns s limited to lo..hi cells (hi 0 means no upper limit).
func (s Sizing) Between(lo, hi int) Sizing {
	s.Min, s.MaxLen = max(lo, 0), max(hi, 0)
	return s
}

// IsGrow reports whether s takes a share of the free space.
func (s Sizing) IsGrow() bool { return s.kind == sizeGrow }

func (s Sizing) bound(n int) int {
	if s.MaxLen > 0 {
		n = min(n, s.MaxLen)
	}
	return max(n, s.Min)
}

// Distribute splits total cells along a main axis between children separated
// by gap cells. measure(i, limit) returns the content size of auto child i
// when given at most limit cells.
//
// Fixed and percent children are sized first, then auto children in order,
// each limited to what remains. The rest is shared among grow children by
// weight, with remainders going to the earliest. When children do not fit,
// later ones get less, down to zero: the result never exceeds total.
func Distribute(total, gap int, items []Sizing, measure func(i, limit int) int) []int {
	sizes := make([]int, len(items))
	if len(items) == 0 {
		return sizes
	}
	total = max(total, 0)
	remaining := max(total-gap*(len(items)-1), 0)
	take := func(i, want int) {
		n := min(max(want, 0), remaining)
		sizes[i] = n
		remaining -= n
	}
	for i, it := range items {
		switch it.kind {
		case sizeFixed:
			take(i, it.bound(it.value))
		case sizePercent:
			take(i, it.bound(total*it.value/100))
		}
	}
	for i, it := range items {
		if it.kind == sizeAuto {
			want := remaining
			if it.MaxLen > 0 {
				want = min(want, it.MaxLen)
			}
			take(i, it.bound(measure(i, want)))
		}
	}
	// Grow children with a minimum get it first, so their minimums hold
	// when space is short.
	weights := 0
	for i, it := range items {
		if it.kind == sizeGrow {
			weights += it.value
			if it.Min > 0 {
				take(i, it.Min)
			}
		}
	}
	if weights == 0 {
		return sizes
	}
	// Share what is left, honoring maximums: a child that hits its maximum
	// drops out and its share is redistributed.
	active := make([]bool, len(items))
	for i, it := range items {
		active[i] = it.kind == sizeGrow
	}
	for remaining > 0 && weights > 0 {
		pool, w := remaining, weights
		given := 0
		for i, it := range items {
			if !active[i] {
				continue
			}
			share := pool * it.value / w
			if it.MaxLen > 0 && sizes[i]+share >= it.MaxLen {
				share = it.MaxLen - sizes[i]
				active[i] = false
				weights -= it.value
			}
			sizes[i] += share
			given += share
		}
		remaining -= given
		if given == 0 {
			// Integer division left a remainder smaller than the weights:
			// hand it out one cell at a time from the front.
			for i := range items {
				if remaining == 0 {
					break
				}
				if active[i] {
					sizes[i]++
					remaining--
				}
			}
			break
		}
	}
	return sizes
}

// Align positions a child of size n within space cells.
type Align uint8

const (
	Start Align = iota
	Center
	End
	Stretch
)

// Offset returns where a child of size n starts within space cells.
func (a Align) Offset(space, n int) int {
	switch a {
	case Center:
		return max((space-n)/2, 0)
	case End:
		return max(space-n, 0)
	}
	return 0
}
