package input

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// maxSequence bounds one escape sequence. Longer sequences are dropped.
	maxSequence = 4096
	// DefaultMaxPaste bounds bracketed paste text kept by a Decoder.
	DefaultMaxPaste = 8 << 20
)

var pasteEnd = []byte("\x1b[201~")

// Decoder turns terminal input bytes into events. The zero value is ready to
// use. A Decoder is not safe for concurrent use.
//
// A lone ESC byte is ambiguous: it is the Escape key or the start of a
// sequence whose remaining bytes have not arrived yet. Feed holds such
// incomplete input. When Pending reports true and no further bytes arrive
// within a short timeout (about 30ms), call Flush to decode what is held.
type Decoder struct {
	// MaxPaste caps paste text in bytes. Zero means DefaultMaxPaste.
	MaxPaste int

	buf       []byte
	pasting   bool
	paste     []byte
	truncated bool
}

// Feed decodes p, calling emit for each complete event, and holds back any
// incomplete trailing sequence for the next call.
func (d *Decoder) Feed(p []byte, emit func(Event)) {
	d.buf = append(d.buf, p...)
	d.drain(false, emit)
}

// Pending reports whether the decoder holds an incomplete sequence that a
// timeout should resolve with Flush. It is false while a paste is open:
// pastes end only on their end marker.
func (d *Decoder) Pending() bool { return !d.pasting && len(d.buf) > 0 }

// Flush decodes held input as if no more bytes will follow it: a lone ESC
// becomes the Escape key and ESC before another key becomes alt+that key.
func (d *Decoder) Flush(emit func(Event)) {
	if d.pasting {
		return
	}
	d.drain(true, emit)
}

func (d *Decoder) drain(force bool, emit func(Event)) {
	i := 0
	for i < len(d.buf) {
		if d.pasting {
			n, done := d.pasteChunk(d.buf[i:])
			i += n
			if !done {
				break
			}
			text := strings.ReplaceAll(string(d.paste), "\r\n", "\n")
			text = strings.ReplaceAll(text, "\r", "\n")
			emit(Paste{Text: text, Truncated: d.truncated})
			d.pasting, d.paste, d.truncated = false, nil, false
			continue
		}
		event, n := d.parse(d.buf[i:], force)
		if n == 0 {
			break
		}
		i += n
		if event != nil {
			emit(event)
		}
	}
	rest := copy(d.buf, d.buf[i:])
	d.buf = d.buf[:rest]
	if cap(d.buf) > 64<<10 && rest < 1024 {
		d.buf = append([]byte(nil), d.buf...)
	}
}

// pasteChunk consumes paste text up to and including the end marker. It holds
// back a trailing partial marker so a marker split across reads is found.
func (d *Decoder) pasteChunk(b []byte) (consumed int, done bool) {
	if at := bytes.Index(b, pasteEnd); at >= 0 {
		d.addPaste(b[:at])
		return at + len(pasteEnd), true
	}
	keep := 0
	for k := min(len(pasteEnd)-1, len(b)); k > 0; k-- {
		if bytes.HasSuffix(b, pasteEnd[:k]) {
			keep = k
			break
		}
	}
	d.addPaste(b[:len(b)-keep])
	return len(b) - keep, false
}

func (d *Decoder) addPaste(b []byte) {
	limit := d.MaxPaste
	if limit <= 0 {
		limit = DefaultMaxPaste
	}
	room := limit - len(d.paste)
	if len(b) > room {
		b = b[:max(room, 0)]
		// Do not keep half of a UTF-8 sequence at the cut.
		if start := lastRuneStart(b); start >= 0 && !utf8.FullRune(b[start:]) {
			b = b[:start]
		}
		d.truncated = true
	}
	d.paste = append(d.paste, b...)
}

func lastRuneStart(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			return i
		}
	}
	return -1
}

