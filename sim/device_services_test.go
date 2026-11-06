package sim

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

func TestNotesDeviceServicesProtocolWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			note, err := service.Save(userContext, "", "Device service fixture", "Saved baseline")
			if err != nil {
				t.Fatal(err)
			}
			check := deviceBrowserCheck(t, ctx)
			check(chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(fmt.Sprintf(`
					window.__deviceRequests = [];
					window.__deviceOutcome = 'completed';
					window.__gofastrDeviceServices = {
						capabilities: async () => Object.fromEntries(['notifications/v1', 'sharing/v1', 'clipboard/v1', 'audio/v1'].map(key => [key, 'available'])),
						perform: async request => {
							window.__deviceRequests.push(JSON.parse(JSON.stringify(request)));
							if (request.audio && window.__deviceOutcome === 'completed') return {
								status: 'completed', durationMillis: 1000,
								file: { name: 'voice-fixture.wav', mediaType: 'audio/wav', data: %s }
							};
							return { status: window.__deviceOutcome, ...(request.notification ? { identifier: request.notification.identifier } : {}) };
						}
					};`, mustJSON(deviceAudioFixture()))).Do(ctx)
				return err
			}), chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
				setMobileField(`input[name="title"]`, "Unsaved service title"),
				setMobileField(`textarea[name="body"]`, "Private unsaved text café"),
				setMobileField(`input[name="reminder_date"]`, "2099-12-31"),
				setMobileField(`input[name="reminder_time"]`, "09:30"))
			perform := func(label string) {
				t.Helper()
				check(clickMobileAction(label), chromedp.WaitVisible(`[data-gfw-service-perform]`, chromedp.ByQuery),
					chromedp.Click(`[data-gfw-service-perform]`, chromedp.ByQuery),
					chromedp.Poll(`!document.querySelector('[data-gfw-service]')`, nil))
			}
			perform("Share note")
			check(chromedp.Poll(`window.__deviceRequests.length === 1 && window.__deviceRequests[0].share.text === 'Unsaved service title\n\nPrivate unsaved text café' && new TextDecoder().decode(Uint8Array.from(atob(window.__deviceRequests[0].share.files[0].data), character => character.charCodeAt(0))) === window.__deviceRequests[0].share.text`, nil))
			perform("Copy note")
			check(chromedp.Poll(`window.__deviceRequests.length === 2 && window.__deviceRequests[1].clipboard.text === window.__deviceRequests[0].share.text`, nil),
				chromedp.Evaluate(`window.__deviceOutcome = 'denied'`, nil))
			perform("Copy note")
			check(chromedp.Poll(`document.body.innerText.includes('Device access denied') && window.__deviceRequests.length === 3`, nil),
				chromedp.Evaluate(`window.__deviceOutcome = 'completed'`, nil))
			perform("Schedule reminder")
			check(chromedp.Poll(`window.__deviceRequests.length === 4 && window.__deviceRequests[3].notification.date === '2099-12-31' && window.__deviceRequests[3].notification.time === '09:30'`, nil))
			perform("Check reminder status")
			perform("Cancel reminder")
			check(chromedp.Poll(`window.__deviceRequests.length === 6 && window.__deviceRequests[4].notification.operation === 'status' && window.__deviceRequests[5].notification.operation === 'cancel' && document.querySelector('textarea[name="body"]').value === 'Private unsaved text café'`, nil))
			perform("Record audio")
			check(chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.WaitVisible(`[data-gfw-audio-playback]`, chromedp.ByQuery))
			var recordingURL string
			check(chromedp.Location(&recordingURL), chromedp.Poll(`window.__deviceRequests.length === 7 && !!document.querySelector('[data-gfw-audio-playback]').src`, nil))
			check(chromedp.Navigate(recordingURL), chromedp.WaitVisible(`[data-gfw-audio-playback]`, chromedp.ByQuery))
			perform("Share file")
			check(chromedp.Poll(fmt.Sprintf(`window.__deviceRequests.length === 1 && window.__deviceRequests[0].share.files[0].data === %s && window.__deviceRequests[0].share.files[0].mediaType === 'audio/wav'`, mustJSON(deviceAudioFixture())), nil),
				chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
				chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Private unsaved text café' && document.querySelector('[data-gfw-attachments]').textContent.includes('voice-fixture.wav')`, nil))
			assertDeviceTraffic(t, traffic)
		})
	}
}

func TestNotesBrowserReminderUnsupported(t *testing.T) {
	ctx, service, userContext, traffic := deviceNotesBrowser(t, mobileui.IOS)
	note, err := service.Save(userContext, "", "Browser reminder", "Still only note metadata")
	if err != nil {
		t.Fatal(err)
	}
	check := deviceBrowserCheck(t, ctx)
	check(chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
		setMobileField(`input[name="reminder_date"]`, "2099-12-31"), setMobileField(`input[name="reminder_time"]`, "09:30"),
		clickMobileAction("Schedule reminder"), chromedp.WaitVisible(`[data-gfw-service]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfw-service]').textContent.includes('Scheduled reminders are unavailable in a plain browser')`, nil),
		chromedp.Click(`[data-gfw-service-perform]`, chromedp.ByQuery),
		chromedp.Poll(`!document.querySelector('[data-gfw-service]') && document.body.innerText.includes('unsupported in this host')`, nil))
	assertDeviceTraffic(t, traffic)
}

