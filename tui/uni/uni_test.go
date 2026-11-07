package uni

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// TestGraphemeBreakConformance runs the official UAX #29 test file.
func TestGraphemeBreakConformance(t *testing.T) {
	f, err := os.Open("testdata/GraphemeBreakTest.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	cases := 0
	for line := 1; sc.Scan(); line++ {
		spec, comment, _ := strings.Cut(sc.Text(), "#")
		if strings.TrimSpace(spec) == "" {
			continue
		}
		var want []string
		var current strings.Builder
		var input strings.Builder
		for _, field := range strings.Fields(spec) {
			switch field {
			case "÷":
				if current.Len() > 0 {
					want = append(want, current.String())
					current.Reset()
				}
			case "×":
			default:
				cp, err := strconv.ParseUint(field, 16, 32)
				if err != nil {
					t.Fatalf("line %d: %v", line, err)
				}
				current.WriteRune(rune(cp))
				input.WriteRune(rune(cp))
			}
		}
		var got []string
		for s := input.String(); s != ""; {
			cluster, rest := FirstGrapheme(s)
			got = append(got, cluster)
			s = rest
		}
		if !equal(got, want) {
			t.Errorf("line %d: got %s, want %s\n  %s", line, hex(got), hex(want), strings.TrimSpace(comment))
		}
		var viaNext []string
		for unit := range Graphemes.Units(input.String()) {
			viaNext = append(viaNext, unit)
		}
		if !equal(viaNext, want) {
			t.Errorf("line %d: Graphemes.Units gives %s, want %s", line, hex(viaNext), hex(want))
		}
		cases++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if cases < 700 {
		t.Fatalf("only %d cases parsed", cases)
	}
}

func TestWidth(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		codepoints int
		graphemes  int
	}{
		{"ascii", "hello", 5, 5},
		{"empty", "", 0, 0},
		{"han", "界", 2, 2},
		{"hiragana", "あい", 4, 4},
		{"fullwidth", "Ａ", 2, 2},
		{"halfwidth katakana", "ｱ", 1, 1},
		{"combining acute", "é", 1, 1},
		{"precomposed", "é", 1, 1},
		{"emoji", "🙂", 2, 2},
		{"skin tone", "👍🏽", 4, 2},
		{"zwj family", "👨‍👩‍👧", 6, 2},
		{"flag", "🇯🇵", 2, 2},
		{"lone regional indicator", "🇯", 1, 1},
		{"heart with vs16", "❤️", 1, 2},
		{"heart alone", "❤", 1, 1},
		{"keycap", "1️⃣", 1, 2},
		{"emoji with vs15", "🙂︎", 2, 1},
		{"hangul syllable", "한", 2, 2},
		{"hangul jamo", "각", 2, 2},
		{"devanagari conjunct", "क्ष", 2, 1},
		{"soft hyphen", "­", 1, 1},
		{"zero width space", "a​b", 2, 2},
		{"tab", "\t", 0, 0},
		{"cr lf", "\r\n", 0, 0},
		{"leading combining mark", "́a", 1, 1},
		{"arabic number sign", "؀١", 2, 1},
		{"thai spacing vowel", "กำ", 2, 1}, // one cluster: the widest code point counts
		{"invalid utf-8", "\xff", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Codepoints.Width(tt.s); got != tt.codepoints {
				t.Errorf("Codepoints.Width(%q) = %d, want %d", tt.s, got, tt.codepoints)
			}
			if got := Graphemes.Width(tt.s); got != tt.graphemes {
				t.Errorf("Graphemes.Width(%q) = %d, want %d", tt.s, got, tt.graphemes)
			}
		})
	}
}

func TestCodepointUnits(t *testing.T) {
	var units []string
	var widths []int
	for unit, w := range Codepoints.Units("é👨‍👩́x\r\n") {
		units = append(units, unit)
		widths = append(widths, w)
	}
	want := []string{"é", "👨‍", "👩́", "x", "\r", "\n"}
	if !equal(units, want) {
		t.Fatalf("units %s, want %s", hex(units), hex(want))
	}
	if !equalInts(widths, []int{1, 2, 2, 1, 0, 0}) {
		t.Fatalf("widths %v", widths)
	}
}

// TestAgreesWithStandardLibrary cross-checks the generated table against the
// unicode package, which is built from the same Unicode version.
func TestAgreesWithStandardLibrary(t *testing.T) {
	if unicode.Version != UnicodeVersion {
		t.Skipf("unicode package is %s, tables are %s; run go generate", unicode.Version, UnicodeVersion)
	}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		w := RuneWidth(r)
		switch {
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cc) && w != 0:
			t.Fatalf("%U is a mark or control with width %d", r, w)
		case unicode.In(r, unicode.Han, unicode.Hiragana) && unicode.IsLetter(r) && w != 2:
			t.Fatalf("%U is a Han or Hiragana letter with width %d", r, w)
		case unicode.IsLetter(r) && r < 0x1100 && w != 1:
			t.Fatalf("%U is an alphabetic letter with width %d", r, w)
		}
	}
}

// FuzzUnits checks that both methods split any input into nonempty units that
// rebuild it exactly, with widths that add up to Width.
func FuzzUnits(f *testing.F) {
	for _, s := range []string{"hello", "👨‍👩‍👧", "क्ष", "🇯🇵🇯", "\r\n\r", "é́", "\xff\xfe", "한ᆨ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, m := range []Method{Codepoints, Graphemes} {
			var rebuilt strings.Builder
			total := 0
			for unit, w := range m.Units(s) {
				if unit == "" {
					t.Fatalf("%v: empty unit in %q", m, s)
				}
				if w < 0 || w > 2 {
					t.Fatalf("%v: width %d for %q", m, w, unit)
				}
				rebuilt.WriteString(unit)
				total += w
			}
			if rebuilt.String() != s {
				t.Fatalf("%v: units rebuild %q, want %q", m, rebuilt.String(), s)
			}
			if got := m.Width(s); got != total {
				t.Fatalf("%v: Width = %d, units sum to %d", m, got, total)
			}
		}
		if utf8.ValidString(s) {
			if cluster, rest := FirstGrapheme(s); s != "" && (cluster == "" || cluster+rest != s) {
				t.Fatalf("FirstGrapheme(%q) = %q, %q", s, cluster, rest)
			}
		}
	})
}

func BenchmarkWidthASCII(b *testing.B) {
	s := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 4)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		Graphemes.Width(s)
	}
}

func BenchmarkWidthMixed(b *testing.B) {
	s := strings.Repeat("héllo 界 👍🏽 🇯🇵 क्ष ", 8)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		Graphemes.Width(s)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hex(clusters []string) string {
	var b strings.Builder
	for i, c := range clusters {
		if i > 0 {
			b.WriteString(" | ")
		}
		for j, r := range c {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.FormatInt(int64(r), 16))
		}
	}
	return "[" + b.String() + "]"
}
