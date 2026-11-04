package notesnative

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesterminal"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
)

func TestNativeCommandsPersistAcrossRestarts(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	if _, err := List(""); err == nil {
		t.Fatal("command ran before startup")
	}
	directory := filepath.Join(t.TempDir(), "first-launch", "app")
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err == nil {
		t.Fatal("duplicate startup accepted")
	}
	created, err := Save("", "A native idea", "Written without a WebView.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(created.ID, "  ", "Keep my draft"); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("native validation: %v", err)
	}
	if _, err := Save(created.ID, "A native idea", "Edited with native controls."); err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	items, err := List("native")
	if err != nil || items.Count() != 1 || items.Item(0).Body != "Edited with native controls." {
		t.Fatalf("persisted native search: %+v %v", items, err)
	}
	if items.Item(-1) != nil || items.Item(1) != nil {
		t.Fatal("out-of-range native snapshot access")
	}
	if _, err := Get("missing"); err == nil {
		t.Fatal("missing note was returned")
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(created.ID); err == nil {
		t.Fatal("another installation can read the first one's data")
	}
}

func TestNativeNotesOpenInWebAndTerminal(t *testing.T) {
	t.Cleanup(func() { _ = Stop(); owner.SetExtractor(nil) })
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	created, err := Save("", "Native to web", "Shared behavior, independent screens.")
	if err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	app, host, err := notesapp.Build(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Stop(context.Background()) })
	response, err := host.Do(context.Background(), &mobilehost.Request{Method: "GET", URL: mobilehost.DefaultOrigin + "/notes/" + created.ID})
	if err != nil || response.Status != 200 || !strings.Contains(string(response.Body), created.Body) {
		t.Fatalf("native note did not reach the web presentation: %+v %v", response, err)
	}
	user, err := localapp.LoadOrMint(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handler.SetUser(context.Background(), user)
	var output bytes.Buffer
	if err := notesterminal.Run(ctx, notesapp.Service(app), strings.NewReader("open "+created.ID+"\nquit\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), created.Body) {
		t.Fatalf("native note did not reach terminal: %s", output.String())
	}
}
