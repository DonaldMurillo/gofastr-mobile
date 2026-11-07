package layout

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestDistribute(t *testing.T) {
	content := func(sizes ...int) func(int, int) int {
		return func(i, limit int) int { return min(sizes[i], limit) }
	}
	tests := []struct {
		name    string
		total   int
		gap     int
		items   []Sizing
		content []int
		want    []int
	}{
		{"fixed and grow", 20, 0, []Sizing{Fixed(5), Grow(1)}, []int{0, 0}, []int{5, 15}},
		{"weights", 30, 0, []Sizing{Grow(1), Grow(2)}, []int{0, 0}, []int{10, 20}},
		{"remainder goes first", 10, 0, []Sizing{Grow(1), Grow(1), Grow(1)}, []int{0, 0, 0}, []int{4, 3, 3}},
		{"gaps", 12, 1, []Sizing{Grow(1), Grow(1), Grow(1)}, []int{0, 0, 0}, []int{4, 3, 3}},
		{"auto uses content", 20, 0, []Sizing{Auto(), Grow(1)}, []int{7, 0}, []int{7, 13}},
		{"percent", 40, 0, []Sizing{Percent(25), Grow(1)}, []int{0, 0}, []int{10, 30}},
		{"overflow clips later children", 10, 0, []Sizing{Fixed(6), Fixed(6), Grow(1)}, []int{0, 0, 0}, []int{6, 4, 0}},
		{"grow maximum redistributes", 30, 0, []Sizing{Grow(1).Between(0, 5), Grow(1)}, []int{0, 0}, []int{5, 25}},
		{"grow minimum holds when short", 10, 0, []Sizing{Fixed(8), Grow(1).Between(4, 0)}, []int{0, 0}, []int{8, 2}},
		{"auto maximum", 20, 0, []Sizing{Auto().Between(0, 3), Grow(1)}, []int{9, 0}, []int{3, 17}},
		{"empty", 10, 1, nil, nil, []int{}},
		{"zero space", 0, 2, []Sizing{Fixed(3), Grow(1)}, []int{0, 0}, []int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Distribute(tt.total, tt.gap, tt.items, content(tt.content...))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDistributeNeverOverflows checks the invariant layout relies on: sizes
// are never negative and, with gaps, never exceed the total.
func TestDistributeNeverOverflows(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 20000; trial++ {
		n := rng.IntN(6)
		items := make([]Sizing, n)
		content := make([]int, n)
		for i := range items {
			switch rng.IntN(4) {
			case 0:
				items[i] = Fixed(rng.IntN(30))
			case 1:
				items[i] = Grow(1 + rng.IntN(4))
			case 2:
				items[i] = Percent(rng.IntN(101))
			default:
				items[i] = Auto()
			}
			if rng.IntN(3) == 0 {
				items[i] = items[i].Between(rng.IntN(10), rng.IntN(20))
			}
			content[i] = rng.IntN(40)
		}
		total, gap := rng.IntN(80), rng.IntN(3)
		sizes := Distribute(total, gap, items, func(i, limit int) int { return min(content[i], limit) })
		sum := 0
		grows := false
		for i, s := range sizes {
			if s < 0 {
				t.Fatalf("negative size %v for %v", sizes, items)
			}
			sum += s
			grows = grows || items[i].IsGrow()
		}
		avail := max(total-gap*max(n-1, 0), 0)
		if sum > avail {
			t.Fatalf("sizes %v sum to %d, more than %d (items %+v)", sizes, sum, avail, items)
		}
		capped := false
		for _, it := range items {
			capped = capped || it.IsGrow() && it.MaxLen > 0
		}
		if grows && !capped && sum != avail {
			t.Fatalf("grow children left space: %v sum %d of %d (items %+v)", sizes, sum, avail, items)
		}
	}
}

func TestRect(t *testing.T) {
	r := Rect{2, 3, 10, 5}
	if !r.Contains(Point{2, 3}) || r.Contains(Point{12, 3}) || r.Contains(Point{2, 8}) {
		t.Error("Contains")
	}
	if got := r.Intersect(Rect{8, 0, 10, 4}); got != (Rect{8, 3, 4, 1}) {
		t.Errorf("Intersect = %+v", got)
	}
	if got := r.Intersect(Rect{20, 20, 1, 1}); !got.Empty() {
		t.Errorf("disjoint Intersect = %+v", got)
	}
	if got := r.Inset(Insets{1, 2, 1, 2}); got != (Rect{4, 4, 6, 3}) {
		t.Errorf("Inset = %+v", got)
	}
	if got := (Rect{0, 0, 2, 2}).Inset(Uniform(3)); got.W != 0 || got.H != 0 {
		t.Errorf("Inset past zero = %+v", got)
	}
}

func TestConstraints(t *testing.T) {
	c := Constraints{2, 10, 0, 4}
	if got := c.Constrain(Size{20, 1}); got != (Size{10, 1}) {
		t.Errorf("Constrain = %+v", got)
	}
	if got := c.Deflate(Uniform(1)); got != (Constraints{0, 8, 0, 2}) {
		t.Errorf("Deflate = %+v", got)
	}
	if Center.Offset(10, 4) != 3 || End.Offset(10, 4) != 6 || Start.Offset(10, 4) != 0 || Center.Offset(3, 5) != 0 {
		t.Error("Align.Offset")
	}
}
