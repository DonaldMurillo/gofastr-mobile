package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

// host is what the model needs from the app loop; *app.App provides it.
type host interface {
	Post(func())
	Quit()
	Tree() *view.Tree
	After(time.Duration, func()) func()
}

const (
	colProto = iota
	colLocal
	colRemote
	colState
	colPID
	colProcess
)

var columns = []widget.TableColumn{
	colProto:   {Title: "Proto", Width: layout.Fixed(5)},
	colLocal:   {Title: "Local", Width: layout.Auto().Between(12, 46)},
	colRemote:  {Title: "Remote", Width: layout.Grow(1).Between(12, 0)},
	colState:   {Title: "State", Width: layout.Fixed(11)},
	colPID:     {Title: "PID", Width: layout.Fixed(7), Align: layout.End},
	colProcess: {Title: "Process", Width: layout.Grow(1).Between(8, 0)},
}

const historyLength = 240

type model struct {
	host     host
	source   Source
	interval time.Duration
	now      func() time.Time

	all       []Conn
	rows      []Conn // filtered and sorted
	firstSeen map[string]time.Time
	history   []float64
	updated   time.Time
	loads     int
	err       error
	busy      bool
	paused    bool

	filter   string
	sortBy   int
	desc     bool
	selected string // key of the selected row
	index    int    // its position in rows, -1 for none
	help     bool
	notices  widget.Notifications
}

func newModel(source Source, interval time.Duration, now func() time.Time) *model {
	return &model{source: source, interval: interval, now: now, firstSeen: map[string]time.Time{}, sortBy: colProcess, index: -1}
}

// refresh reads the connections on a goroutine and applies them on the
// loop. A refresh that finds the previous one still running is skipped.
func (m *model) refresh() {
	if m.busy {
		return
	}
	m.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conns, err := m.source(ctx)
		cancel()
		m.host.Post(func() { m.apply(conns, err) })
	}()
}

func (m *model) tick() {
	if !m.paused {
		m.refresh()
	}
}

func (m *model) apply(conns []Conn, err error) {
	m.busy = false
	if err != nil {
		if m.err == nil || m.err.Error() != err.Error() {
			m.notify(widget.NoticeError, "Refresh failed", err.Error())
		}
		m.err = err
		return
	}
	if m.err != nil {
		m.notify(widget.NoticeSuccess, "Refreshing again", "")
	}
	m.err = nil
	now := m.now()
	present := make(map[string]bool, len(conns))
	for _, c := range conns {
		k := c.Key()
		present[k] = true
		if _, ok := m.firstSeen[k]; !ok {
			m.firstSeen[k] = now
		}
	}
	for k := range m.firstSeen {
		if !present[k] {
			delete(m.firstSeen, k)
		}
	}
	m.all, m.updated = conns, now
	m.loads++
	m.history = append(m.history, float64(len(conns)))
	if len(m.history) > historyLength {
		m.history = m.history[len(m.history)-historyLength:]
	}
	m.rebuild()
}

func (m *model) notify(level widget.NoticeLevel, title, text string) {
	id := m.notices.Push(level, title, text)
	m.host.After(4*time.Second, func() { m.notices.Dismiss(id) })
}

func (m *model) copied(text string) {
	m.notify(widget.NoticeSuccess, "Copied", fmt.Sprintf("%d characters", utf8.RuneCountInString(text)))
}

func field(c Conn, col int) string {
	switch col {
	case colProto:
		return c.Proto
	case colLocal:
		return c.Local
	case colRemote:
		return c.Remote
	case colState:
		return c.State
	case colPID:
		if c.PID == 0 {
			return ""
		}
		return strconv.Itoa(c.PID)
	default:
		return c.Process
	}
}

func (m *model) matches(c Conn, terms []string) bool {
	text := strings.ToLower(strings.Join([]string{c.Proto, c.Local, c.Remote, c.State, field(c, colPID), c.Process}, " "))
	for _, t := range terms {
		if !strings.Contains(text, t) {
			return false
		}
	}
	return true
}

func (m *model) compare(a, b Conn) int {
	var r int
	if m.sortBy == colPID {
		r = cmp.Compare(a.PID, b.PID)
	} else {
		r = strings.Compare(strings.ToLower(field(a, m.sortBy)), strings.ToLower(field(b, m.sortBy)))
	}
	if m.desc {
		r = -r
	}
	if r == 0 {
		r = strings.Compare(a.Key(), b.Key())
	}
	return r
}

// rebuild filters and sorts, keeping the selected connection selected. If
// it went away, the selection stays at the same position.
func (m *model) rebuild() {
	terms := strings.Fields(strings.ToLower(m.filter))
	m.rows = m.rows[:0]
	for _, c := range m.all {
		if m.matches(c, terms) {
			m.rows = append(m.rows, c)
		}
	}
	slices.SortFunc(m.rows, m.compare)
	at := -1
	if m.selected != "" {
		at = slices.IndexFunc(m.rows, func(c Conn) bool { return c.Key() == m.selected })
	}
	if at < 0 && len(m.rows) > 0 {
		at = min(max(m.index, 0), len(m.rows)-1)
	}
	m.choose(at)
}

