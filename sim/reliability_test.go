package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

func TestMobileHistoryWaitsForDurableDraft(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			note, err := service.Save(userContext, "", "History draft", "Saved body")
			if err != nil {
				t.Fatal(err)
			}
			check := deviceBrowserCheck(t, ctx)
			t.Cleanup(func() {
				if t.Failed() {
					var diagnostic string
					_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({commands: window.__historyCommands, leave: window.__historyLeave, state: history.state, editor: document.querySelector('[data-gfw-screen]')?.dataset.gfwEditor})`, &diagnostic))
					t.Log(diagnostic)
				}
			})
			check(chromedp.Navigate(origin+"/notes"), waitForFileScreen("/notes"),
				chromedp.Click(`a[href="/notes/`+note.ID+`"]`, chromedp.ByQuery), waitForFileScreen("/notes/"+note.ID),
				chromedp.Evaluate(`(() => {
					window.__historyRoot = document.querySelector('[data-gfw-screen]');
					window.__historyOriginal = window.__simPort.onmessage;
					window.__historyCommands = [];
					window.__simPort.onmessage = event => {
						const message = JSON.parse(event.data);
						if (message.method === 'POST' && new URL(message.url).pathname === '/_mobile/actions') {
							const command = JSON.parse(atob(message.body || ''));
							window.__historyCommands.push(command.kind);
							if (command.kind === 'leave') window.__historyLeave = command;
							if (command.kind === 'leave' && command.choice === 'keep' && !window.__releaseHistory) {
								window.__heldHistoryCheckpoint = event;
								return;
							}
						}
						window.__historyOriginal(event);
					};
					const body = document.querySelector('textarea[name="body"]');
					body.value = 'Durable history draft';
					body.dispatchEvent(new Event('input', { bubbles: true }));
					history.back();
				})()`, nil),
				chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Poll(`!!window.__heldHistoryCheckpoint && document.querySelector('[data-gfw-screen]') === window.__historyRoot`, nil, chromedp.WithPollingTimeout(2*time.Second)),
				chromedp.Evaluate(`window.__releaseHistory = true; window.__historyOriginal(window.__heldHistoryCheckpoint)`, nil),
				waitForFileScreen("/notes"), chromedp.Evaluate(`sessionStorage.clear(); history.forward()`, nil),
				waitForFileScreen("/notes/"+note.ID),
				chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Durable history draft'`, nil))
			check(
				chromedp.Evaluate(`window.__historyRoot = document.querySelector('[data-gfw-screen]'); history.back()`, nil),
				chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="cancel"]`, chromedp.ByQuery),
				chromedp.Click(`dialog[open] [data-cui-confirm-part="cancel"]`, chromedp.ByQuery),
				chromedp.Poll(`!document.querySelector('dialog') && document.querySelector('[data-gfw-screen]') === window.__historyRoot`, nil))
			check(
				chromedp.Evaluate(`window.__releaseHistory = false; window.__heldHistoryCheckpoint = null; history.back()`, nil),
				chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Poll(`!!window.__heldHistoryCheckpoint`, nil),
				setMobileField(`textarea[name="body"]`, "Newer history draft"),
				chromedp.Evaluate(`window.__releaseHistory = true; window.__historyOriginal(window.__heldHistoryCheckpoint)`, nil),
				chromedp.Poll(`document.querySelector('[data-gfw-form]').dataset.gfwError === 'true' && document.querySelector('[data-gfw-screen]') === window.__historyRoot && document.querySelector('textarea[name="body"]').value === 'Newer history draft'`, nil))
			check(
				chromedp.Evaluate(`history.back()`, nil),
				chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
				waitForFileScreen("/notes"))
			var commands []string
			check(chromedp.Evaluate(`window.__simPort.onmessage = window.__historyOriginal; window.__historyCommands`, &commands))
			for _, command := range commands {
				if command == "action" || command == "respond" || command == "service-result" || command == "file-result" {
					t.Fatalf("history leave replayed a mutation: %v", commands)
				}
			}
			persisted, err := service.Get(userContext, note.ID)
			if err != nil || persisted.Body != "Saved body" {
				t.Fatalf("keeping a history draft saved the note: %+v, %v", persisted, err)
			}
			assertDeviceTraffic(t, traffic)
		})
	}
}

func TestMobileBackgroundCheckpointSurvivesReload(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			note, err := service.Save(userContext, "", "Background draft", "Saved body")
			if err != nil {
				t.Fatal(err)
			}
			check := deviceBrowserCheck(t, ctx)
			check(chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
				chromedp.Evaluate(`(() => {
					const body = document.querySelector('textarea[name="body"]');
					body.value = 'Background checkpoint before debounce';
					body.dispatchEvent(new Event('input', { bubbles: true }));
					Object.defineProperty(document, 'hidden', { configurable: true, value: true });
					document.dispatchEvent(new Event('visibilitychange'));
				})()`, nil),
				chromedp.Poll(`(() => { const editor = JSON.parse(document.querySelector('[data-gfw-screen]').dataset.gfwEditor || '{}'); return editor.revision === 1 && editor.dirty && editor.draftStored; })()`, nil),
				chromedp.Evaluate(`sessionStorage.clear(); document.querySelector('[data-gfw-form]').dispatchEvent(new CustomEvent('gofastr:formresult', { bubbles: true, detail: { ok: true } }))`, nil),
				chromedp.Reload(), waitForFileScreen("/notes/"+note.ID),
				chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Background checkpoint before debounce'`, nil))
			persisted, err := service.Get(userContext, note.ID)
			if err != nil || persisted.Body != "Saved body" {
				t.Fatalf("backgrounding saved the note instead of its draft: %+v, %v", persisted, err)
			}
			assertDeviceTraffic(t, traffic)
		})
	}
}

