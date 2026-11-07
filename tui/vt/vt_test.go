package vt

import "testing"

func TestRepliesToQueries(t *testing.T) {
	s := New(10, 2)
	s.Write([]byte("\x1b[?2026h\x1b[?2026$p\x1b[?2027$p\x1b[?9999$p\x1b[c"))
	want := "\x1b[?2026;1$y\x1b[?2027;2$y\x1b[?9999;0$y\x1b[?62;22c"
	if string(s.Replies) != want {
		t.Fatalf("replies %q, want %q", s.Replies, want)
	}
	if len(s.Errors) > 0 {
		t.Fatalf("errors %q", s.Errors)
	}
}

func TestGraphemeClusterMode(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		cells  []string // cell texts from column 0, "" for a continuation
		cursor int
	}{
		{"flag", "🇯🇵x", []string{"🇯🇵", "", "x"}, 3},
		{"zwj family", "👨‍👩‍👧x", []string{"👨‍👩‍👧", "", "x"}, 3},
		{"vs16 widens", "❤️x", []string{"❤️", "", "x"}, 3},
		{"combining", "éx", []string{"é", "x"}, 2},
		{"three regional indicators", "🇯🇵🇯", []string{"🇯🇵", "", "🇯"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(10, 1)
			s.Write([]byte("\x1b[?2027h" + tt.text))
			for x, want := range tt.cells {
				if got := s.Cell(x, 0).Text; got != want {
					t.Errorf("cell %d = %q, want %q", x, got, want)
				}
			}
			if s.X != tt.cursor {
				t.Errorf("cursor at %d, want %d", s.X, tt.cursor)
			}
			if len(s.Errors) > 0 {
				t.Errorf("errors %q", s.Errors)
			}
		})
	}
}

func TestCodepointModeAdvancesPerCodepoint(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("👨‍👩x🇯🇵"))
	if s.X != 7 || s.Cell(0, 0).Text != "👨‍" || s.Cell(2, 0).Text != "👩" || s.Cell(4, 0).Text != "x" {
		t.Fatalf("line %q, cursor %d", s.Line(0), s.X)
	}
}

func TestClusterDoesNotJoinAcrossCursorMoves(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("\x1b[?2027h🇯\x1b[1;6H🇵"))
	if s.Cell(0, 0).Text != "🇯" || s.Cell(5, 0).Text != "🇵" {
		t.Fatalf("line %q", s.Line(0))
	}
}

func TestClusterDoesNotJoinAcrossExplicitPosition(t *testing.T) {
	for _, position := range []string{"\x1b[1;2H", "\x1b[2G", "\x1b[D\x1b[C", "\r\x1b[C"} {
		t.Run(position, func(t *testing.T) {
			s := New(10, 1)
			s.Write([]byte("\x1b[?2027h🇯" + position + "🇵"))
			if s.Cell(0, 0).Text != "🇯" || s.Cell(1, 0).Text != "🇵" || len(s.Errors) != 0 {
				t.Fatalf("explicit position joined cells: %q, errors %q", s.Line(0), s.Errors)
			}
		})
	}
}

func TestWideningClusterRepairsOverlappedWideCell(t *testing.T) {
	s := New(6, 1)
	s.Write([]byte("\x1b[?2027h\x1b[1;2H界\x1b[H🇯🇵\x1b[1;3Hx"))
	if s.Cell(0, 0).Text != "🇯🇵" || s.Cell(1, 0).Width != 0 || s.Cell(2, 0).Text != "x" {
		t.Fatalf("overlap damaged flag: %q", s.Line(0))
	}
}

func TestUnansweredQueriesAreIgnored(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("\x1b[?u\x1b]10;?\x1b\\\x1b]11;?\x07\x1b[>0q"))
	if len(s.Errors) > 0 || len(s.Replies) > 0 {
		t.Fatalf("errors %q, replies %q", s.Errors, s.Replies)
	}
}
