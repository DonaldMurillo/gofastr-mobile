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

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/chromedp/cdproto/emulation"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestNotesVisualHierarchyWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			t.Cleanup(func() { owner.SetExtractor(nil) })
			directory := t.TempDir()
			app, host, err := notesapp.BuildScreensWithPlatform(directory, origin, platform)
			if err != nil {
				t.Fatal(err)
			}
			photo := image.NewRGBA(image.Rect(0, 0, 800, 480))
			draw.Draw(photo, photo.Bounds(), &image.Uniform{C: color.RGBA{R: 165, G: 188, B: 187, A: 255}}, image.Point{}, draw.Src)
			draw.Draw(photo, image.Rect(0, 300, 800, 480), &image.Uniform{C: color.RGBA{R: 88, G: 117, B: 100, A: 255}}, image.Point{}, draw.Src)
			var photoBytes bytes.Buffer
			if err := jpeg.Encode(&photoBytes, photo, &jpeg.Options{Quality: 85}); err != nil {
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
			user, err := localapp.LoadOrMint(directory)
			if err != nil {
				t.Fatal(err)
			}
			userContext := handler.SetUser(context.Background(), user)
			service := notesapp.Service(app)
			note, err := service.SaveDetails(userContext, "", "A little space for good ideas", "A quiet place to think, collect, and make things.\n\nKeep the writing simple. Everything else can wait.", notes.Details{Pinned: true, Tag: "ideas"})
			if err != nil {
				t.Fatal(err)
			}
			collection, err := service.Collection(userContext, notes.Query{Page: 1})
			if err != nil || len(collection.Notes) != 1 || collection.Notes[0].Body == "" || !collection.Notes[0].Pinned {
				row, _ := app.MustCrudHandler("notes").GetOne(userContext, note.ID, nil)
				t.Fatalf("collection must retain visual preview metadata: %+v, %v; source=%+v", collection, err, row)
			}
			if _, err := service.Save(userContext, "", "Weekend wandering", "The bookshop on the corner. A long walk home. Coffee with no plans."); err != nil {
				t.Fatal(err)
			}
			store, err := files.NewStore(filepath.Join(directory, "mobile-files"))
			if err != nil {
				t.Fatal(err)
			}
			file, err := store.Put(userContext, note.ID, files.Document{Name: "Thoughts for later.md", MediaType: "text/markdown", Data: []byte("# Thoughts for later\n\nGood ideas need a little room to grow.")})
			if err != nil {
				t.Fatal(err)
			}
			photoFile, err := store.Put(userContext, note.ID, files.Document{Name: "A quiet afternoon.jpg", MediaType: "image/jpeg", Data: photoBytes.Bytes()})
			if err != nil {
				t.Fatal(err)
			}
			for _, scheme := range []string{"light", "dark"} {
				t.Run(scheme, func(t *testing.T) {
					ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android)
					check := deviceBrowserCheck(t, ctx)
					shot := func(name string) chromedp.Action {
						return chromedp.ActionFunc(func(ctx context.Context) error {
							var photoRect []float64
							if strings.HasPrefix(name, "photo-detail-") {
								if err := chromedp.Evaluate(`(async () => {
									const photo = document.querySelector('[data-gfw-file-preview] img');
									await photo.decode();
									await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
									const rect = photo.getBoundingClientRect();
									return [rect.x, rect.y, rect.width, rect.height, innerWidth];
								})()`, &photoRect, func(parameters *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
									return parameters.WithAwaitPromise(true)
								}).Do(ctx); err != nil {
									return err
								}
							}
							var pixels []byte
							if err := chromedp.CaptureScreenshot(&pixels).Do(ctx); err != nil {
								return err
							}
							if len(photoRect) != 0 {
								preview, err := png.Decode(bytes.NewReader(pixels))
								if err != nil {
									return err
								}
								scale := float64(preview.Bounds().Dx()) / photoRect[4]
								point := image.Pt(int((photoRect[0]+photoRect[2]/2)*scale), int((photoRect[1]+photoRect[3]/4)*scale))
								actual := color.NRGBAModel.Convert(preview.At(point.X, point.Y)).(color.NRGBA)
								if photoRect[2] < 100 || photoRect[3] < 100 || actual.R < 157 || actual.R > 173 || actual.G < 180 || actual.G > 196 || actual.B < 179 || actual.B > 195 {
									return fmt.Errorf("photo must be painted in the detail preview: rect=%v, pixel=%v", photoRect, actual)
								}
							}
							return os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-design-"+name+".png"), pixels, 0600)
						})
					}
					check(emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}}),
						chromedp.Navigate(origin+"/notes"), waitForFileScreen("/notes"),
						chromedp.Poll(`!document.querySelector('[data-gfw-filters]').open && document.body.innerText.includes('Pinned')`, nil), shot("list-"+scheme))
					check(chromedp.Click(`a[href="/notes/`+note.ID+`"]`, chromedp.ByQuery), waitForFileScreen("/notes/"+note.ID),
						chromedp.Poll(`document.querySelector('[data-gfw-form-style="document"]') && !document.querySelector('[data-gfw-details]').open && !document.body.innerText.includes('text/markdown') && document.body.innerText.includes('Markdown')`, nil),
						chromedp.Poll(`document.querySelector('[data-gfw-image] img')?.naturalWidth === 512`, nil),
						shot("editor-"+scheme),
						chromedp.Click(`[data-gfw-details] > summary`, chromedp.ByQuery), chromedp.WaitVisible(`input[name="pinned"]`, chromedp.ByQuery),
						emulation.SetDeviceMetricsOverride(320, 844, 3, true), chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
						chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth + 1 && document.querySelector('.gfm-back').getBoundingClientRect().right + 4 <= document.querySelector('.gfm-nav > .gfm-nav-actions').getBoundingClientRect().left`, nil),
						chromedp.Poll(`Array.from(document.querySelectorAll('.gfw-detail-fields .gfw-field')).every(field => { const label = field.querySelector('label').getBoundingClientRect(); const control = field.querySelector('.gfw-input').getBoundingClientRect(); return label.bottom <= control.top + 1 || label.right + 4 <= control.left; })`, nil),
						chromedp.Evaluate(`document.querySelector('[data-gfw-details]').scrollIntoView({block: 'center'})`, nil), shot("large-text-"+scheme),
						emulation.SetDeviceMetricsOverride(390, 844, 3, true), chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil))
					check(chromedp.Click(`[data-gfw-tab="files"]`, chromedp.ByQuery), waitForFileScreen("/files"),
						chromedp.Poll(`!document.querySelector('a.gfm-back') && document.body.innerText.includes('Thoughts for later.md') && document.body.innerText.includes('A little space for good ideas')`, nil), shot("files-"+scheme),
						chromedp.Click(`a[href="/notes/`+note.ID+`/files/`+file.Handle+`"]`, chromedp.ByQuery), waitForFileScreen("/notes/"+note.ID+"/files/"+file.Handle),
						chromedp.Poll(`document.querySelector('[data-gfw-file-preview] h1')?.textContent === 'Thoughts for later.md' && document.querySelector('[data-gfw-file-preview] pre')?.textContent.includes('Good ideas need a little room to grow.')`, nil), shot("file-detail-"+scheme))
					check(chromedp.Click(`[data-gfw-tab="files"]`, chromedp.ByQuery), waitForFileScreen("/files"),
						chromedp.Click(`a[href="/notes/`+note.ID+`/files/`+photoFile.Handle+`"]`, chromedp.ByQuery), waitForFileScreen("/notes/"+note.ID+"/files/"+photoFile.Handle),
						chromedp.Poll(`document.querySelector('[data-gfw-file-preview] img')?.naturalWidth === 800`, nil), shot("photo-detail-"+scheme))
					assertDeviceTraffic(t, traffic)
				})
			}
		})
	}
}
