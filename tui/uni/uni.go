// Package uni splits text into the units a terminal draws and reports how
// many columns each one takes.
//
// Terminals disagree about width. Most advance the cursor per code point,
// using wcwidth-style rules: a ZWJ emoji sequence takes the sum of its
// parts. Terminals that support mode 2027 treat each grapheme cluster as one
// character. Method selects which model to follow; package probe detects it.
//
// The tables are generated from the Unicode Character Database; see
// UnicodeVersion.
package uni

import (
	"iter"
	"unicode/utf8"
)

//go:generate go run ./internal/gen -version 17.0.0

// Method is a terminal's model of character width.
type Method uint8

const (
	// Codepoints follows terminals that advance per code point. A unit is one
	// code point with nonzero width plus the zero-width code points after it.
	Codepoints Method = iota
	// Graphemes follows terminals with grapheme cluster support (mode 2027).
	// A unit is one extended grapheme cluster (UAX #29).
	Graphemes
)

func (m Method) String() string {
	if m == Graphemes {
		return "graphemes"
	}
	return "codepoints"
}

// Next returns the first unit of s and the number of columns it takes,
// 0, 1 or 2. Width 0 means the unit has nothing to draw on its own: a
// control character, or zero-width code points with no base before them.
// Next returns "", 0 for an empty string.
func (m Method) Next(s string) (unit string, width int) {
	if s == "" {
		return "", 0
	}
	// Fast path: printable ASCII followed by ASCII is always its own unit.
	if c := s[0]; c >= 0x20 && c < 0x7f && (len(s) == 1 || s[1] < 0x80) {
		return s[:1], 1
	}
	if m == Graphemes {
		n := graphemeLen(s)
		return s[:n], clusterWidth(s[:n])
	}
	return codepointUnit(s)
}

// Width returns the number of columns s takes.
func (m Method) Width(s string) int {
	total := 0
	for s != "" {
		unit, w := m.Next(s)
		total += w
		s = s[len(unit):]
	}
	return total
}

// Units iterates over the units of s with their widths.
func (m Method) Units(s string) iter.Seq2[string, int] {
	return func(yield func(string, int) bool) {
		for s != "" {
			unit, w := m.Next(s)
			if !yield(unit, w) {
				return
			}
			s = s[len(unit):]
		}
	}
}

// RuneWidth returns the columns a code point takes on its own: 0 for marks,
// format and control characters, 2 for East Asian wide and full-width
// characters (which include emoji with emoji presentation), otherwise 1.
func RuneWidth(r rune) int {
	return lookup(r).width()
}

// FirstGrapheme splits the first extended grapheme cluster off s.
func FirstGrapheme(s string) (cluster, rest string) {
	if s == "" {
		return "", ""
	}
	n := graphemeLen(s)
	return s[:n], s[n:]
}

func codepointUnit(s string) (string, int) {
	r, n := utf8.DecodeRuneInString(s)
	p := lookup(r)
	width := p.width()
	if p.isControl() { // never combines with what follows (GB4)
		return s[:n], width
	}
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		q := lookup(r)
		if q.width() != 0 || q.isControl() {
			break
		}
		n += size
	}
	return s[:n], width
}

const (
	vs15 = '︎' // text presentation selector
	vs16 = '️' // emoji presentation selector
)

// clusterWidth follows the mode 2027 convention: the widest code point in
// the cluster, with presentation selectors overriding emoji bases and a
// regional indicator pair taking two columns.
func clusterWidth(cluster string) int {
	first, n := utf8.DecodeRuneInString(cluster)
	fp := lookup(first)
	width := fp.width()
	regional := fp.gcb() == gcbRegional
	for _, r := range cluster[n:] {
		switch {
		case r == vs16 && fp.isEmoji():
			return 2
		case r == vs15 && fp.isEmoji():
			return 1
		}
		p := lookup(r)
		if regional && p.gcb() == gcbRegional {
			return 2
		}
		width = max(width, p.width())
	}
	return width
}