func TestMobileBackgroundCheckpointWaitsForDiscard(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			note, err := service.Save(userContext, "", "Discard checkpoint", "Saved body")
			if err != nil {
				t.Fatal(err)
			}
			check := deviceBrowserCheck(t, ctx)
			check(chromedp.Navigate(origin+"/notes/"+note.ID), waitForFileScreen("/notes/"+note.ID),
				chromedp.Evaluate(`(() => {
					window.confirm = () => true;
					window.__discardOriginal = window.__simPort.onmessage;
					window.__discardCommands = [];
					window.__simPort.onmessage = event => {
						const message = JSON.parse(event.data);
						if (message.method === 'POST' && new URL(message.url).pathname === '/_mobile/actions') {
							const command = JSON.parse(atob(message.body || ''));
							window.__discardCommands.push(command);
							if (command.kind === 'leave' && command.choice === 'discard' && !window.__releaseDiscard) {
								window.__heldDiscard = event;
								return;
							}
						}
						window.__discardOriginal(event);
					};
					const body = document.querySelector('textarea[name="body"]');
					body.value = 'Do not resurrect this discarded buffer';
					body.dispatchEvent(new Event('input', { bubbles: true }));
					document.querySelector('[data-gfw-discard]').click();
				})()`, nil),
				chromedp.Poll(`!!window.__heldDiscard`, nil),
				chromedp.Evaluate(`Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange'))`, nil),
				chromedp.Poll(`document.querySelector('[data-gfw-form]').getAttribute('aria-busy') === 'true'`, nil))
			var earlyCheckpoints int
			check(chromedp.Evaluate(`window.__discardCommands.filter(command => command.kind === 'checkpoint').length`, &earlyCheckpoints))
			if earlyCheckpoints != 0 {
				t.Fatalf("background checkpoint overtook discard: %d", earlyCheckpoints)
			}
			check(chromedp.Evaluate(`window.__releaseDiscard = true; window.__discardOriginal(window.__heldDiscard)`, nil),
				chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Saved body' && !document.querySelector('[data-gfw-form]').hasAttribute('aria-busy')`, nil),
				chromedp.Poll(`window.__discardCommands.some(command => command.kind === 'checkpoint' && command.values.body === 'Saved body')`, nil),
				chromedp.Poll(`JSON.parse(document.querySelector('[data-gfw-screen]').dataset.gfwEditor).dirty === false`, nil),
				chromedp.Evaluate(`sessionStorage.clear()`, nil), chromedp.Reload(), waitForFileScreen("/notes/"+note.ID),
				chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Saved body'`, nil))
			persisted, err := service.Get(userContext, note.ID)
			if err != nil || persisted.Body != "Saved body" {
				t.Fatalf("discard/background changed saved data: %+v, %v", persisted, err)
			}
			assertDeviceTraffic(t, traffic)
		})
	}
}

