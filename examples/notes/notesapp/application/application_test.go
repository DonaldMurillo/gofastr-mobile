package application_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp/application"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
)

func start(t *testing.T, directory string) (*application.Application, *mobilehost.Host) {
	t.Helper()
	shared, err := application.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.App.Shutdown(context.Background()) })
	if len(shared.App.Mountables()) != 0 {
		t.Fatal("host-neutral composition requires a presentation")
	}
	host, err := mobilehost.New(shared.App, mobilehost.Config{Data: shared.Data, ServiceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, host) })
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	return shared, host
}

func stop(t *testing.T, host *mobilehost.Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.Stop(ctx); err != nil {
		t.Error(err)
	}
}

func TestFirstLaunchAndRestartKeepOneInstallation(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := filepath.Join(t.TempDir(), "first", "installation")
	shared, err := application.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	var tableCount int
	if err := shared.Data.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'notes'").Scan(&tableCount); err != nil || tableCount != 0 {
		t.Fatalf("composition ran migrations before Serve: tables=%d, %v", tableCount, err)
	}
	if err := shared.App.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := shared.Data.DB.Ping(); err == nil {
		t.Fatal("abandoned composition left its database open")
	}
	shared, host := start(t, directory)
	userID, secret := shared.Data.User.GetID(), shared.Data.Secret
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	created, err := shared.Service.Save(ctx, "", "Fresh install", "No UIHost was required.")
	if err != nil {
		t.Fatal(err)
	}
	if len(userID) != 32 || created.UpdatedAt.IsZero() {
		t.Fatalf("installation or migrated timestamps missing: %q %+v", userID, created)
	}
	for _, filename := range []string{localapp.DBFile, localapp.SecretFile, localapp.IdentityFile} {
		info, err := os.Stat(filepath.Join(directory, filename))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private installation file %s: %v, %v", filename, info, err)
		}
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("first-launch directory: %v, %v", info, err)
	}
	stop(t, host)
	if err := shared.Data.DB.Ping(); err == nil {
		t.Fatal("shutdown did not close installation data")
	}
	restarted, _ := start(t, directory)
	if restarted.Data.User.GetID() != userID || restarted.Data.Secret != secret {
		t.Fatal("restart replaced installation identity or signing secret")
	}
	ctx = handler.SetUser(context.Background(), restarted.Data.User)
	again, err := restarted.Service.Get(ctx, created.ID)
	if err != nil || again.Title != created.Title || again.Body != created.Body {
		t.Fatalf("restart lost persisted data: %+v %v", again, err)
	}
}

func TestLegacySchemaMigratesWithoutReplacingIdentity(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	legacy, err := localapp.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	userID, secret := legacy.User.GetID(), legacy.Secret
	if _, err := legacy.DB.Exec("CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT, title TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.DB.Exec("INSERT INTO notes (id, user_id, title) VALUES (?, ?, ?)", "existing", userID, "Before upgrade"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	shared, host := start(t, directory)
	if shared.Data.User.GetID() != userID || shared.Data.Secret != secret {
		t.Fatal("migration changed installation identity")
	}
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	before, err := shared.Service.Get(ctx, "existing")
	if err != nil || before.Title != "Before upgrade" {
		t.Fatalf("migration lost the existing note: %+v %v", before, err)
	}
	if before.Details != (notes.Details{}) {
		t.Fatalf("legacy metadata defaults: %+v", before.Details)
	}
	updated, err := shared.Service.Save(ctx, before.ID, before.Title, "Body added after migration")
	if err != nil || updated.UpdatedAt.IsZero() {
		t.Fatalf("migrated columns are not writable: %+v %v", updated, err)
	}
	metadata := notes.Details{Pinned: true, Tag: "ideas", ReminderDate: "2028-02-29", ReminderTime: "14:30"}
	if _, err := shared.Service.SaveDetails(ctx, before.ID, before.Title, updated.Body, metadata); err != nil {
		t.Fatalf("migrated metadata is not writable: %v", err)
	}
	stop(t, host)
	restarted, _ := start(t, directory)
	ctx = handler.SetUser(context.Background(), restarted.Data.User)
	again, err := restarted.Service.Get(ctx, before.ID)
	if err != nil || again.Body != updated.Body || again.Details != metadata {
		t.Fatalf("migrated note did not persist: %+v %v", again, err)
	}
}

func TestServiceAndHTTPShareAuthorizationAndValidation(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	shared, host := start(t, t.TempDir())
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	created, err := shared.Service.Save(ctx, "", "Scoped service", "Native and HTTP share one repository.")
	if err != nil {
		t.Fatal(err)
	}
	otherID := strings.Repeat("f", 32)
	if otherID == shared.Data.User.GetID() {
		otherID = strings.Repeat("e", 32)
	}
	other := handler.SetUser(context.Background(), localapp.ForID(otherID))
	if _, err := shared.Service.Get(other, created.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("foreign owner read = %v", err)
	}
	if _, err := shared.Service.Save(other, created.ID, "Stolen", "No"); err == nil {
		t.Fatal("foreign owner updated a note")
	}
	if rows, err := shared.Service.List(other, ""); err != nil || len(rows) != 0 {
		t.Fatalf("foreign owner list = %+v %v", rows, err)
	}
	if _, err := shared.Service.List(context.Background(), ""); err == nil {
		t.Fatal("anonymous service list was authorized")
	}
	if _, err := shared.Service.Save(context.Background(), "", "Anonymous", "No"); err == nil {
		t.Fatal("anonymous service write was authorized")
	}
	if _, err := shared.Service.Save(ctx, created.ID, " ", "Do not commit"); err == nil {
		t.Fatal("invalid service write was accepted")
	}
	response, err := host.Do(context.Background(), &mobilehost.Request{URL: mobilehost.DefaultOrigin + "/api/notes/" + created.ID})
	if err != nil || response.Status != http.StatusOK || !strings.Contains(string(response.Body), created.Title) {
		t.Fatalf("service write missing from HTTP: %+v %v", response, err)
	}
	response, err = host.Do(context.Background(), &mobilehost.Request{
		Method: http.MethodPost, URL: mobilehost.DefaultOrigin + "/api/notes",
		Header: http.Header{"Content-Type": {"application/json"}, "Origin": {mobilehost.DefaultOrigin}, "Sec-Fetch-Site": {"same-origin"}},
		Body:   []byte(`{"title":"From HTTP","body":"Same installation","user_id":"` + otherID + `"}`),
	})
	if err != nil || response.Status != http.StatusCreated {
		t.Fatalf("HTTP write = %+v %v", response, err)
	}
	items, err := shared.Service.List(ctx, "From HTTP")
	if err != nil || len(items) != 1 || items[0].Body != "Same installation" {
		t.Fatalf("HTTP write missing from service: %+v %v", items, err)
	}
	if rows, err := shared.Service.List(other, "From HTTP"); err != nil || len(rows) != 0 {
		t.Fatalf("HTTP caller forged ownership: %+v %v", rows, err)
	}
}
