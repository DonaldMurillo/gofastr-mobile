package nativeui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

func liveFixture(t *testing.T, watch Watch, allowed *atomic.Bool, editor bool) (*Runtime, Command) {
	t.Helper()
	app, err := New("/list", Route{Path: "/list", Screen: func(context.Context, Request) (Screen, error) {
		if allowed != nil && !allowed.Load() {
			return Screen{}, &ActionError{Code: "forbidden", Message: "No longer authorized"}
		}
		screen := Screen{Title: "Live", List: &List{SearchKey: "q", Rows: []Row{}}, Live: &Live{Version: "epoch:1"}}
		if editor {
			screen.List = nil
			screen.Form = &Form{Fields: []Field{{Key: "body", Label: "Body", Kind: "multiline", Value: "saved"}}, Draft: &Draft{Key: "live-editor"}}
		}
		return screen, nil
	}, Watch: watch})
	if err != nil {
		t.Fatal(err)
	}
	options := RuntimeOptions{}
	if editor {
		options.Drafts, err = behavior.NewFileDraftStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
	}
	runtime, err := NewRuntime(app, options)
	if err != nil {
		t.Fatal(err)
	}
	connection := serviceConnection(t, runtime)
	opened := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: connection, Route: "/list", Values: map[string]string{"q": "private"}})
	if opened.Error != nil || opened.DataVersion != "epoch:1" {
		t.Fatalf("live snapshot unavailable: %+v", opened)
	}
	command := Command{Kind: "watch", Connection: connection, Instance: opened.Instance, DataVersion: opened.DataVersion, Values: map[string]string{"q": "private"}, Revision: 1}
	if editor {
		command.Values = nil
	}
	return runtime, command
}

func TestLiveWatchReleasesLockAndRejectsObsoleteInstances(t *testing.T) {
	for _, interruption := range []string{"close", "open", "checkpoint"} {
		t.Run(interruption, func(t *testing.T) {
			started := make(chan struct{})
			runtime, command := liveFixture(t, func(ctx context.Context, request Request, version string) (string, error) {
				if request.Values["q"] != "private" || version != "epoch:1" {
					t.Error("watch lost authorized query")
				}
				close(started)
				<-ctx.Done()
				return "epoch:2", nil
			}, nil, false)
			finished := make(chan Reply, 1)
			go func() { finished <- serviceCommand(t, context.Background(), runtime, command) }()
			<-started
			interrupted := command
			interrupted.Kind = interruption
			if interruption == "open" {
				interrupted.Route = "/list"
			}
			acted := make(chan Reply, 1)
			go func() { acted <- serviceCommand(t, context.Background(), runtime, interrupted) }()
			select {
			case reply := <-acted:
				if reply.Error != nil {
					t.Fatal(reply.Error)
				}
			case <-time.After(time.Second):
				t.Fatal("watch held the session lock")
			}
			select {
			case reply := <-finished:
				if reply.Error == nil || reply.Error.Code != "stale" || reply.Changed {
					t.Fatalf("obsolete watch applied: %+v", reply)
				}
			case <-time.After(time.Second):
				t.Fatal("retirement did not cancel the watch")
			}
		})
	}
}

func TestLiveWatchBoundedCancellationAndReadOnlyResults(t *testing.T) {
	t.Run("changed", func(t *testing.T) {
		runtime, command := liveFixture(t, func(context.Context, Request, string) (string, error) { return "epoch:2", nil }, nil, false)
		for attempt := 0; attempt < 20; attempt++ {
			reply := serviceCommand(t, context.Background(), runtime, command)
			if reply.Error != nil || !reply.Changed || reply.DataVersion != "epoch:2" || reply.Response != nil || reply.Screen != nil || reply.Editor != nil {
				t.Fatalf("watch triggered non-read effects or failed to release its worker: %+v", reply)
			}
		}
	})
	t.Run("ignores cancellation", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		runtime, command := liveFixture(t, func(context.Context, Request, string) (string, error) { <-release; return "late", nil }, nil, false)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		started := time.Now()
		reply := serviceCommand(t, ctx, runtime, command)
		if reply.Error == nil || time.Since(started) > time.Second {
			t.Fatalf("watch did not respect cancellation: %+v", reply)
		}
		if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "pending" {
			t.Fatal("uncooperative watch spawned unbounded replacements")
		}
	})
	t.Run("maximum deadline", func(t *testing.T) {
		runtime, command := liveFixture(t, func(ctx context.Context, _ Request, _ string) (string, error) {
			deadline, exists := ctx.Deadline()
			if !exists || time.Until(deadline) > 10*time.Second {
				t.Error("watch lacks its maximum deadline")
			}
			return "epoch:1", nil
		}, nil, false)
		if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil || reply.Changed {
			t.Fatal("unchanged watch claimed refresh")
		}
	})
}

func TestLiveWatchReauthorizesAndValidatesQueryVersion(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	var calls atomic.Int32
	runtime, command := liveFixture(t, func(context.Context, Request, string) (string, error) {
		calls.Add(1)
		allowed.Store(false)
		return "epoch:2", nil
	}, &allowed, false)
	for _, version := range []string{"", strings.Repeat("x", 257), "bad\x00version"} {
		invalid := command
		invalid.DataVersion = version
		if reply := serviceCommand(t, context.Background(), runtime, invalid); reply.Error == nil {
			t.Fatal("invalid version reached watch")
		}
	}
	invalid := command
	invalid.Values = map[string]string{"q": "foreign"}
	if reply := serviceCommand(t, context.Background(), runtime, invalid); reply.Error == nil {
		t.Fatal("different query reached retained watch")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid watch invoked application code")
	}
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "forbidden" || reply.Changed {
		t.Fatalf("revoked route leaked live result: %+v", reply)
	}
}

func TestLiveWatchRetainsErrorsAndRejectsSupersededDraft(t *testing.T) {
	runtime, command := liveFixture(t, func(context.Context, Request, string) (string, error) { return "", errors.New("offline") }, nil, false)
	if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error == nil || reply.Error.Code != "internal" {
		t.Fatalf("read error was misreported as cancellation: %+v", reply)
	}
	started := make(chan struct{})
	runtime, command = liveFixture(t, func(ctx context.Context, _ Request, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "epoch:2", nil
	}, nil, true)
	finished := make(chan Reply, 1)
	go func() { finished <- serviceCommand(t, context.Background(), runtime, command) }()
	<-started
	if reply := serviceCommand(t, context.Background(), runtime, Command{Kind: "open", Connection: command.Connection, Route: "/list", Values: map[string]string{"q": "private"}}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	select {
	case reply := <-finished:
		if reply.Error == nil || reply.Changed {
			t.Fatal("superseded watch applied")
		}
	case <-time.After(time.Second):
		t.Fatal("superseded draft watch did not return")
	}
}
