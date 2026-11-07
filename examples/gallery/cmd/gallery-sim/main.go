// Command gallery-sim opens the component gallery in a phone-shaped Chromium
// window. Every request is answered in process; no HTTP listener is opened.
// The data directory and browser profile are temporary and removed on exit.
//
//	go run ./examples/gallery/cmd/gallery-sim --platform android --trace
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/gallery/gallery"
	"github.com/DonaldMurillo/gofastr-native/examples/gallery/galleryweb"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
	"github.com/DonaldMurillo/gofastr-native/sim/browsersim"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gallery-sim", flag.ContinueOnError)
	flags.SetOutput(stderr)
	platform := flags.String("platform", "ios", "phone shape and transport: ios or android")
	trace := flags.Bool("trace", false, "print redacted gofastr-trace lines to stderr")
	describe := flags.Bool("describe", false, "print the route registry as JSON and exit")
	dark := flags.Bool("dark", false, "emulate prefers-color-scheme: dark")
	headless := flags.Bool("headless", false, "run without a window (use with --screenshot)")
	screenshot := flags.String("screenshot", "", "save a PNG of the first screen to this path, then exit")
	chrome := flags.String("chrome", "", "browser executable; SIM_CHROME or discovery when empty")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "gallery-sim: unexpected arguments")
		return 2
	}
	selected := mobileui.Platform(*platform)
	if err := selected.Validate(); err != nil {
		fmt.Fprintln(stderr, "gallery-sim: --platform must be ios or android")
		return 2
	}
	if *describe {
		app, err := gallery.New()
		if err != nil {
			fmt.Fprintln(stderr, "gallery-sim:", err)
			return 1
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(app.Describe()); err != nil {
			return 1
		}
		return 0
	}
	if *headless && *screenshot == "" {
		fmt.Fprintln(stderr, "gallery-sim: --headless needs --screenshot; there is no window to close")
		return 2
	}
	if err := serve(selected, *trace, *dark, *headless, *screenshot, *chrome, stderr); err != nil {
		fmt.Fprintln(stderr, "gallery-sim:", err)
		return 1
	}
	return 0
}

func serve(platform mobileui.Platform, trace, dark, headless bool, screenshot, chrome string, stderr io.Writer) (failure error) {
	dataDir, err := os.MkdirTemp("", "gofastr-gallery-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(dataDir); err != nil && failure == nil {
			failure = fmt.Errorf("remove temporary data: %w", err)
		}
	}()
	options := nativeui.RuntimeOptions{}
	var logger *devtrace.Logger
	if trace {
		logger = devtrace.NewLogger(stderr, devtrace.DefaultSize)
		options.Observer = logger.Observe
		defer func() {
			logger.Close()
			if dropped := logger.Dropped(); dropped != 0 {
				fmt.Fprintf(stderr, "gallery-sim: %d trace lines dropped\n", dropped)
			}
		}()
	}
	host, _, err := galleryweb.Build(dataDir, platform, options)
	if err != nil {
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	defer func() {
		stopping, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(stopping); err != nil && failure == nil {
			failure = err
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	session, err := browsersim.Open(ctx, host, browsersim.Options{Platform: platform, Headless: headless, Chrome: chrome, Dark: dark})
	if err != nil {
		return err
	}
	defer session.Close()
	if err := session.Navigate("/"); err != nil {
		return err
	}
	if screenshot != "" {
		time.Sleep(time.Second)
		return session.Screenshot(screenshot)
	}
	fmt.Fprintln(stderr, "gallery-sim: close the window or press Ctrl+C to exit")
	session.Wait()
	for _, message := range session.Errors() {
		fmt.Fprintln(stderr, "gallery-sim: page:", message)
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	return nil
}