// parse decodes one event at the start of b, returning the bytes consumed.
// n == 0 means b is an incomplete prefix; with force it is never 0.
// A nil event with n > 0 means bytes were consumed without an event.
func (d *Decoder) parse(b []byte, force bool) (Event, int) {
	c := b[0]
	switch {
	case c == 0x1b:
		return d.parseEscape(b, force)
	case c < 0x20 || c == 0x7f:
		return controlKey(c), 1
	case c < 0x80:
		return Key{Code: KeyRune, Rune: rune(c)}, 1
	}
	if !utf8.FullRune(b) {
		if !force {
			return nil, 0
		}
		return nil, 1
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n == 1 || r >= 0x80 && r <= 0x9f {
		return nil, n // invalid UTF-8 or a C1 control: never typed text
	}
	return Key{Code: KeyRune, Rune: r}, n
}

func controlKey(c byte) Key {
	switch c {
	case 0x0d:
		return Key{Code: KeyEnter}
	case 0x09:
		return Key{Code: KeyTab}
	case 0x7f:
		return Key{Code: KeyBackspace}
	case 0x08:
		// Most terminals send 0x08 for ctrl+backspace (and for ctrl+h).
		return Key{Code: KeyBackspace, Mods: ModCtrl}
	case 0x00:
		return Key{Code: KeyRune, Rune: ' ', Mods: ModCtrl}
	case 0x1c, 0x1d, 0x1e, 0x1f:
		return Key{Code: KeyRune, Rune: rune("\\]^_"[c-0x1c]), Mods: ModCtrl}
	}
	return Key{Code: KeyRune, Rune: rune('a' + c - 1), Mods: ModCtrl}
}

func (d *Decoder) parseEscape(b []byte, force bool) (Event, int) {
	if len(b) == 1 {
		if force {
			return Key{Code: KeyEscape}, 1
		}
		return nil, 0
	}
	var (
		event Event
		n     int
	)
	switch b[1] {
	case '[':
		event, n = d.parseCSI(b)
	case 'O':
		event, n = parseSS3(b)
	case ']', 'P', '_', '^', 'X':
		event, n = parseString(b)
	case 0x1b:
		return d.parseDoubleEscape(b, force)
	default:
		return d.alt(b, force)
	}
	if n == 0 && force {
		return d.alt(b, true)
	}
	return event, n
}

// alt decodes b[1:] as a key and adds the alt modifier: ESC x is alt+x.
func (d *Decoder) alt(b []byte, force bool) (Event, int) {
	event, n := d.parse(b[1:], force)
	if n == 0 {
		return nil, 0
	}
	if key, ok := event.(Key); ok {
		key.Mods |= ModAlt
		return key, n + 1
	}
	return Key{Code: KeyEscape}, 1
}

// parseDoubleEscape handles ESC ESC, which some terminals send for alt+esc
// and for alt+arrow (ESC ESC [ A).
func (d *Decoder) parseDoubleEscape(b []byte, force bool) (Event, int) {
	if len(b) == 2 {
		if force {
			return Key{Code: KeyEscape, Mods: ModAlt}, 2
		}
		return nil, 0
	}
	if b[2] == '[' || b[2] == 'O' {
		event, n := d.parseEscape(b[1:], force)
		if n == 0 {
			return nil, 0
		}
		if key, ok := event.(Key); ok {
			key.Mods |= ModAlt
			return key, n + 1
		}
		return Key{Code: KeyEscape}, 1
	}
	return Key{Code: KeyEscape, Mods: ModAlt}, 2
}

func parseSS3(b []byte) (Event, int) {
	if len(b) < 3 {
		return nil, 0
	}
	switch c := b[2]; c {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return Key{Code: cursorKey(c)}, 3
	case 'P', 'Q', 'R', 'S':
		return Key{Code: KeyF1 + KeyCode(c-'P')}, 3
	case 'M':
		return Key{Code: KeyEnter}, 3
	}
	return nil, 3
}

func cursorKey(c byte) KeyCode {
	switch c {
	case 'A':
		return KeyUp
	case 'B':
		return KeyDown
	case 'C':
		return KeyRight
	case 'D':
		return KeyLeft
	case 'H':
		return KeyHome
	}
	return KeyEnd
}

// parseString handles OSC, DCS, APC, PM and SOS: ESC x ... (BEL | ESC \).
func parseString(b []byte) (Event, int) {
	kind := b[1]
	for i := 2; i < len(b); i++ {
		if i > maxSequence {
			return nil, i
		}
		switch b[i] {
		case 0x07:
			if kind == ']' {
				return oscEvent(b[2:i]), i + 1
			}
		case 0x1b:
			if i+1 == len(b) {
				return nil, 0
			}
			if b[i+1] != '\\' {
				return nil, i // malformed: drop up to the new sequence
			}
			body := b[2:i]
			switch kind {
			case ']':
				return oscEvent(body), i + 2
			case 'P':
				return dcsEvent(body), i + 2
			}
			return nil, i + 2
		}
	}
	if len(b) > maxSequence {
		return nil, len(b)
	}
	return nil, 0
}

func oscEvent(body []byte) Event {
	text := string(body)
	index, value, ok := strings.Cut(text, ";")
	if !ok || index != "10" && index != "11" {
		return nil
	}
	spec, ok := strings.CutPrefix(value, "rgb:")
	if !ok {
		return nil
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 3 {
		return nil
	}
	var rgb [3]uint16
	for i, part := range parts {
		if len(part) < 1 || len(part) > 4 {
			return nil
		}
		v, err := strconv.ParseUint(part, 16, 16)
		if err != nil {
			return nil
		}
		scale := uint64(1)<<(4*len(part)) - 1
		rgb[i] = uint16(v * 0xffff / scale)
	}
	n, _ := strconv.Atoi(index)
	return ColorReport{Index: n, R: rgb[0], G: rgb[1], B: rgb[2]}
}

func dcsEvent(body []byte) Event {
	name, ok := bytes.CutPrefix(body, []byte(">|"))
	if !ok {
		return nil
	}
	clean := make([]byte, 0, min(len(name), 128))
	for _, c := range name {
		if c >= 0x20 && c < 0x7f && len(clean) < 128 {
			clean = append(clean, c)
		}
	}
	return TerminalVersion{Name: string(clean)}
}

// csi is one parsed control sequence: ESC [ marker params intermediates final.
type csi struct {
	marker byte
	params [][]int // -1 marks an omitted value
	inter  string
	final  byte
}

func (s csi) param(i, fallback int) int {
	if i < len(s.params) && len(s.params[i]) > 0 && s.params[i][0] >= 0 {
		return s.params[i][0]
	}
	return fallback
}

func (s csi) sub(i, j, fallback int) int {
	if i < len(s.params) && j < len(s.params[i]) && s.params[i][j] >= 0 {
		return s.params[i][j]
	}
	return fallback
}

func (d *Decoder) parseCSI(b []byte) (Event, int) {
	if len(b) < 3 {
		return nil, 0
	}
	if b[2] == 'M' { // legacy X10 mouse: ESC [ M cb cx cy
		if len(b) < 6 {
			return nil, 0
		}
		return x10Mouse(b[3], b[4], b[5]), 6
	}
	i := 2
	start := i
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
		i++
	}
	paramEnd := i
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
		i++
	}
	if i > maxSequence {
		return nil, i
	}
	if i == len(b) {
		if len(b) > maxSequence {
			return nil, len(b)
		}
		return nil, 0
	}
	final := b[i]
	if final < 0x40 || final > 0x7e {
		return nil, i // malformed: drop the prefix, keep the stray byte
	}
	seq := csi{inter: string(b[paramEnd:i]), final: final}
	params := b[start:paramEnd]
	if len(params) > 0 && params[0] >= '<' && params[0] <= '?' {
		seq.marker = params[0]
		params = params[1:]
	}
	var ok bool
	if seq.params, ok = parseParams(params); !ok {
		return nil, i + 1
	}
	return d.csiEvent(seq), i + 1
}

