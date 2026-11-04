// Command notes-terminal exercises the terminal adapter over Notes' local data.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesterminal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	directory := flag.String("data", "", "absolute Notes data directory (required)")
	flag.Parse()
	if !filepath.IsAbs(*directory) {
		return fmt.Errorf("pass --data with an absolute directory path")
	}
	app, host, err := notesapp.Build(*directory, "")
	if err != nil {
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = host.Stop(ctx)
	}()
	user, err := localapp.LoadOrMint(*directory)
	if err != nil {
		return err
	}
	return notesterminal.Run(handler.SetUser(context.Background(), user), notesapp.Service(app), os.Stdin, os.Stdout)
}