func (m *model) choose(i int) {
	m.index, m.selected = i, ""
	if i >= 0 && i < len(m.rows) {
		m.selected = m.rows[i].Key()
	}
}

func (m *model) setFilter(s string) {
	m.filter = s
	m.rebuild()
}

func (m *model) sort(col int) {
	if col == m.sortBy {
		m.desc = !m.desc
	} else {
		m.sortBy, m.desc = col, false
	}
	m.rebuild()
}

func (m *model) focus(key string) {
	tree := m.host.Tree()
	tree.FocusWithin(tree.Find(func(n *view.Node) bool { return n.Key() == key }))
}

// focusIn reports whether the focused node is the keyed element or inside
// it; the table's focus lands on the list it builds.
func (m *model) focusIn(key string) bool {
	for n := m.host.Tree().Focused(); n != nil; n = n.Parent() {
		if n.Key() == key {
			return true
		}
	}
	return false
}

// key handles keys the focused widget left alone.
func (m *model) key(k input.Key) bool {
	if k.Code == input.KeyEscape && k.Mods == 0 {
		switch {
		case m.focusIn("filter"):
			m.focus("table")
		case m.filter != "":
			m.setFilter("")
		default:
			return false
		}
		return true
	}
	if k.Code != input.KeyRune || k.Mods != 0 {
		return false
	}
	switch r := k.Rune; {
	case r == '/':
		m.focus("filter")
	case r == 'q':
		m.host.Quit()
	case r == 'p':
		m.paused = !m.paused
		if m.paused {
			m.notify(widget.NoticeInfo, "Paused", "")
		} else {
			m.notify(widget.NoticeInfo, "Resumed", "")
		}
	case r == 'r':
		if m.busy {
			m.notify(widget.NoticeInfo, "Already refreshing", "")
		} else {
			m.refresh()
		}
	case r == '?':
		m.help = true
	case r >= '1' && r <= '0'+rune(len(columns)):
		m.sort(int(r - '1'))
	default:
		return false
	}
	return true
}

var (
	styleNew     = cell.Style{Fg: cell.Green, Attrs: cell.Bold}
	styleWarn    = cell.Style{Fg: cell.Yellow}
	styleError   = cell.Style{Fg: cell.Red, Attrs: cell.Bold}
	stylePaused  = cell.Style{Fg: cell.Black, Bg: cell.Yellow, Attrs: cell.Bold}
	stateStyles  = map[string]cell.Style{"ESTABLISHED": {Fg: cell.Green}, "LISTEN": {Fg: cell.Cyan}, "TIME_WAIT": {Fg: cell.Yellow}, "CLOSE_WAIT": {Fg: cell.Yellow}, "SYN_SENT": {Fg: cell.Magenta}}
	styleLabel   = cell.Style{Fg: cell.BrightBlack}
	styleProcess = cell.Style{Attrs: cell.Bold}
)

// fresh reports whether c appeared in the latest refresh, after the first.
func (m *model) fresh(c Conn) bool {
	return m.loads > 1 && m.firstSeen[c.Key()].Equal(m.updated)
}

func (m *model) view() view.Element {
	main := widget.Column(
		m.header(),
		widget.Row(
			widget.Styled(" / ", widget.Theme.Muted),
			widget.Grow(1, view.E(widget.Input{
				Value: m.filter, Placeholder: "filter by process, address, port or state",
				OnChange: m.setFilter, OnSubmit: func(string) { m.focus("table") },
			}).WithKey("filter")),
		),
		widget.Grow(1, view.E(widget.Split{Dir: widget.Vertical, Percent: 62, Min: 3, First: m.table(), Second: m.details()})),
		m.footer(),
	)
	return widget.OnKey(m.key, widget.Layers(main, m.notices.Element(), view.If(m.help, m.helpDialog())))
}

func (m *model) header() view.Element {
	spans := []widget.Span{
		{Text: " netmon ", Style: cell.Style{Fg: cell.Black, Bg: cell.Cyan, Attrs: cell.Bold}},
		{Text: fmt.Sprintf("  %d sockets · %d shown", len(m.all), len(m.rows))},
	}
	if !m.updated.IsZero() {
		spans = append(spans, widget.Span{Text: " · updated " + m.updated.Format("15:04:05"), Style: widget.Theme.Muted})
	} else {
		spans = append(spans, widget.Span{Text: " · loading", Style: widget.Theme.Muted})
	}
	if m.paused {
		spans = append(spans, widget.Span{Text: "  "}, widget.Span{Text: " PAUSED ", Style: stylePaused})
	}
	if m.err != nil {
		spans = append(spans, widget.Span{Text: "  " + m.err.Error(), Style: styleError})
	}
	return view.E(widget.Text{Spans: spans, Ellipsis: true})
}

