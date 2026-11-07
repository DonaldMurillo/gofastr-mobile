// Package widget has the standard widgets built on package view: text,
// flex rows and columns, boxes, stacks, modals, dialogs, notifications,
// buttons, text input, scroll views, virtualized lists and tables, split
// panes, tabs and sparklines.
//
// Widgets are plain structs. Wrap one in view.E to make an element, or
// use the helper constructors such as Row, Column and Label. See
// widget/docs/dialogs-notifications.md for dialog and notification usage.
// See widget/docs/paste-tabs-clipboard.md for paste and tab behavior.
package widget

import "github.com/DonaldMurillo/gofastr-native/tui/cell"

// Styles is the set of styles widgets fall back to.
type Styles struct {
	Text, Muted, Accent         cell.Style
	Border, BorderFocus         cell.Style
	Title                       cell.Style
	Selected, SelectedBlurred   cell.Style
	Header                      cell.Style
	Button, ButtonHover         cell.Style
	ButtonFocus, ButtonPress    cell.Style
	ButtonPrimary, ButtonDanger cell.Style
	NoticeInfo, NoticeSuccess   cell.Style
	NoticeWarning, NoticeError  cell.Style
	Input, InputFocus           cell.Style
	Placeholder                 cell.Style
	ScrollTrack, ScrollThumb    cell.Style
	Divider, DividerActive      cell.Style
	Backdrop                    cell.Style
	TabActive, Tab              cell.Style
}

// DefaultTheme uses the 16 ANSI colors, so it follows the user's terminal
// palette, plus two grays from the 256-color palette for input fields. The
// renderer downgrades those on 16-color terminals.
var DefaultTheme = Styles{
	Text:            cell.Style{},
	Muted:           cell.Style{Fg: cell.BrightBlack},
	Accent:          cell.Style{Fg: cell.Cyan},
	Border:          cell.Style{Fg: cell.BrightBlack},
	BorderFocus:     cell.Style{Fg: cell.Cyan},
	Title:           cell.Style{Attrs: cell.Bold},
	Selected:        cell.Style{Fg: cell.Black, Bg: cell.Cyan},
	SelectedBlurred: cell.Style{Attrs: cell.Reverse},
	Header:          cell.Style{Attrs: cell.Bold},
	Button:          cell.Style{Attrs: cell.Reverse},
	ButtonPrimary:   cell.Style{Fg: cell.Black, Bg: cell.Cyan, Attrs: cell.Bold},
	ButtonDanger:    cell.Style{Fg: cell.White, Bg: cell.Red, Attrs: cell.Bold},
	NoticeInfo:      cell.Style{Fg: cell.Cyan},
	NoticeSuccess:   cell.Style{Fg: cell.Green},
	NoticeWarning:   cell.Style{Fg: cell.Yellow},
	NoticeError:     cell.Style{Fg: cell.Red},
	ButtonHover:     cell.Style{Fg: cell.Black, Bg: cell.BrightCyan},
	ButtonFocus:     cell.Style{Fg: cell.Black, Bg: cell.Cyan, Attrs: cell.Bold},
	ButtonPress:     cell.Style{Fg: cell.Black, Bg: cell.BrightWhite, Attrs: cell.Bold},
	Input:           cell.Style{Bg: cell.Indexed(236)},
	InputFocus:      cell.Style{Bg: cell.Indexed(238)},
	Placeholder:     cell.Style{Fg: cell.BrightBlack, Bg: cell.Indexed(236)},
	ScrollTrack:     cell.Style{Fg: cell.Indexed(238)},
	ScrollThumb:     cell.Style{Fg: cell.BrightBlack},
	Divider:         cell.Style{Fg: cell.BrightBlack},
	DividerActive:   cell.Style{Fg: cell.Cyan},
	Backdrop:        cell.Style{Attrs: cell.Dim},
	TabActive:       cell.Style{Fg: cell.Black, Bg: cell.Cyan, Attrs: cell.Bold},
	Tab:             cell.Style{Fg: cell.BrightBlack},
}

// Theme is the styles widgets use when their own style fields are zero.
// Change it before the first frame.
var Theme = DefaultTheme