func parseParams(b []byte) ([][]int, bool) {
	if len(b) == 0 {
		return nil, true
	}
	var params [][]int
	for field := range bytes.SplitSeq(b, []byte(";")) {
		if len(params) >= 32 {
			return nil, false
		}
		var values []int
		for part := range bytes.SplitSeq(field, []byte(":")) {
			if len(values) >= 8 {
				return nil, false
			}
			if len(part) == 0 {
				values = append(values, -1)
				continue
			}
			n := 0
			for _, c := range part {
				if c < '0' || c > '9' {
					return nil, false
				}
				if n > 1<<20 {
					return nil, false
				}
				n = n*10 + int(c-'0')
			}
			values = append(values, n)
		}
		params = append(params, values)
	}
	return params, true
}

func (d *Decoder) csiEvent(s csi) Event {
	switch s.marker {
	case '<':
		if (s.final == 'M' || s.final == 'm') && s.inter == "" && len(s.params) == 3 {
			return sgrMouse(s.param(0, 0), s.param(1, 1), s.param(2, 1), s.final == 'm')
		}
		return nil
	case '?':
		switch {
		case s.final == 'c' && s.inter == "":
			values := make([]int, 0, len(s.params))
			for i := range s.params {
				values = append(values, s.param(i, 0))
			}
			return DeviceAttributes{Params: values}
		case s.final == 'y' && s.inter == "$":
			return ModeReport{Mode: s.param(0, 0), Setting: s.param(1, 0)}
		case s.final == 'u' && s.inter == "":
			return KeyboardFlags{Flags: s.param(0, 0)}
		}
		return nil
	case 0:
	default:
		return nil
	}
	if s.inter != "" {
		return nil
	}
	mods := modifiers(s.param(1, 1))
	switch s.final {
	case '~':
		return d.tildeKey(s, mods)
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return Key{Code: cursorKey(s.final), Mods: mods}
	case 'P':
		return Key{Code: KeyF1, Mods: mods}
	case 'Q':
		return Key{Code: KeyF2, Mods: mods}
	case 'S':
		return Key{Code: KeyF4, Mods: mods}
	case 'R':
		if s.param(0, 1) == 1 {
			return Key{Code: KeyF3, Mods: mods}
		}
		return CursorPosition{X: s.param(1, 1) - 1, Y: s.param(0, 1) - 1}
	case 'Z':
		return Key{Code: KeyTab, Mods: ModShift | mods}
	case 'I', 'O':
		if len(s.params) == 0 {
			return Focus{Focused: s.final == 'I'}
		}
	case 'u':
		return kittyKey(s)
	}
	return nil
}

