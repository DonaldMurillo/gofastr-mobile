package sim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

func TestPortableNotesFlowsInBrowser(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	app, host, err := notesapp.BuildScreensWithPlatform(directory, origin, mobileui.Android)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	user, err := localapp.LoadOrMint(directory)
	if err != nil {
		t.Fatal(err)
	}
	service := notesapp.Service(app)
	auth := handler.SetUser(context.Background(), user)
	for _, title := range []string{"Needle", "Other"} {
		if _, err := service.Save(auth, "", title, strings.Repeat("before ", 45)+"needle passage with <script>plain text</script>"); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := notesbackup.Encode(notesbackup.Bundle{Format: "gofastr-notes", Version: 1, Notes: []notesbackup.Record{{Note: notes.Note{ID: "portable-source", Title: "Imported backup", Body: "Restored body", Details: notes.Details{Tag: "work"}}, Files: []files.Document{{Name: "brief.txt", MediaType: "text/plain", Data: []byte("Restored bytes")}}}}})
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "import.gfnbackup")
	if err = os.WriteFile(input, backup.Data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, traffic := mobileScreensBrowser(t, host, true)
	check := func(actions ...chromedp.Action) {
		t.Helper()
		stage, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := chromedp.Run(stage, actions...); err != nil {
			var diagnostic string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({route:location.pathname+location.search,config:document.querySelector('[data-gfw-config]')?.dataset.gfwConfig,instance:document.querySelector('[data-gfw-screen]')?.dataset.gfwInstance,body:document.body.innerText})`, &diagnostic))
			t.Fatalf("%v\n%s", err, diagnostic)
		}
	}
	check(chromedp.Navigate(origin+"/notes?search=needle&order=relevance"), chromedp.WaitVisible(`[data-gfw-screen][data-gfw-instance]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('.gfm-row mark').length >= 3`, nil),
		chromedp.Poll(`document.querySelector('.gfm-row h2')?.textContent === 'Needle' && document.querySelector('.gfm-row p')?.textContent.includes('needle passage')`, nil),
		chromedp.Click(`[data-gfw-selection-toggle]`, chromedp.ByQuery), chromedp.Click(`[data-gfw-select-loaded]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('[data-gfw-select]:checked').length === 2`, nil),
		chromedp.Click(`[data-gfw-clear-selection]`, chromedp.ByQuery), chromedp.Poll(`document.querySelectorAll('[data-gfw-select]:checked').length === 0`, nil),
		chromedp.Click(`[data-gfw-select-loaded]`, chromedp.ByQuery), clickMobileAction("Tag selected"), chromedp.WaitVisible(`[data-gfw-option="ideas"]`, chromedp.ByQuery))
	check(chromedp.Click(`[data-gfw-option="ideas"]`, chromedp.ByQuery), chromedp.Click(`[data-gfw-choice="apply"]`, chromedp.ByQuery), presentationClosed(),
		chromedp.Poll(`document.querySelectorAll('.gfw-row-metadata').length === 2 && Array.from(document.querySelectorAll('.gfw-row-metadata')).every(node => node.textContent.includes('ideas'))`, nil),
		chromedp.Poll(`document.querySelectorAll('.gfm-row mark').length >= 3`, nil))
	var pixels []byte
	check(chromedp.CaptureScreenshot(&pixels))
	if err = os.WriteFile(filepath.Join(shotsDir(t), "portable-search.png"), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	check(chromedp.Navigate(origin+"/settings/backup"), chromedp.WaitVisible(`[data-gfw-screen][data-gfw-instance]`, chromedp.ByQuery),
		clickMobileAction("Choose backup to restore"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{input}, chromedp.ByQuery), fileDialogClosed(),
		chromedp.WaitVisible(`.gfm-section-heading`, chromedp.ByQuery), chromedp.Poll(`document.body.innerText.includes('1 to restore') && document.body.innerText.includes('Imported backup')`, nil), chromedp.Poll(`!!document.querySelector('[data-gfw-screen]')?.dataset.gfwInstance`, nil))
	items, err := service.BackupNotes(auth)
	if err != nil || len(items) != 2 {
		t.Fatal("browser file picker restored without review")
	}
	check(chromedp.CaptureScreenshot(&pixels))
	if err = os.WriteFile(filepath.Join(shotsDir(t), "portable-backup-review.png"), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	check(clickMobileAction("Restore"), waitForFileScreen("/settings/backup"))
	items, err = service.BackupNotes(auth)
	if err != nil || len(items) != 3 {
		t.Fatalf("browser restore: notes=%d %v", len(items), err)
	}
	// Verify an actual browser download, rather than just its admitted protocol.
	downloadDir := t.TempDir()
	downloaded := make(chan struct{}, 1)
	chromedp.ListenBrowser(ctx, func(event any) {
		if progress, ok := event.(*browser.EventDownloadProgress); ok && progress.State == browser.DownloadProgressStateCompleted {
			select {
			case downloaded <- struct{}{}:
			default:
			}
		}
	})
	check(chromedp.ActionFunc(func(ctx context.Context) error {
		return browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).WithDownloadPath(downloadDir).WithEventsEnabled(true).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
	}),
		clickMobileAction("Export backup"), chromedp.WaitVisible(`[data-gfw-file-download]`, chromedp.ByQuery), chromedp.Click(`[data-gfw-file-download]`, chromedp.ByQuery), fileDialogClosed())
	select {
	case <-downloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("backup download did not complete")
	}
	data, err := os.ReadFile(filepath.Join(downloadDir, "Notes.gfnbackup"))
	if err != nil {
		t.Fatal(err)
	}
	exported, err := notesbackup.Decode(data)
	if err != nil || len(exported.Notes) != 3 {
		t.Fatalf("downloaded backup: %v", err)
	}
	fileCount := 0
	for _, record := range exported.Notes {
		fileCount += len(record.Files)
	}
	if fileCount != 1 {
		t.Fatal("download lost attachment")
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) > 0 {
		t.Fatalf("browser errors: %v", traffic.errs)
	}
}