func TestRetainedCollectionRecoversReadWithoutReplayingActions(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			ctx, service, userContext, traffic := deviceNotesBrowser(t, platform)
			note, err := service.Save(userContext, "", "Retained collection", "Saved content must not change")
			if err != nil {
				t.Fatal(err)
			}
			check := deviceBrowserCheck(t, ctx)
			check(chromedp.Navigate(origin+"/notes"), waitForFileScreen("/notes"),
				chromedp.Click(`[data-gfw-selection-toggle]`, chromedp.ByQuery),
				chromedp.Click(`[data-gfw-select="`+note.ID+`"]`, chromedp.ByQuery),
				chromedp.Evaluate(`(() => {
					window.__recoveryCommands = [];
					window.__retainedRoot = document.querySelector('[data-gfw-screen]');
					window.__retainedInstance = window.__retainedRoot.dataset.gfwInstance;
					window.__recoveryOriginal = window.__simPort.onmessage;
					window.__retireOnce = true;
					window.__simPort.onmessage = event => {
						const message = JSON.parse(event.data);
						if (message.method === 'POST' && new URL(message.url).pathname === '/_mobile/actions') {
							const command = JSON.parse(atob(message.body || ''));
							window.__recoveryCommands.push(command.kind);
							if (command.kind === 'watch' && window.__retireOnce) {
								window.__retireOnce = false;
								const headers = { 'Content-Type': 'application/json', 'X-GoFastr-Mobile-Protocol': '1' };
								const csrf = window.__retainedRoot.querySelector('[name="_csrf"]');
								if (csrf) headers['X-CSRF-Token'] = csrf.value;
								const request = body => window.__gofastr_transport(new Request(new URL('/_mobile/actions', location.href), {
									method: 'POST', headers, body: JSON.stringify({ version: 1, connection: command.connection, ...body })
								})).then(reply => reply.json());
								void request({ kind: 'open', route: '/settings', values: {} }).then(reply => {
									window.__retiredReply = reply;
									if (reply.instance) return request({ kind: 'close', instance: reply.instance });
								}).catch(error => { window.__retirementError = error.message; });
								}
							}
						window.__recoveryOriginal(event);
					};
					document.dispatchEvent(new Event('visibilitychange'));
				})()`, nil),
				chromedp.Poll(`window.__retiredReply?.instance && !window.__retiredReply.error`, nil),
				chromedp.Poll(`document.querySelector('[data-gfw-screen]').dataset.gfwInstance !== window.__retainedInstance && ['idle', 'watching'].includes(window.__retainedRoot.dataset.gfwLiveState)`, nil),
				chromedp.Poll(fmt.Sprintf(`document.querySelector('[data-gfw-screen]') === window.__retainedRoot && document.querySelector('[data-gfw-select="%s"]').checked`, note.ID), nil))
			var commands []string
			check(chromedp.Evaluate(`window.__simPort.onmessage = window.__recoveryOriginal; window.__recoveryCommands`, &commands))
			for _, command := range commands {
				switch command {
				case "watch", "open", "close":
				default:
					t.Fatalf("read recovery issued %q: %v", command, commands)
				}
			}
			persisted, err := service.Get(userContext, note.ID)
			if err != nil || persisted.Title != note.Title || persisted.Body != note.Body {
				t.Fatalf("read recovery changed the saved note: %+v, %v", persisted, err)
			}
			assertDeviceTraffic(t, traffic)
		})
	}
}
