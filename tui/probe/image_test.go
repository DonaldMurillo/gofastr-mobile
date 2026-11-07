package probe

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
)

func TestImageProbeProtocols(t *testing.T) {
	for _, test := range []struct {
		name, reply, program string
		protocol             cell.ImageProtocol
	}{
		{"kitty", "\x1b_Gi=2147483647;OK\x1b\\\x1b[?62;4c", "iTerm.app", cell.ImageKitty},
		{"sixel", "\x1b[?62;4;22c", "", cell.ImageSixel},
		{"iterm", "\x1b[?62c", "iTerm.app", cell.ImageITerm},
		{"wezterm", "\x1b[?62c", "WezTerm", cell.ImageITerm},
		{"none", "\x1b[?62c", "Apple_Terminal", cell.ImageNone},
		{"error", "\x1b_Gi=2147483647;EINVAL:unsupported\x1b\\\x1b[?62c", "", cell.ImageNone},
		{"wrong id", "\x1b_Gi=32;OK\x1b\\\x1b[?62c", "", cell.ImageNone},
		{"after sentinel", "\x1b[?62c\x1b_Gi=2147483647;OK\x1b\\", "", cell.ImageNone},
		{"silent", "", "", cell.ImageNone},
		{"silent iterm", "", "iTerm.app", cell.ImageITerm},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &script{}
			if test.reply != "" {
				conn.chunks = []string{test.reply}
			}
			var decoder input.Decoder
			result, _, err := Run(conn, &decoder, time.Millisecond, env(map[string]string{"TERM_PROGRAM": test.program}))
			if err != nil || result.Images != test.protocol || result.Options().Images != test.protocol {
				t.Fatalf("result %+v error %v", result, err)
			}
			if result.CellWidth != 10 || result.CellHeight != 20 {
				t.Fatalf("fallback pixel size %+v", result)
			}
			if !strings.HasSuffix(conn.written.String(), "\x1b[c") || !strings.Contains(conn.written.String(), "\x1b[16t\x1b_Gi=2147483647,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\") {
				t.Fatalf("batch %q", conn.written.String())
			}
		})
	}
}

type pixelScript struct {
	script
	width, height int
}

func (connection *pixelScript) CellPixels() (int, int) { return connection.width, connection.height }

func TestImageProbeCellPixels(t *testing.T) {
	for _, test := range []struct {
		name, reply                          string
		width, height, wantWidth, wantHeight int
	}{
		{"query wins", "\x1b[6;18;9t\x1b[?62c", 12, 24, 9, 18},
		{"kernel", "\x1b[?62c", 12, 24, 12, 24},
		{"unknown", "\x1b[?62c", 0, 0, 10, 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &pixelScript{script: script{chunks: []string{test.reply}}, width: test.width, height: test.height}
			var decoder input.Decoder
			result, _, err := Run(conn, &decoder, time.Second, env(nil))
			if err != nil || result.CellWidth != test.wantWidth || result.CellHeight != test.wantHeight {
				t.Fatalf("result %+v error %v", result, err)
			}
		})
	}
}
