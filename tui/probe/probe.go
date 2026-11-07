// Package probe asks the terminal what it supports when a program starts.
//
// Run writes a batch of queries ending with Primary Device Attributes (DA1).
// Every terminal answers DA1, and terminals answer in order, so the DA1 reply
// marks the end: anything the terminal was going to say about the other
// queries has arrived by then. A terminal that answers nothing is given up on
// after a timeout. Where the terminal is silent, the environment decides.
package probe

import (
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

// DefaultTimeout bounds how long Run waits for a terminal that does not
// answer. Local terminals answer in a few milliseconds.
const DefaultTimeout = 200 * time.Millisecond

// Query is the byte sequence Run writes: mode reports for synchronized output
// (2026) and grapheme clusters (2027), the kitty keyboard flags, the default
// foreground and background colors, XTVERSION, then DA1.
// GraphicsQueryID distinguishes our one-pixel query from unrelated replies.
const GraphicsQueryID = 2147483647
const Query = "\x1b[?2026$p" + "\x1b[?2027$p" + "\x1b[?u" + "\x1b]10;?\x1b\\" + "\x1b]11;?\x1b\\" + "\x1b[>0q" + "\x1b[16t" + "\x1b_Gi=2147483647,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[c"

// Conn is the part of a terminal the probe uses; *term.Terminal has it.
// Read returns 0, nil when the timeout passes without input.
type Conn interface {
	Read(p []byte, timeout time.Duration) (int, error)
	Write(p []byte) (int, error)
}

// Support describes a terminal mode the program may want.
type Support uint8

const (
	// Unsupported: the terminal did not recognize the mode, or it is
	// permanently off.
	Unsupported Support = iota
	// Available: the terminal recognizes the mode and it is off now.
	Available
	// Enabled: the mode is on already, possibly permanently.
	Enabled
)

func support(setting int) Support {
	switch setting {
	case 1, 3:
		return Enabled
	case 2:
		return Available
	}
	return Unsupported
}

// RGB is a color the terminal reported, 8 bits per channel.
type RGB struct{ R, G, B uint8 }

// Result is what the terminal and environment say.
type Result struct {
	Images                cell.ImageProtocol
	CellWidth, CellHeight int
	// Replied is true when the DA1 reply arrived before the timeout.
	Replied bool
	// Name is the XTVERSION reply, for example "kitty(0.40.0)".
	Name string

	SyncOutput    Support // mode 2026
	Graphemes     Support // mode 2027
	KittyKeyboard bool    // answered the kitty keyboard query

	Foreground, Background       RGB
	HasForeground, HasBackground bool

	Colors          cell.ColorMode
	StyledUnderline bool
	Hyperlinks      bool
}

// Options returns renderer options for the detected terminal.
func (r Result) Options() cell.Options {
	return cell.Options{
		Images: r.Images, CellWidth: r.CellWidth, CellHeight: r.CellHeight,
		Colors:          r.Colors,
		SyncOutput:      r.SyncOutput != Unsupported,
		StyledUnderline: r.StyledUnderline,
		Hyperlinks:      r.Hyperlinks,
	}
}

// Method returns the width model to lay text out with. It returns
// uni.Graphemes when mode 2027 is on or available; in the second case the
// caller must turn on term.GraphemeClusters before drawing.
func (r Result) Method() uni.Method {
	if r.Graphemes != Unsupported {
		return uni.Graphemes
	}
	return uni.Codepoints
}

// DarkBackground reports whether the background color is dark, and whether
// the terminal reported it at all.
func (r Result) DarkBackground() (dark, known bool) {
	if !r.HasBackground {
		return false, false
	}
	b := r.Background
	luma := 299*int(b.R) + 587*int(b.G) + 114*int(b.B)
	return luma < 128*1000, true
}

// Run queries the terminal and waits up to timeout for the answers. Replies
// are decoded with d; input that is not a reply, such as keys typed during
// the probe, is returned in order so the caller can handle it. Bytes after
// the DA1 reply stay in d. getenv is usually os.Getenv.
//
// Run must be called before anything else reads from the terminal.
func Run(c Conn, d *input.Decoder, timeout time.Duration, getenv func(string) string) (Result, []input.Event, error) {
	var r Result
	var other []input.Event
	if _, err := c.Write([]byte(Query)); err != nil {
		return r, nil, err
	}
	emit := func(e input.Event) {
		if r.Replied {
			other = append(other, e) // after the sentinel: the caller's input
			return
		}
		switch e := e.(type) {
		case input.DeviceAttributes:
			for _, attr := range e.Params {
				if attr == 4 && r.Images != cell.ImageKitty {
					r.Images = cell.ImageSixel
				}
			}
			r.Replied = true
		case input.GraphicsReply:
			if e.ID == GraphicsQueryID {
				if e.Message == "OK" {
					r.Images = cell.ImageKitty
				}
			} else {
				other = append(other, e)
			}
		case input.CellSize:
			r.CellWidth, r.CellHeight = e.Width, e.Height
		case input.ModeReport:
			switch e.Mode {
			case 2026:
				r.SyncOutput = support(e.Setting)
			case 2027:
				r.Graphemes = support(e.Setting)
			default:
				other = append(other, e)
			}
		case input.KeyboardFlags:
			r.KittyKeyboard = true
		case input.ColorReport:
			rgb := RGB{uint8(e.R >> 8), uint8(e.G >> 8), uint8(e.B >> 8)}
			switch e.Index {
			case 10:
				r.Foreground, r.HasForeground = rgb, true
			case 11:
				r.Background, r.HasBackground = rgb, true
			default:
				other = append(other, e)
			}
		case input.TerminalVersion:
			r.Name = e.Name
		default:
			other = append(other, e)
		}
	}

	buf := make([]byte, 1024)
	deadline := time.Now().Add(timeout)
	for !r.Replied {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		n, err := c.Read(buf, remaining)
		if err != nil {
			return r, other, err
		}
		d.Feed(buf[:n], emit)
	}
	r.detect(getenv)
	if r.CellWidth <= 0 || r.CellHeight <= 0 {
		if pixels, ok := c.(interface{ CellPixels() (int, int) }); ok {
			r.CellWidth, r.CellHeight = pixels.CellPixels()
		}
	}
	if r.CellWidth <= 0 || r.CellHeight <= 0 {
		r.CellWidth, r.CellHeight = 10, 20
	}
	return r, other, nil
}

// terminals lists what known terminals support, matched by the start of
// their lowercased XTVERSION reply or TERM_PROGRAM value.
var terminals = []struct {
	prefix                           string
	truecolor, underline, hyperlinks bool
}{
	{"kitty", true, true, true},
	{"wezterm", true, true, true},
	{"ghostty", true, true, true},
	{"foot", true, true, true},
	{"contour", true, true, true},
	{"iterm2", true, true, true},
	{"iterm.app", true, true, true},
	{"vscode", true, true, true},
	{"xterm(", true, false, false},
}

func (r *Result) detect(getenv func(string) string) {
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	if r.Images == cell.ImageNone && (program == "iterm.app" || strings.HasPrefix(program, "wezterm")) {
		r.Images = cell.ImageITerm
	}
	r.Colors = cell.Color256
	termName := getenv("TERM")
	switch {
	case strings.HasSuffix(termName, "-direct"), strings.Contains(termName, "truecolor"):
		r.Colors = cell.ColorTrue
	case termName == "linux":
		r.Colors = cell.Color16
	}
	if ct := getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		r.Colors = cell.ColorTrue
	}

	for _, name := range []string{r.Name, getenv("TERM_PROGRAM")} {
		lower := strings.ToLower(name)
		for _, t := range terminals {
			if lower != "" && strings.HasPrefix(lower, t.prefix) {
				if t.truecolor {
					r.Colors = cell.ColorTrue
				}
				r.StyledUnderline = r.StyledUnderline || t.underline
				r.Hyperlinks = r.Hyperlinks || t.hyperlinks
			}
		}
	}
	// VTE (GNOME Terminal and others) does not answer XTVERSION but exports
	// its version: 0.50 added OSC 8 links, 0.52 styled underlines.
	if v, err := strconv.Atoi(getenv("VTE_VERSION")); err == nil {
		r.Colors = cell.ColorTrue
		r.Hyperlinks = r.Hyperlinks || v >= 5000
		r.StyledUnderline = r.StyledUnderline || v >= 5200
	}
	if getenv("WT_SESSION") != "" { // Windows Terminal
		r.Colors = cell.ColorTrue
		r.Hyperlinks = true
	}
	// A terminal that answers the newer queries is modern enough for RGB.
	if r.Colors == cell.Color256 && (r.KittyKeyboard || r.SyncOutput != Unsupported || r.Graphemes != Unsupported) {
		r.Colors = cell.ColorTrue
	}
	if termName == "dumb" || getenv("NO_COLOR") != "" { // https://no-color.org
		r.Colors = cell.ColorNone
	}
}
