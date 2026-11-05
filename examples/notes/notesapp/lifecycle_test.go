package notesapp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

func TestLegacyBuildersKeepWebPresentationAndSharedService(t *testing.T) {
	builders := map[string]func(string, string) (*framework.App, *mobilehost.Host, error){
		"Build": Build,
		"BuildScreensWithPlatform": func(directory, origin string) (*framework.App, *mobilehost.Host, error) {
			return BuildScreensWithPlatform(directory, origin, mobileui.IOS)
		},
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { owner.SetExtractor(nil) })
			directory := t.TempDir()
			app, host, err := build(directory, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stopNotes(t, host) })
			if len(app.Mountables()) != 1 {
				t.Fatal("legacy builder no longer mounts its web presentation")
			}
			if err := host.Start(); err != nil {
				t.Fatal(err)
			}
			user, err := localapp.LoadOrMint(directory)
			if err != nil {
				t.Fatal(err)
			}
			ctx := handler.SetUser(context.Background(), user)
			created, err := Service(app).Save(ctx, "", "Shared composition", "Legacy web callers still work.")
			if err != nil {
				t.Fatal(err)
			}
			page := do(t, host, http.MethodGet, "/notes", "", "")
			if page.Status != http.StatusOK || !strings.Contains(string(page.Body), created.Title) {
				t.Fatalf("shared service missing from legacy presentation: %d %s", page.Status, page.Body)
			}
			policy := "geolocation=(), microphone=(), camera=()"
			if name == "BuildScreensWithPlatform" {
				policy = "geolocation=(), microphone=(self), camera=()"
			}
			if page.Header.Get("Permissions-Policy") != policy || page.Header.Get("Content-Security-Policy") == "" {
				t.Fatalf("presentation permissions or security defaults changed: %v", page.Header)
			}
			if strings.Contains(page.Header.Get("Content-Security-Policy"), "media-src 'self' blob:") != (name == "BuildScreensWithPlatform") {
				t.Fatalf("private audio playback policy leaked across presentations: %v", page.Header)
			}
			bridge := do(t, host, http.MethodGet, "/__gofastr-mobile/bridge.js", "", "")
			if bridge.Status != http.StatusOK || len(bridge.Body) == 0 {
				t.Fatal("legacy builder lost the WebView bridge")
			}
			stopNotes(t, host)
			if err := app.DB.Ping(); err == nil {
				t.Fatal("legacy shutdown leaked installation data")
			}
		})
	}
}

func TestFailedPresentationConstructionClosesData(t *testing.T) {
	failure := errors.New("presentation construction failed")
	for _, origin := range []string{"", "not-an-origin"} {
		t.Run("origin="+origin, func(t *testing.T) {
			var database *sql.DB
			_, _, err := buildWithPresentation(t.TempDir(), origin, mobileui.IOS, func(app *framework.App, _ *notes.Service) error {
				database = app.DB
				if origin == "" {
					return failure
				}
				return nil
			})
			if err == nil {
				t.Fatal("invalid presentation or host configuration was accepted")
			}
			if origin == "" && !errors.Is(err, failure) {
				t.Fatalf("lost construction failure: %v", err)
			}
			if database == nil {
				t.Fatal("test did not reach shared composition")
			}
			if err := database.Ping(); err == nil {
				t.Fatal("failed construction leaked the shared database")
			}
		})
	}
}
