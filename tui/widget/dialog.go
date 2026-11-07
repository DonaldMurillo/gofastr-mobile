package widget

import (
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// DialogButton describes one dialog action. Default takes focus on open.
type DialogButton struct {
	Label   string
	Variant ButtonVariant
	OnClick func()
	Default bool
}

// Dialog shows a titled modal with a body and right-aligned actions.
type Dialog struct {
	Title string
	Text  string
	// Body replaces Text when non-empty.
	Body        view.Element
	Buttons     []DialogButton
	OnClose     func()
	Width       int // maximum width; 60 when zero
	BorderStyle cell.Style
}

// Element returns the modal element. Put it last in Layers.
func (d Dialog) Element() view.Element {
	body := d.Body
	if body.Widget == nil {
		body = Paragraph(d.Text)
	}
	defaultAt := 0
	for i, b := range d.Buttons {
		if b.Default {
			defaultAt = i
			break
		}
	}
	buttons := make([]view.Element, len(d.Buttons))
	for i, b := range d.Buttons {
		buttons[i] = view.E(Button{Label: b.Label, Variant: b.Variant, OnClick: b.OnClick, AutoFocused: i == defaultAt})
	}
	width := d.Width
	if width <= 0 {
		width = 60
	}
	actions := view.E(dialogActions{Flex{Dir: Horizontal, Gap: 1, Align: layout.Start, Justify: layout.End}}, buttons...)
	// Visit actions first so a body's AutoFocuser cannot override Default.
	content := view.E(dialogContent{}, actions, Label(""), body)
	box := view.E(surfaceBox{Box{Border: BorderRounded, Title: d.Title, Padding: layout.Symmetric(0, 1), BorderStyle: d.BorderStyle, Style: Theme.Text}}, content)
	return view.E(Modal{OnClose: d.OnClose}, Limit(width, 0, box))
}

type dialogContent struct{}

func (dialogContent) Layout(n *view.Node, c layout.Constraints) layout.Size {
	size := (Flex{Dir: Vertical, Align: layout.Stretch}).Layout(n, c)
	bodyHeight := n.Child(2).Size().H
	n.Place(2, layout.Point{})
	n.Place(1, layout.Point{Y: bodyHeight})
	n.Place(0, layout.Point{Y: bodyHeight + n.Child(1).Size().H})
	return size
}

func (dialogContent) Draw(*view.Node, *view.Canvas) {}

type dialogActions struct{ Flex }

func (dialogActions) Handle(n *view.Node, e view.Event) bool {
	k, ok := e.(view.KeyEvent)
	if !ok || k.Mods != 0 || (k.Code != input.KeyLeft && k.Code != input.KeyRight) || n.Len() == 0 {
		return false
	}
	for i := 0; i < n.Len(); i++ {
		if n.Child(i).Focused() {
			delta := 1
			if k.Code == input.KeyLeft {
				delta = -1
			}
			n.Child((i + delta + n.Len()) % n.Len()).Focus()
			return true
		}
	}
	return false
}

// Confirm returns a dialog with Cancel and a default primary action.
func Confirm(title, text, confirmLabel string, onConfirm, onCancel func()) Dialog {
	return Dialog{
		Title: title, Text: text, OnClose: onCancel,
		Buttons: []DialogButton{
			{Label: "Cancel", OnClick: onCancel},
			{Label: confirmLabel, Variant: ButtonPrimary, OnClick: onConfirm, Default: true},
		},
	}
}