func (m *model) table() view.Element {
	return view.E(widget.Table{
		Columns: columns, Count: len(m.rows),
		Cell: func(row, col int) widget.Span {
			c := m.rows[row]
			sp := widget.Span{Text: field(c, col)}
			switch {
			case col == colState:
				sp.Style = stateStyles[c.State]
			case col == colProcess:
				sp.Style = styleProcess
			}
			if m.fresh(c) && col != colState {
				sp.Style = styleNew
			}
			return sp
		},
		Key:      func(row int) string { return m.rows[row].Key() },
		Selected: m.index,
		OnSelect: m.choose,
		OnActivate: func(row int) {
			m.choose(row)
			m.setFilter(m.rows[row].Process)
		},
		SortBy: m.sortBy, SortDesc: m.desc, OnSort: m.sort,
		AutoFocused: true,
	}).WithKey("table")
}

func kv(k, v string, s cell.Style) view.Element {
	return widget.Line(widget.Span{Text: fmt.Sprintf("%-10s", k), Style: styleLabel}, widget.Span{Text: v, Style: s})
}

func (m *model) details() view.Element {
	var body view.Element
	if m.index < 0 || m.index >= len(m.rows) {
		body = widget.Styled("no connection selected", widget.Theme.Muted)
	} else {
		c := m.rows[m.index]
		process := c.Process
		if process == "" {
			process = "unknown"
		}
		if c.PID != 0 {
			same := 0
			for _, o := range m.all {
				if o.PID == c.PID {
					same++
				}
			}
			process += fmt.Sprintf(" (pid %d) · %d socket", c.PID, same)
			if same != 1 {
				process += "s"
			}
		}
		seen := m.now().Sub(m.firstSeen[c.Key()]).Round(time.Second)
		state := c.State
		if state == "" {
			state = "none (datagram)"
		}
		body = widget.Column(
			kv("Process", process, styleProcess),
			kv("Protocol", c.Proto, cell.Style{}),
			kv("Local", c.Local, cell.Style{}),
			kv("Remote", c.Remote, cell.Style{}),
			kv("State", state, stateStyles[c.State]),
			kv("Seen for", seen.String(), cell.Style{}),
		)
	}
	counts := map[string]int{}
	for _, c := range m.all {
		counts[c.State]++
	}
	var states []widget.Span
	for _, s := range []string{"ESTABLISHED", "LISTEN", "TIME_WAIT", "CLOSE_WAIT"} {
		if counts[s] > 0 {
			states = append(states, widget.Span{Text: fmt.Sprintf("%s\u00a0%d  ", strings.ToLower(s), counts[s]), Style: stateStyles[s]})
		}
	}
	if n := counts[""]; n > 0 {
		states = append(states, widget.Span{Text: fmt.Sprintf("udp\u00a0%d", n), Style: widget.Theme.Muted})
	}
	activity := widget.Column(
		widget.Styled("sockets over time", styleLabel),
		widget.Fixed(3, view.E(widget.Sparkline{Values: m.history})),
		view.E(widget.Text{Spans: states, Wrap: true}),
	)
	return widget.Row(
		widget.Grow(3, widget.Panel("Connection", body)),
		widget.Grow(2, widget.Panel("Activity", activity)),
	)
}

func (m *model) footer() view.Element {
	hint := func(k, what string) []widget.Span {
		return []widget.Span{{Text: " " + k, Style: widget.Theme.Accent}, {Text: " " + what + " ", Style: widget.Theme.Muted}}
	}
	var spans []widget.Span
	for _, h := range [][2]string{{"↑↓", "select"}, {"1-6", "sort"}, {"/", "filter"}, {"p", "pause"}, {"r", "refresh"}, {"?", "help"}, {"q", "quit"}} {
		spans = append(spans, hint(h[0], h[1])...)
	}
	return view.E(widget.Text{Spans: spans, Ellipsis: true})
}

func (m *model) helpDialog() view.Element {
	closeHelp := func() { m.help = false }
	lines := []string{
		"Arrows, j and k, PgUp, PgDn, g and G   move the selection",
		"Click a row to select it, double-click to filter by its process",
		"Click a column header, or press 1 to 6, to sort; again to reverse",
		"/ edits the filter; Enter or Esc returns to the table",
		"Esc in the table clears the filter",
		"p pauses refreshing, r refreshes now",
		"Drag the line above the details to resize the panes",
		"Drag across text to copy it",
		"q or ctrl+c quits",
	}
	rows := make([]view.Element, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, widget.Label(l))
	}
	return (widget.Dialog{
		Title: "Keys", Body: widget.Column(rows...), Width: 76,
		Buttons: []widget.DialogButton{{Label: "Close", Variant: widget.ButtonPrimary, Default: true, OnClick: closeHelp}},
		OnClose: closeHelp,
	}).Element()
}
