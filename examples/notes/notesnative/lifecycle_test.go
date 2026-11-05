package notesnative

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
)

func TestNativeStartupDoesNotDependOnWebPresentation(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	output, err := exec.Command("go", "list", "-deps", "github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesnative").CombinedOutput()
	if err != nil {
		t.Fatalf("native dependency boundary: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, forbidden := range []string{
			"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp",
			"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesweb",
			"github.com/DonaldMurillo/gofastr-mobile/mobileui",
			"github.com/DonaldMurillo/gofastr-mobile/render/webview",
		} {
			if dependency == forbidden {
				t.Errorf("native bootstrap depends on a web presentation: %s", dependency)
			}
		}
	}
	if err := Start(filepath.Join(t.TempDir(), "fresh", "native")); err != nil {
		t.Fatal(err)
	}
	state.Lock()
	host := state.host
	state.Unlock()
	for _, route := range []string{"/", "/notes", "/notes/new", "/__gofastr-mobile/bridge.js"} {
		response, err := host.Do(context.Background(), &mobilehost.Request{URL: mobilehost.DefaultOrigin + route})
		if err != nil || response.Status != http.StatusNotFound {
			t.Fatalf("native startup installed unused web route %s: %+v %v", route, response, err)
		}
	}
	if _, err := Render(Root(), "{}"); err != nil {
		t.Fatalf("removing the web UI broke native screens: %v", err)
	}
}

func TestNativeRefusesForeignIdentityBeforeOpeningData(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	owner.SetExtractor(func(context.Context) (any, bool) { return "auth-owner", true })
	directory := filepath.Join(t.TempDir(), "not-created")
	if err := Start(directory); err == nil || !strings.Contains(err.Error(), "authenticator") {
		t.Fatalf("foreign authenticator = %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused startup created installation data: %v", err)
	}
	if id, ok := owner.GetExtractor()(context.Background()); !ok || id != "auth-owner" {
		t.Fatal("native bootstrap replaced the authenticator")
	}
	if _, err := Save("", "Not started", "No"); err == nil {
		t.Fatal("failed startup admitted a command")
	}
	owner.SetExtractor(nil)
	if err := Start(directory); err != nil {
		t.Fatalf("refused startup prevented a later valid startup: %v", err)
	}
}

func TestNativeMigrationFailureCanRestartWithoutReminting(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	directory := t.TempDir()
	data, err := localapp.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	identity, secret := data.User.GetID(), data.Secret
	if _, err := data.DB.Exec("CREATE VIEW notes AS SELECT 'invalid' AS id"); err != nil {
		t.Fatal(err)
	}
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("invalid persisted schema started: %v", err)
	}
	if _, err := List(""); err == nil {
		t.Fatal("failed startup published its service")
	}
	if err := Stop(); err != nil {
		t.Fatalf("failed startup requires extra teardown: %v", err)
	}
	data, err = localapp.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if data.User.GetID() != identity || data.Secret != secret {
		t.Fatal("failed startup replaced installation identity")
	}
	if _, err := data.DB.Exec("DROP VIEW notes"); err != nil {
		t.Fatal(err)
	}
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatalf("corrected installation cannot restart: %v", err)
	}
	created, err := Save("", "After migration repair", "Lifecycle was not duplicated.")
	if err != nil || created.ID == "" {
		t.Fatalf("recovered native service: %+v %v", created, err)
	}
	state.Lock()
	userID := state.user.GetID()
	state.Unlock()
	if userID != identity {
		t.Fatal("recovered startup reminted identity")
	}
}

func TestStopDrainsCommandsWithoutHoldingLifecycleLock(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	nestedResult := make(chan error, 1)
	screens, err := nativeui.New("/wait", nativeui.Route{
		Path: "/wait",
		Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Waiting", Text: "Wait for the lifecycle test.", Actions: []nativeui.Action{{ID: "wait", Label: "Wait"}}}, nil
		},
		Actions: map[string]nativeui.Handle{"wait": func(ctx context.Context, _ nativeui.Request) (nativeui.Result, error) {
			if _, err := List(""); err != nil {
				return nativeui.Result{}, err
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nativeui.Result{}, ctx.Err()
			}
			_, err := List("")
			nestedResult <- err
			return nativeui.Result{}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.Lock()
	state.screens = screens
	service := state.service
	ctx := handler.SetUser(context.Background(), state.user)
	state.Unlock()
	commandDone := make(chan error, 1)
	go func() { _, err := Dispatch("/wait", "wait", "{}"); commandDone <- err }()
	select {
	case <-entered:
	case err := <-commandDone:
		t.Fatalf("command failed before entering its handler: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("command could not call another native boundary without deadlocking")
	}
	stops := make(chan error, 2)
	for range 2 {
		go func() { stops <- Stop() }()
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		state.Lock()
		stopping := state.stopping != nil
		state.Unlock()
		if stopping {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("Stop could not close command admission during an active command")
		}
	}
	if _, err := List(""); err == nil {
		t.Fatal("Stop admitted another list")
	}
	if _, err := Save("", "Too late", "No"); err == nil {
		t.Fatal("Stop admitted another save")
	}
	if _, err := Render("/wait", "{}"); err == nil {
		t.Fatal("Stop admitted another render")
	}
	if _, err := Dispatch("/wait", "wait", "{}"); err == nil {
		t.Fatal("Stop admitted another dispatch")
	}
	if err := Start(directory); err == nil {
		t.Fatal("startup overlapped a draining runtime")
	}
	if _, err := service.List(ctx, ""); err != nil {
		t.Fatalf("Stop closed the database under an admitted command: %v", err)
	}
	select {
	case err := <-stops:
		t.Fatalf("Stop returned before the command completed: %v", err)
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-nestedResult:
		if err == nil {
			t.Fatal("a callback bypassed closed admission")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop held a business lock across the callback wait")
	}
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admitted command did not finish")
	}
	for range 2 {
		select {
		case err := <-stops:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent Stop did not observe completed shutdown")
		}
	}
	if _, err := service.List(ctx, ""); err == nil {
		t.Fatal("shutdown left the service's database open")
	}
	if err := Start(directory); err != nil {
		t.Fatalf("runtime cannot restart after its operations drain: %v", err)
	}
}
