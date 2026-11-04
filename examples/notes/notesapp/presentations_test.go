package notesapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesterminal"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

func TestTerminalAndWebSharePersistedNotes(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	dir := t.TempDir()
	app, host, err := BuildWithPlatform(dir, "", mobileui.Android)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopNotes(t, host)
	user, err := localapp.LoadOrMint(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handler.SetUser(context.Background(), user)
	service := Service(app)
	var terminal bytes.Buffer
	if err := notesterminal.Run(ctx, service, strings.NewReader("new\nA terminal thought\nWritten without a DOM.\n.\nlist\nquit\n"), &terminal); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "Saved on this device:") || !strings.Contains(terminal.String(), "A terminal thought") {
		t.Fatalf("terminal workflow: %s", terminal.String())
	}
	page := do(t, host, http.MethodGet, "/notes", "", "")
	if page.Status != http.StatusOK || !strings.Contains(string(page.Body), "Written without a DOM.") || !strings.Contains(string(page.Body), `data-gfm-platform="android"`) {
		t.Fatalf("terminal note missing from web: %d %s", page.Status, page.Body)
	}
	items, err := service.List(ctx, "terminal")
	if err != nil || len(items) != 1 {
		t.Fatalf("typed search = %+v, %v", items, err)
	}
	if items[0].UpdatedAt.IsZero() {
		t.Error("the compatibility adapter lost the updated timestamp")
	}
	id := items[0].ID
	form := url.Values{"id": {id}, "title": {"Edited from the web"}, "body": {"Same note, different presentation."}}
	response := do(t, host, http.MethodPost, "/notes/save", "application/x-www-form-urlencoded", form.Encode())
	if response.Status != http.StatusOK {
		t.Fatalf("web edit: %d %s", response.Status, response.Body)
	}
	terminal.Reset()
	if err := notesterminal.Run(ctx, service, strings.NewReader("open "+id+"\nquit\n"), &terminal); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "Same note, different presentation.") {
		t.Fatalf("web edit missing from terminal: %s", terminal.String())
	}
	other := handler.SetUser(context.Background(), localapp.ForID(strings.Repeat("f", 32)))
	if _, err := service.Get(other, id); err == nil {
		t.Fatal("terminal service bypassed owner-scoped reads")
	}
	if _, err := service.Get(ctx, "missing-note"); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("repository not-found classification = %v", err)
	}
	missing := do(t, host, http.MethodGet, "/notes/missing-note", "", "")
	if missing.Status != http.StatusNotFound {
		t.Errorf("unavailable note status = %d", missing.Status)
	}
	if _, err := service.Save(other, id, "Not mine", "Not mine"); err == nil {
		t.Fatal("terminal service bypassed owner-scoped updates")
	}
	if _, err := service.List(context.Background(), ""); err == nil {
		t.Fatal("a renderer can access owner-scoped data without identity")
	}
	var validation *notes.ValidationError
	if _, err := service.Save(ctx, id, "  ", "Keep me"); !errors.As(err, &validation) {
		t.Fatalf("terminal validation = %v", err)
	}
	badForm := url.Values{"id": {id}, "title": {"  "}, "body": {"Keep me"}}
	badResponse := do(t, host, http.MethodPost, "/notes/save", "application/x-www-form-urlencoded", badForm.Encode())
	var payload map[string]string
	if err := json.Unmarshal(badResponse.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if badResponse.Status != http.StatusUnprocessableEntity || payload["error"] != validation.Message {
		t.Fatalf("presentations disagree on validation: %d %s", badResponse.Status, badResponse.Body)
	}
	current, err := service.Get(ctx, id)
	if err != nil || current.Title != "Edited from the web" {
		t.Fatalf("failed save changed persisted data: %+v %v", current, err)
	}
	t.Log(terminal.String())
}

func TestPresentationCoreDoesNotImportWeb(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps",
		"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes",
		"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesterminal").CombinedOutput()
	if err != nil {
		t.Fatalf("dependency boundary: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "github.com/DonaldMurillo/gofastr/") || strings.Contains(string(output), "gofastr-mobile/mobileui") {
		t.Fatalf("shared behavior or terminal presentation imports the web stack:\n%s", output)
	}
}
