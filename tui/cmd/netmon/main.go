// Command netmon shows the machine's network connections and the
// processes that hold them, refreshed every few seconds. It only reads:
// it never closes connections or signals processes.
//
// It lists what the current user may inspect. Run it as root to see
// every process's sockets.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/app"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

func main() {
	every := flag.Duration("every", 2*time.Second, "refresh interval")
	fake := flag.Int("fake", 0, "show `n` simulated connections that churn, instead of the system's")
	flag.Parse()

	source := Source(systemSource)
	if *fake > 0 {
		source = fakeSource(*fake)
	}
	m := newModel(source, max(*every, 100*time.Millisecond), time.Now)
	err := app.Run(func(*app.App) view.Element { return m.view() }, app.Options{
		Start: func(a *app.App) {
			m.host = a
			m.refresh()
			a.Every(m.interval, m.tick)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "netmon:", err)
		os.Exit(1)
	}
}
