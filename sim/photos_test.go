package sim

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/photos"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func TestNotesPhotosWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runNotesPhotos(t, platform) })
	}
}

func runNotesPhotos(t *testing.T, platform mobileui.Platform) {
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
	photoPath := filepath.Join(directory, "sample.png")
	photo := image.NewRGBA(image.Rect(0, 0, 3000, 1800))
	draw.Draw(photo, photo.Bounds(), &image.Uniform{C: color.RGBA{R: 32, G: 140, B: 220, A: 255}}, image.Point{}, draw.Src)
	var fixture bytes.Buffer
	if err := png.Encode(&fixture, photo); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(photoPath, fixture.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotsDir(t), "photo-source.png"), fixture.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(directory, "fake.jpg")
	if err := os.WriteFile(invalidPath, []byte("not a photograph"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android)
	downloaded := make(chan struct{}, 1)
	chromedp.ListenBrowser(ctx, func(event any) {
		if progress, ok := event.(*browser.EventDownloadProgress); ok && progress.State == browser.DownloadProgressStateCompleted {
			select {
			case downloaded <- struct{}{}:
			default:
			}
		}
	})
	stage := 0
	check := func(actions ...chromedp.Action) {
		t.Helper()
		stage++
		t.Logf("photo stage %d", stage)
		stageContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := chromedp.Run(stageContext, actions...); err != nil {
			var body, location string
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location))
			t.Fatalf("%s photo stage %d: %v\n%s\n%s", platform, stage, err, location, body)
		}
	}
	check(chromedp.Navigate(origin+"/"), clickMobileAction("New note"), chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		setMobileField(`input[name="title"]`, "Photo attachments"), setMobileField(`textarea[name="body"]`, "A Go-authored photo task."),
		clickMobileAction("Done"), waitForFileScreen("/notes"), chromedp.Click(`a.gfm-row`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery), clickMobileAction("Attachments"),
		clickMobileAction("Choose photo"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.Poll(`(() => { const root = document.querySelector('[data-gfw-screen]'); const config = JSON.parse(root.dataset.gfwConfig); return config.extensions?.length === 1 && window.__gofastr_file_extension_definitions?.some(adapter => adapter.name === 'gofastr.photos/v1'); })()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-gfw-photo-source="library"]')`, nil),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{photoPath}, chromedp.ByQuery),
		fileDialogClosed(), chromedp.Poll(`!!document.querySelector('[data-gfw-file-preview] [data-gfw-file-icon="photo"]') && document.querySelector('.gfw-file-description').textContent.includes('Photo')`, nil))
	check(chromedp.Poll(`(() => { const photo = document.querySelector('[data-gfw-image] img'); return photo?.complete && photo.naturalWidth > 0; })()`, nil, chromedp.WithPollingTimeout(4*time.Second)))
	var detailURL string
	check(chromedp.Location(&detailURL), chromedp.ActionFunc(func(ctx context.Context) error {
		return browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).WithDownloadPath(directory).WithEventsEnabled(true).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
	}), clickMobileAction("Export file"), chromedp.WaitVisible(`[data-gfw-file-download]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-file-download]`, chromedp.ByQuery), fileDialogClosed())
	select {
	case <-downloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("normalized photo download did not complete")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var normalized []byte
	for _, entry := range entries {
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.Name() != "fake.jpg" && (extension == ".jpg" || extension == ".jpeg") {
			normalized, err = os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(normalized))
	if err != nil || config.Width != photos.MaxDimension || config.Height > photos.MaxDimension || len(normalized) > files.MaxBytes {
		t.Fatalf("photo was not normalized to a bounded JPEG: %+v %d bytes %v", config, len(normalized), err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(normalized)); err != nil {
		t.Fatal("normalized photo is incomplete", err)
	}
	listURL := detailURL[:strings.LastIndex(detailURL, "/")]
	noteURL := strings.TrimSuffix(listURL, "/files")
	check(chromedp.Click(`a.gfm-back`, chromedp.ByQuery), waitForFileScreen(strings.TrimPrefix(listURL, origin)),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery), waitForFileScreen(strings.TrimPrefix(noteURL, origin)),
		chromedp.Poll(`(() => { const photo = document.querySelector('[data-gfw-image] img'); return photo?.complete && photo.naturalWidth > 0 && document.querySelector('textarea[name="body"]').value === 'A Go-authored photo task.'; })()`, nil),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.ScrollIntoView(`[data-gfw-image]`, chromedp.ByQuery),
		chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth + 1`, nil))
	var editorPixels []byte
	check(chromedp.CaptureScreenshot(&editorPixels))
	if err := os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-note-with-photo.png"), editorPixels, 0600); err != nil {
		t.Fatal(err)
	}
	check(chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil),
		setMobileField(`textarea[name="body"]`, "A Go-authored photo task. Unsaved text survives photos."),
		chromedp.Click(`a[data-gfw-image]`, chromedp.ByQuery),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		waitForFileScreen(strings.TrimPrefix(detailURL, origin)),
		chromedp.WaitNotPresent(`textarea[name="body"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfw-image] img')?.naturalWidth > 0`, nil),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery), waitForFileScreen(strings.TrimPrefix(noteURL, origin)),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value.endsWith('Unsaved text survives photos.') && document.querySelector('[data-gfw-image] img')?.naturalWidth > 0`, nil))
	check(chromedp.Navigate(listURL), waitForFileScreen(strings.TrimPrefix(listURL, origin)), clickMobileAction("Take photo"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('[data-gfw-photo-source="camera"]') && document.querySelector('[data-gfw-file-input]').getAttribute('capture') === 'environment'`, nil),
		chromedp.Click(`[data-gfw-file-cancel]`, chromedp.ByQuery), fileDialogClosed(),
		clickMobileAction("Choose photo"), chromedp.WaitVisible(`[data-gfw-file-input]`, chromedp.ByQuery),
		chromedp.SetUploadFiles(`[data-gfw-file-input]`, []string{invalidPath}, chromedp.ByQuery), fileDialogClosed(),
		chromedp.Poll(`location.href === `+fmt.Sprintf("%q", listURL)+` && document.querySelectorAll('a.gfm-row [data-gfw-file-icon="photo"]').length === 1`, nil),
		clickMobileAction("Choose photo"), chromedp.WaitVisible(`[data-gfw-file]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Poll(`(() => { const dialog = document.querySelector('[data-gfw-file]'); return dialog.scrollWidth <= dialog.clientWidth + 1; })()`, nil))
	var pixels []byte
	check(chromedp.CaptureScreenshot(&pixels), chromedp.Click(`[data-gfw-file-cancel]`, chromedp.ByQuery), fileDialogClosed())
	if err := os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-photo-picker.png"), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	check(chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil),
		chromedp.Navigate(detailURL), clickMobileAction("Remove"),
		chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-choice="remove"]`, chromedp.ByQuery), presentationClosed(),
		chromedp.Navigate(noteURL), chromedp.WaitVisible(`textarea[name="body"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('[data-gfw-image]').length === 0 && document.querySelector('textarea[name="body"]').value.endsWith('Unsaved text survives photos.')`, nil))
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Errorf("photo renderer errors: %v", traffic.errs)
	}
}
