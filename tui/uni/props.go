package uni

// prop packs the properties of a code point. The layout matches
// internal/gen.
type prop uint16

type span struct {
	lo, hi rune
	p      prop
}

const (
	gcbOther = iota
	gcbCR
	gcbLF
	gcbControl
	gcbExtend
	gcbZWJ
	gcbRegional
	gcbPrepend
	gcbSpacingMark
	gcbL
	gcbV
	gcbT
	gcbLV
	gcbLVT
)

const (
	incbNone = iota
	incbLinker
	incbConsonant
	incbExtend
)

const (
	propExtPict = 1 << 4
	propEmoji   = 1 << 9
	defaultProp = prop(1 << 7) // Other, width 1
)

func (p prop) gcb() int      { return int(p & 0x0f) }
func (p prop) extPict() bool { return p&propExtPict != 0 }
func (p prop) incb() int     { return int(p>>5) & 3 }
func (p prop) width() int    { return int(p>>7) & 3 }
func (p prop) isEmoji() bool { return p&propEmoji != 0 }
func (p prop) isControl() bool {
	g := p.gcb()
	return g == gcbControl || g == gcbCR || g == gcbLF
}

// asciiProps covers U+0000..U+007F without a search.
var asciiProps = func() (t [128]prop) {
	for r := range t {
		t[r] = search(rune(r))
	}
	return t
}()

func lookup(r rune) prop {
	if r >= 0 && r < 128 {
		return asciiProps[r]
	}
	return search(r)
}

func search(r rune) prop {
	lo, hi := 0, len(table)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch s := &table[mid]; {
		case r < s.lo:
			hi = mid
		case r > s.hi:
			lo = mid + 1
		default:
			return s.p
		}
	}
	return defaultProp
}