func (d *Decoder) tildeKey(s csi, mods Mods) Event {
	code := s.param(0, 0)
	switch code {
	case 200:
		d.pasting = true
		return nil
	case 27: // xterm modifyOtherKeys: CSI 27 ; mods ; code ~
		return functionalKey(s.param(2, 0), mods)
	}
	if key, ok := tildeKeys[code]; ok {
		return Key{Code: key, Mods: mods}
	}
	return nil
}

var tildeKeys = map[int]KeyCode{
	1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPageUp, 6: KeyPageDown,
	7: KeyHome, 8: KeyEnd, 11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5,
	17: KeyF6, 18: KeyF7, 19: KeyF8, 20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12,
}

// kittyKey decodes CSI code[:alternates] ; mods[:event] ; text u.
func kittyKey(s csi) Event {
	if s.sub(1, 1, 1) == 3 { // release events are not reported
		return nil
	}
	return functionalKey(s.sub(0, 0, 0), modifiers(s.sub(1, 0, 1)))
}

func functionalKey(code int, mods Mods) Event {
	switch code {
	case 13:
		return Key{Code: KeyEnter, Mods: mods}
	case 9:
		return Key{Code: KeyTab, Mods: mods}
	case 27:
		return Key{Code: KeyEscape, Mods: mods}
	case 127, 8:
		return Key{Code: KeyBackspace, Mods: mods}
	}
	// 57344-63743 is the kitty protocol's private range for functional keys
	// (keypad, media, lock keys). They are not reported yet.
	if code < 0x20 || code >= 0x7f && code <= 0x9f || code >= 57344 && code <= 63743 || code > utf8.MaxRune || !utf8.ValidRune(rune(code)) {
		return nil
	}
	return Key{Code: KeyRune, Rune: rune(code), Mods: mods}
}

// modifiers decodes the xterm/kitty modifier parameter: 1 + bit set.
func modifiers(param int) Mods {
	bits := param - 1
	if bits <= 0 {
		return 0
	}
	var mods Mods
	if bits&1 != 0 {
		mods |= ModShift
	}
	if bits&2 != 0 {
		mods |= ModAlt
	}
	if bits&4 != 0 {
		mods |= ModCtrl
	}
	if bits&8 != 0 {
		mods |= ModSuper
	}
	return mods
}

func sgrMouse(cb, x, y int, release bool) Event {
	event := Mouse{X: max(x-1, 0), Y: max(y-1, 0), Mods: mouseMods(cb)}
	low := cb & 3
	switch {
	case cb&64 != 0:
		if release {
			return nil
		}
		event.Button = ButtonWheelUp + Button(low)
		return event
	case cb&128 != 0:
		switch low {
		case 0:
			event.Button = ButtonBack
		case 1:
			event.Button = ButtonForward
		}
	default:
		if low < 3 {
			event.Button = ButtonLeft + Button(low)
		}
	}
	switch {
	case cb&32 != 0 && event.Button == ButtonNone:
		event.Action = MouseMove
	case cb&32 != 0:
		event.Action = MouseDrag
	case release:
		event.Action = MouseRelease
	default:
		event.Action = MousePress
	}
	return event
}

func x10Mouse(cb, cx, cy byte) Event {
	if cb < 32 || cx < 33 || cy < 33 {
		return nil
	}
	code := int(cb - 32)
	if code&64 == 0 && code&32 == 0 && code&3 == 3 {
		return Mouse{X: int(cx) - 33, Y: int(cy) - 33, Action: MouseRelease, Mods: mouseMods(code)}
	}
	return sgrMouse(code, int(cx)-32, int(cy)-32, false)
}

func mouseMods(cb int) Mods {
	var mods Mods
	if cb&4 != 0 {
		mods |= ModShift
	}
	if cb&8 != 0 {
		mods |= ModAlt
	}
	if cb&16 != 0 {
		mods |= ModCtrl
	}
	return mods
}
