// Command gen builds the uni package's property table from the Unicode
// Character Database. Run it through go generate in the uni directory.
//
// Files are downloaded once per version into the user cache directory, or
// read from -ucd when given.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Property bit layout. Keep in sync with props.go in package uni.
const (
	gcbMask    = 0x0f
	extPict    = 1 << 4
	incbShift  = 5
	widthShift = 7
	emoji      = 1 << 9
)

var gcbValues = map[string]uint16{
	"CR": 1, "LF": 2, "Control": 3, "Extend": 4, "ZWJ": 5, "Regional_Indicator": 6,
	"Prepend": 7, "SpacingMark": 8, "L": 9, "V": 10, "T": 11, "LV": 12, "LVT": 13,
}

var incbValues = map[string]uint16{"Linker": 1, "Consonant": 2, "Extend": 3}

const maxRune = 0x10ffff

func main() {
	version := flag.String("version", "17.0.0", "Unicode version")
	ucd := flag.String("ucd", "", "directory holding the UCD files (default: download into the user cache)")
	out := flag.String("o", "tables.go", "output file")
	testdata := flag.String("testdata", "testdata", "directory for GraphemeBreakTest.txt")
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("gen: ")

	dir := *ucd
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(cache, "gofastr-tui", "ucd", *version)
	}
	files := map[string]string{
		"GraphemeBreakProperty.txt":  "auxiliary/GraphemeBreakProperty.txt",
		"GraphemeBreakTest.txt":      "auxiliary/GraphemeBreakTest.txt",
		"emoji-data.txt":             "emoji/emoji-data.txt",
		"EastAsianWidth.txt":         "EastAsianWidth.txt",
		"DerivedCoreProperties.txt":  "DerivedCoreProperties.txt",
		"DerivedGeneralCategory.txt": "extracted/DerivedGeneralCategory.txt",
	}
	data := map[string][]byte{}
	for name, remote := range files {
		b, err := load(dir, name, "https://www.unicode.org/Public/"+*version+"/ucd/"+remote)
		if err != nil {
			log.Fatal(err)
		}
		// Most files name the version in their first line; emoji-data.txt
		// has a "# Version: 17.0" line instead.
		header := b[:min(len(b), 1024)]
		if !bytes.Contains(header, []byte("-"+*version+".txt")) && !bytes.Contains(header, []byte("# Version: "+strings.TrimSuffix(*version, ".0")+"\n")) {
			log.Fatalf("%s is not version %s", name, *version)
		}
		data[name] = b
	}

	props := make([]uint16, maxRune+1)
	setWidth := func(r int, w uint16) { props[r] = props[r]&^(3<<widthShift) | w<<widthShift }
	for r := range props {
		setWidth(r, 1)
	}

	// Wide: East_Asian_Width W or F, including the unassigned code points
	// the file header says default to W.
	for _, span := range [][2]int{{0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0xf900, 0xfaff}, {0x20000, 0x2fffd}, {0x30000, 0x3fffd}} {
		for r := span[0]; r <= span[1]; r++ {
			setWidth(r, 2)
		}
	}
	each(data["EastAsianWidth.txt"], func(lo, hi int, fields []string) {
		w := uint16(1)
		if fields[0] == "W" || fields[0] == "F" {
			w = 2
		}
		for r := lo; r <= hi; r++ {
			setWidth(r, w)
		}
	})

	// Zero width: nonspacing and enclosing marks, format and control
	// characters, line and paragraph separators. The soft hyphen is
	// rendered as a hyphen by terminals.
	category := make([]string, maxRune+1)
	each(data["DerivedGeneralCategory.txt"], func(lo, hi int, fields []string) {
		for r := lo; r <= hi; r++ {
			category[r] = fields[0]
			switch fields[0] {
			case "Mn", "Me", "Cf", "Cc", "Zl", "Zp", "Cs":
				if r != 0xad {
					setWidth(r, 0)
				}
			}
		}
	})
	each(data["DerivedCoreProperties.txt"], func(lo, hi int, fields []string) {
		switch {
		case fields[0] == "Default_Ignorable_Code_Point":
			for r := lo; r <= hi; r++ {
				// Hangul fillers are letters with a visible advance.
				if category[r] != "Lo" && r != 0xad {
					setWidth(r, 0)
				}
			}
		case fields[0] == "InCB" && len(fields) > 1:
			v, ok := incbValues[fields[1]]
			if !ok {
				log.Fatalf("unknown InCB value %q", fields[1])
			}
			for r := lo; r <= hi; r++ {
				props[r] |= v << incbShift
			}
		}
	})
	each(data["GraphemeBreakProperty.txt"], func(lo, hi int, fields []string) {
		v, ok := gcbValues[fields[0]]
		if !ok {
			log.Fatalf("unknown Grapheme_Cluster_Break value %q", fields[0])
		}
		for r := lo; r <= hi; r++ {
			props[r] |= v
			switch fields[0] {
			case "V", "T": // Hangul medial vowels and final consonants join the syllable
				setWidth(r, 0)
			case "Prepend": // visible signs such as ARABIC NUMBER SIGN
				if category[r] == "Cf" {
					setWidth(r, 1)
				}
			}
		}
	})
	each(data["emoji-data.txt"], func(lo, hi int, fields []string) {
		var bit uint16
		switch fields[0] {
		case "Extended_Pictographic":
			bit = extPict
		case "Emoji":
			bit = emoji
		default:
			return
		}
		for r := lo; r <= hi; r++ {
			props[r] |= bit
		}
	})

	// Default properties (Other, width 1) are left out; lookups fall back
	// to them between ranges.
	const defaults = 1 << widthShift
	var src bytes.Buffer
	fmt.Fprintf(&src, "// Code generated by internal/gen from the Unicode %s Character Database; DO NOT EDIT.\n", *version)
	fmt.Fprintf(&src, "// Unicode data terms of use: https://www.unicode.org/terms_of_use.html\n\n")
	fmt.Fprintf(&src, "package uni\n\n")
	fmt.Fprintf(&src, "// UnicodeVersion is the Unicode version the tables were built from.\n")
	fmt.Fprintf(&src, "const UnicodeVersion = %q\n\n", *version)
	fmt.Fprintf(&src, "var table = [...]span{\n")
	count := 0
	for r := 0; r <= maxRune; {
		p := props[r]
		end := r
		for end < maxRune && props[end+1] == p {
			end++
		}
		if p != defaults {
			fmt.Fprintf(&src, "{%#x, %#x, %#x},\n", r, end, p)
			count++
		}
		r = end + 1
	}
	fmt.Fprintf(&src, "}\n")
	formatted, err := format.Source(src.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, formatted, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*testdata, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*testdata, "GraphemeBreakTest.txt"), data["GraphemeBreakTest.txt"], 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: %d ranges", *out, count)
}

func load(dir, name, url string) ([]byte, error) {
	path := filepath.Join(dir, name)
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o644)
}

// each calls fn for every data line: "XXXX..YYYY ; Value [; Value] # comment".
func each(data []byte, fn func(lo, hi int, fields []string)) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) < 2 {
			log.Fatalf("malformed line %q", sc.Text())
		}
		first, last, isRange := strings.Cut(strings.TrimSpace(parts[0]), "..")
		lo, err := strconv.ParseUint(first, 16, 32)
		if err != nil {
			log.Fatalf("bad code point in %q", sc.Text())
		}
		hi := lo
		if isRange {
			if hi, err = strconv.ParseUint(last, 16, 32); err != nil {
				log.Fatalf("bad code point in %q", sc.Text())
			}
		}
		fields := make([]string, 0, len(parts)-1)
		for _, f := range parts[1:] {
			fields = append(fields, strings.TrimSpace(f))
		}
		fn(int(lo), int(hi), fields)
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
}
