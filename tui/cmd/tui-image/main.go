package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr-native/tui/app"
	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

type imageScroll struct{ widget.Scroll }

func (imageScroll) Focusable() bool { return true }
func (imageScroll) AutoFocus() bool { return true }

func imageView(source *cell.Image, path, protocol string) view.Element {
	return widget.Column(
		widget.Grow(1, widget.Panel(path, view.E(imageScroll{}, view.E(widget.Image{Source: source, Width: 80, Fit: widget.Contain, Alt: "No image"})))),
		widget.Fixed(1, widget.Label("Image: "+protocol+" · wheel/page keys scroll · q quits")),
	)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tui-image <file>")
		os.Exit(2)
	}
	source, err := widget.LoadImage(os.Args[1])
	if err == nil {
		err = app.Run(func(application *app.App) view.Element {
			return imageView(source, os.Args[1], application.Caps().Images.String())
		}, app.Options{Unhandled: func(application *app.App, event input.Event) bool {
			if key, ok := event.(input.Key); ok && key.Code == input.KeyRune && key.Rune == 'q' && key.Mods == 0 {
				application.Quit()
				return true
			}
			return false
		}})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-image:", err)
		os.Exit(1)
	}
}