func TestNotesAudioRecordingBrowser(t *testing.T) {
	ctx, service, userContext, traffic := deviceNotesBrowser(t, mobileui.IOS,
		chromedp.Flag("use-fake-ui-for-media-stream", true), chromedp.Flag("use-fake-device-for-media-stream", true))
	note, err := service.Save(userContext, "", "Browser audio fixture", "Chromium supplies a synthetic microphone.")
	if err != nil {
		t.Fatal(err)
	}
	check := deviceBrowserCheck(t, ctx)
	check(chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
		clickMobileAction("Record audio"), chromedp.WaitVisible(`[data-gfw-audio-record]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-audio-record]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('[data-gfw-audio-stop]') && !document.querySelector('[data-gfw-audio-stop]').hidden && !document.querySelector('[data-gfw-audio-stop]').disabled`, nil),
		chromedp.Evaluate(`window.__recordingStarted = performance.now()`, nil),
		chromedp.Poll(`performance.now() - window.__recordingStarted > 400`, nil),
		chromedp.Click(`[data-gfw-audio-stop]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfw-audio-accept]')?.disabled === false`, nil),
		chromedp.Click(`[data-gfw-audio-accept]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-gfw-audio-playback]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-gfw-audio-playback]').load()`, nil),
		chromedp.Poll(`document.querySelector('[data-gfw-audio-playback]').readyState >= 2 && !document.querySelector('[data-gfw-audio-playback]').error`, nil))
	assertDeviceTraffic(t, traffic)
}

func assertDeviceTraffic(t *testing.T, traffic *stats) {
	t.Helper()
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Fatalf("device bridge errors: %s", strings.Join(traffic.errs, "; "))
	}
}

func TestNotesLivePagingWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			for index := 0; index < 235; index++ {
				if _, err := service.Save(userContext, "", fmt.Sprintf("Paged seed %03d", index), "Owner-scoped repository paging fixture"); err != nil {
					t.Fatal(err)
				}
			}
			check := deviceBrowserCheck(t, ctx)
			check(chromedp.Navigate(origin+"/notes"), waitForFileScreen("/notes"),
				chromedp.Poll(`document.querySelectorAll('a.gfm-row').length === 10 && !!document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
				chromedp.Evaluate(`window.__deviceSeen = new Set(Array.from(document.querySelectorAll('a.gfm-row'), row => row.textContent.match(/Paged seed \d+/)?.[0]).filter(Boolean))`, nil))
			for batch := 0; batch < 20; batch++ {
				check(chromedp.WaitVisible(`[data-gfw-load-more]`, chromedp.ByQuery),
					chromedp.Evaluate(`window.__deviceBeforePage = document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
					chromedp.Click(`[data-gfw-load-more]`, chromedp.ByQuery),
					chromedp.Poll(`!!document.querySelector('[data-gfw-screen]').dataset.gfwInstance && document.querySelector('[data-gfw-screen]').dataset.gfwInstance !== window.__deviceBeforePage`, nil),
					chromedp.Evaluate(`Array.from(document.querySelectorAll('a.gfm-row'), row => row.textContent.match(/Paged seed \d+/)?.[0]).filter(Boolean).forEach(title => window.__deviceSeen.add(title))`, nil))
			}
			check(chromedp.Poll(`window.__deviceSeen.size >= 210`, nil),
				chromedp.Navigate(origin+"/notes?view=active&tag=&order=title&search=&page=1"), waitForFileScreen("/notes?view=active&tag=&order=title&search=&page=1"),
				chromedp.Poll(`document.querySelectorAll('a.gfm-row').length === 10 && !!document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
				chromedp.Evaluate(`(() => {
					const selected = document.querySelector('[data-gfw-select]');
					selected.click();
					window.__deviceSelected = selected.dataset.gfwSelect;
				})()`, nil),
				chromedp.Poll(`document.querySelector('[data-gfw-select="' + window.__deviceSelected + '"]').checked`, nil))
			check(chromedp.Evaluate(`(async () => {
				const send = async command => {
					const response = await window.__gofastr_transport('/_mobile/actions', {
						method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoFastr-Mobile-Protocol': '1', ...(document.querySelector('[name="_csrf"]') ? { 'X-CSRF-Token': document.querySelector('[name="_csrf"]').value } : {}) },
						body: JSON.stringify({ version: 1, revision: 0, ...command })
					});
					const reply = await response.json();
					if (!response.ok || reply.error) throw new Error(reply.error?.message || 'The independent renderer request failed.');
					return reply;
				};
				const capabilities = Object.fromEntries(['navigation/v1', 'editor/v1', 'drafts/v1', 'presentation/v1', 'files/v1', 'photos/v1', 'sharing/v1', 'clipboard/v1', 'notifications/v1', 'audio/v1', 'live/v1', 'gofastr.media/v1'].map(key => [key, 'available']));
				const { connection } = await send({ kind: 'hello', hello: { minimum: 1, maximum: 1, renderer: 'webview', capabilities } });
				const { instance } = await send({ kind: 'open', connection, route: '/notes/new' });
				const command = { connection, instance, requestId: 'independent-live-save', action: 'save', values: { title: '000 Live added beyond initial snapshot', body: 'An independent renderer command invalidates the visible collection.' } };
				await send({ ...command, kind: 'action' });
				await send({ ...command, kind: 'complete' });
				await send({ kind: 'close', connection, instance, values: {} });
			})()`, nil, func(parameters *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
				return parameters.WithAwaitPromise(true)
			}),
				chromedp.Poll(`Array.from(document.querySelectorAll('a.gfm-row')).some(row => row.textContent.includes('Live added beyond initial snapshot'))`, nil, chromedp.WithPollingTimeout(15*time.Second)),
				chromedp.Poll(`document.querySelector('[data-gfw-select="' + window.__deviceSelected + '"]')?.checked === true`, nil),
				chromedp.WaitVisible(`[data-gfw-live-cancel]`, chromedp.ByQuery),
				chromedp.Click(`[data-gfw-live-cancel]`, chromedp.ByQuery),
				chromedp.Poll(`document.querySelector('[data-gfw-screen]').dataset.gfwLiveState === 'cancelled'`, nil),
				chromedp.Evaluate(`window.__deviceOriginalPort = window.__simPort.onmessage;
					window.__simPort.onmessage = event => {
						const request = JSON.parse(event.data);
						if (request.method === 'POST' && new URL(request.url).pathname === '/_mobile/actions' && JSON.parse(atob(request.body || '')).kind === 'open') {
							window.__simPort.postMessage(JSON.stringify({ id: request.id, error: 'Read outage fixture' }));
						} else window.__deviceOriginalPort(event);
					}`, nil),
				chromedp.Click(`[data-gfw-live-retry]`, chromedp.ByQuery),
				chromedp.Poll(`document.querySelector('[data-gfw-screen]').dataset.gfwLiveState === 'error' && document.querySelector('[data-gfw-live-status]').textContent.includes('Read outage fixture')`, nil),
				chromedp.Evaluate(`window.__simPort.onmessage = window.__deviceOriginalPort`, nil),
				chromedp.Click(`[data-gfw-live-retry]`, chromedp.ByQuery),
				chromedp.Poll(`document.querySelector('[data-gfw-screen]').dataset.gfwLiveState === 'watching' && document.querySelector('[data-gfw-select="' + window.__deviceSelected + '"]')?.checked === true`, nil))
			traffic.mu.Lock()
			defer traffic.mu.Unlock()
			if len(traffic.errs) != 0 {
				t.Fatalf("live collection bridge errors: %v", traffic.errs)
			}
		})
	}
}

func deviceNotesBrowser(t *testing.T, platform mobileui.Platform, options ...chromedp.ExecAllocatorOption) (context.Context, *notes.Service, context.Context, *stats) {
	t.Helper()
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	app, host, err := notesapp.BuildScreensWithPlatform(directory, origin, platform)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	user, err := localapp.LoadOrMint(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android, options...)
	return ctx, notesapp.Service(app), handler.SetUser(context.Background(), user), traffic
}

func deviceBrowserCheck(t *testing.T, ctx context.Context) func(...chromedp.Action) {
	t.Helper()
	stage := 0
	return func(actions ...chromedp.Action) {
		t.Helper()
		stage++
		stageContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := chromedp.Run(stageContext, actions...); err != nil {
			var body, location, details string
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location),
				chromedp.Evaluate(`JSON.stringify({ screens: Array.from(document.querySelectorAll('[data-gfw-screen]'), root => ({ hidden: root.hidden, instance: root.dataset.gfwInstance, route: JSON.parse(root.dataset.gfwConfig || '{}').route })), modules: Object.keys(window.__gofastr?.loadedModules || {}), transport: typeof window.__gofastr_transport, rows: document.querySelectorAll('a.gfm-row').length })`, &details))
			t.Fatalf("device service stage %d: %v\n%s\n%s\n%s", stage, err, location, details, body)
		}
	}
}

func deviceAudioFixture() string {
	const samples = 8000
	data := make([]byte, 44+samples*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], samples)
	binary.LittleEndian.PutUint32(data[28:], samples*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], samples*2)
	return base64.StdEncoding.EncodeToString(data)
}
