package sim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func TestNotesFilesWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runNotesFiles(t, platform) })
	}
}

func runNotesFiles(t *testing.T, platform mobileui.Platform) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	_, host, err := notesapp.BuildScreensWithPlatform(t.TempDir(), origin, platform)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	directory := t.TempDir()
	importPath := filepath.Join(directory, "idea.md")
	attachmentPath := filepath.Join(directory, "brief.txt")
	for filePath, content := range map[string]string{importPath: "# Imported idea\n\nA file became a note.", attachmentPath: "Private attachment"} {
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android)
	chromedp.ListenTarget(ctx, func(event any) {
		if entry, ok := event.(*log.EventEntryAdded); ok {
			t.Logf("browser log: %s", entry.Entry.Text)
		}
	})
	downloaded := make(chan struct{}, 8)
	stage := 0
	chromedp.ListenBrowser(ctx, func(event any) {
		if beginning, ok := event.(*browser.EventDownloadWillBegin); ok {
			t.Logf("download started: %s", beginning.SuggestedFilename)
		}
		if progress, ok := event.(*browser.EventDownloadProgress); ok && progress.State == browser.DownloadProgressStateCompleted {
			downloaded <- struct{}{}
		}
	})
	check := func(actions ...chromedp.Action) {
		t.Helper()
		stage++
		t.Logf("%s file flow stage %d", platform, stage)
		if err := chromedp.Run(ctx, actions...); err != nil {
			var body, location string
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location))
			t.Fatalf("%s files stage %d: %v\n%s\n%s", platform, stage, err, location, body)
		}
	}
	check(chromedp.Navigate(origin+"/"), clickMobileAction("Files"), waitForFileScreen("/files"),
		clickMobileAction("Import note"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.activeElement?.closest('[data-gfw-file]')`, nil),
		chromedp.KeyEvent("\u001b"), fileDialogClosed(), chromedp.Poll(`document.body.innerText.includes('File task cancelled')`, nil),
		clickMobileAction("Import note"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{importPath}, chromedp.ByQuery),
		fileDialogClosed(), chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('input[name="title"]').value === 'Imported idea'`, nil))
	var noteURL string
	check(chromedp.Location(&noteURL), log.Enable(), chromedp.ActionFunc(func(ctx context.Context) error {
		return browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).WithDownloadPath(directory).WithEventsEnabled(true).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
	}), setMobileField(`input[name="title"]`, "Exported locally"), setMobileField(`textarea[name="body"]`, "Unsaved content"),
		clickMobileAction("Export"), chromedp.WaitVisible(`[data-gfw-file-download]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-file-download]`, chromedp.ByQuery), fileDialogClosed(),
		chromedp.Poll(`document.body.innerText.includes('Download initiated')`, nil),
		chromedp.Poll(`document.querySelector('input[name="title"]').value === 'Exported locally'`, nil))
	select {
	case <-downloaded:
	case <-time.After(5 * time.Second):
		entries, _ := os.ReadDir(directory)
		traffic.mu.Lock()
		details := fmt.Sprint(traffic.errs)
		traffic.mu.Unlock()
		t.Fatalf("download did not complete; files: %v; bridge: %s", entries, details)
	case <-ctx.Done():
		t.Fatal("download did not complete", ctx.Err())
	}
	bytes, err := os.ReadFile(filepath.Join(directory, "Exported locally.md"))
	if err != nil || string(bytes) != "Exported locally\n\nUnsaved content" {
		t.Fatalf("downloaded export mismatch: %q %v", bytes, err)
	}
	check(clickMobileAction("Attachments"),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		waitForFileScreen(strings.TrimPrefix(noteURL, origin)+"/files"))
	check(clickMobileAction("Add file"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{attachmentPath}, chromedp.ByQuery),
		fileDialogClosed(), chromedp.Poll(`document.querySelector('[data-gfw-file-preview] pre')?.textContent === 'Private attachment'`, nil))
	var attachmentURL string
	check(chromedp.Location(&attachmentURL),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery), waitForFileScreen(strings.TrimPrefix(noteURL, origin)+"/files"),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery), chromedp.WaitVisible(`[data-gfw-attachment]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfw-attachment]').textContent.includes('brief.txt') && document.querySelector('textarea[name="body"]').value === 'Unsaved content'`, nil),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.ScrollIntoView(`[data-gfw-attachment]`, chromedp.ByQuery),
		chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth + 1`, nil))
	var attachmentPixels []byte
	check(chromedp.CaptureScreenshot(&attachmentPixels))
	if err := os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-note-with-file.png"), attachmentPixels, 0600); err != nil {
		t.Fatal(err)
	}
	check(chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil),
		chromedp.Click(`[data-gfw-attachment]`, chromedp.ByQuery),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		waitForFileScreen(strings.TrimPrefix(attachmentURL, origin)),
		clickMobileAction("Export file"), chromedp.WaitVisible(`[data-gfw-file-download]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-file-cancel]`, chromedp.ByQuery), fileDialogClosed(),
		clickMobileAction("Remove"), chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-choice="remove"]`, chromedp.ByQuery), presentationClosed(),
		waitForFileScreen(strings.TrimPrefix(noteURL, origin)),
		chromedp.Poll(`document.querySelector('[data-gfw-attachments]').hidden && document.querySelector('textarea[name="body"]').value === 'Unsaved content'`, nil),
		clickMobileAction("Add file"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{attachmentPath}, chromedp.ByQuery), fileDialogClosed(),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Poll(`document.body.innerText.includes('Private attachment stored on this device')`, nil),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery), chromedp.WaitVisible(`[data-gfw-attachment]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Unsaved content'`, nil))
	var pixels []byte
	check(chromedp.Navigate(origin+"/files"), clickMobileAction("Import note"), chromedp.WaitVisible(`[data-gfw-file]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Poll(`(() => { const dialog = document.querySelector('[data-gfw-file]'); return dialog.scrollWidth <= dialog.clientWidth + 1; })()`, nil),
		chromedp.CaptureScreenshot(&pixels))
	if err := os.WriteFile(filepath.Join(shotsDir(t), fmt.Sprintf("%s-file-picker.png", platform)), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Errorf("renderer errors: %v", traffic.errs)
	}
}

func fileDialogClosed() chromedp.Action {
	return chromedp.Poll(`!document.querySelector('[data-gfw-file]')`, nil)
}

func waitForFileScreen(route string) chromedp.Action {
	predicate := fmt.Sprintf(`(() => {
		const screen = document.querySelector('[data-gfw-config]');
		return screen && screen.dataset.gfwInstance && JSON.parse(screen.dataset.gfwConfig).route === %s;
	})()`, mustJSON(route))
	return chromedp.ActionFunc(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		for {
			err := chromedp.Poll(predicate, nil, chromedp.WithPollingTimeout(0)).Do(ctx)
			if err == nil || !strings.Contains(err.Error(), "Cannot find context with specified id (-32000)") {
				return err
			}
			// Poll binds one JavaScript context. Navigation can replace it
			// before CDP evaluates this read-only readiness predicate. Recheck
			// the new document without repeating navigation or user actions.
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	})
}
