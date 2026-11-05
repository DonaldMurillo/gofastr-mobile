const assert = require('node:assert/strict');
const { test } = require('node:test');
const { harness, connected, flush, plain } = require('./test-dom.cjs');

const request = (kind, value) => ({ id: 'declared-go-continuation', [kind]: value });
const audioFile = () => {
  const bytes = Buffer.alloc(80);
  Buffer.from([26, 69, 223, 163]).copy(bytes);
  bytes.write('webm', 8); bytes.write('OpusHead', 16); Buffer.from([24, 83, 128, 103]).copy(bytes, 32);
  return { name: 'recording.webm', mediaType: 'audio/webm', data: bytes.toString('base64') };
};
const microphone = fixture => {
  const tracks = [{ stopped: 0, stop() { this.stopped++; } }];
  let prompts = 0;
  const recorders = [];
  fixture.context.navigator.mediaDevices = { getUserMedia: async () => { prompts++; return { getTracks: () => tracks }; } };
  fixture.window.MediaRecorder = class {
    static isTypeSupported(type) { return type === 'audio/webm;codecs=opus'; }
    state = 'inactive';
    mimeType = 'audio/webm;codecs=opus';
    constructor() { recorders.push(this); }
    start() { this.state = 'recording'; }
    pause() { this.state = 'paused'; }
    resume() { this.state = 'recording'; }
    stop() {
      this.state = 'inactive';
      this.ondataavailable?.({ data: new Blob([Buffer.from(audioFile().data, 'base64')]) });
      this.onstop?.();
    }
  };
  return { tracks, recorders, get prompts() { return prompts; } };
};

test('capability discovery is truthful and never requests permission or writes on load', async () => {
  let mic;
  const fixture = harness({ setup(value) { mic = microphone(value); value.context.navigator.clipboard = { writeText: () => assert.fail('Not explicit') }; } });
  const facts = plain(await fixture.adapter.capabilities());
  assert.equal(facts['notifications/v1'], 'unavailable');
  assert.equal(facts['sharing/v1'], 'fallback');
  assert.equal(facts['clipboard/v1'], 'fallback');
  assert.equal(facts['audio/v1'], 'fallback');
  assert.equal(facts['live/v1'], 'available');
  assert.equal(mic.prompts, 0);
  assert.equal(fixture.objects.size, 0);
});

test('reply-capable WK host uses decoded canonical messages and explicit one-shot perform', async () => {
  const messages = [], outcomes = [];
  const fixture = harness({ setup({ window }) {
    window.webkit = { messageHandlers: { gofastrServices: { async postMessage(message) {
      messages.push(plain(message));
      return message.kind === 'capabilities' ? { 'notifications/v1': 'available', 'audio/v1': 'available', bad: 'available' } : { status: 'completed', identifier: 'reminder-1', permission: 'authorized' };
    } } } };
  } });
  assert.equal((await fixture.adapter.capabilities())['notifications/v1'], 'available');
  const definition = request('notification', { operation: 'status', identifier: 'reminder-1' });
  const dialog = fixture.open(definition, async result => outcomes.push(plain(result)));
  assert.equal(messages.length, 1);
  dialog.querySelector('[data-gfw-service-perform]').click();
  dialog.querySelector('[data-gfw-service-perform]').click();
  await flush();
  assert.deepEqual(messages, [{ kind: 'capabilities' }, { kind: 'perform', request: definition }]);
  assert.deepEqual(outcomes, [{ status: 'completed', identifier: 'reminder-1', permission: 'authorized' }]);
  assert.equal(fixture.window.__gofastr_foreground.has(), false);
  assert.throws(() => fixture.open(definition), /not repeated/);
});

test('injected host rejects string/foreign byte results without replaying a device operation', async () => {
  for (const result of ['{"status":"completed"}', { status: 'completed', file: audioFile() }, { status: 'unknown' }]) {
    let calls = 0, observed;
    const fixture = harness({ setup({ window }) { window.__gofastrDeviceServices = { async capabilities() { return {}; }, async perform() { calls++; return result; } }; } });
    const dialog = fixture.open(undefined, async outcome => { observed = outcome; });
    dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
    assert.equal(calls, 1);
    assert.equal(observed.status, 'failed');
    assert.equal(observed.file, undefined);
  }
});

test('unresponsive native capability discovery is bounded and does not call perform', async () => {
  const fixture = harness({ setup({ window }) { window.__gofastrDeviceServices = { capabilities: () => new Promise(() => {}), perform() { assert.fail('No load-time OS call'); } }; } });
  const discovery = fixture.adapter.capabilities();
  assert.equal(fixture.tick(2000), true);
  const facts = plain(await discovery);
  assert.equal(facts['notifications/v1'], 'unavailable');
  assert.equal(facts['audio/v1'], 'unavailable');
  assert.equal(facts['live/v1'], 'available');
});

test('plain-browser reminders report unsupported without timers or notification APIs', async () => {
  let outcome;
  const fixture = harness();
  const dialog = fixture.open(request('notification', { operation: 'schedule', date: '2099-01-01', time: '12:00' }), async value => { outcome = value; });
  assert.equal(fixture.timers.size, 0);
  assert.match(dialog.querySelector('[data-gfw-service-status]').textContent, /No timer/);
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.equal(outcome.status, 'unsupported');
});

test('clipboard is write-only, explicit, once, and preserves local editor text', async () => {
  const writes = [], outcomes = [];
  const fixture = harness({ setup({ context }) { context.navigator.clipboard = { async writeText(text) { writes.push(text); }, readText() { assert.fail('No clipboard reads'); } }; } });
  const editor = fixture.document.createElement('textarea'); editor.value = 'Unsaved local text'; fixture.root.append(editor);
  const dialog = fixture.open(undefined, async value => outcomes.push(value));
  assert.deepEqual(writes, []);
  assert.equal(dialog.querySelector('[data-gfw-service-text]').value, 'Private <text>');
  dialog.querySelector('[data-gfw-service-perform]').click(); dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.deepEqual(writes, ['Private <text>']);
  assert.equal(outcomes[0].status, 'completed');
  assert.equal(editor.value, 'Unsaved local text');
});

test('manual clipboard fallback and permission denial are not reported as copying', async () => {
  for (const supported of [false, true]) {
    let result;
    const fixture = harness({ setup({ context }) { if (supported) context.navigator.clipboard = { async writeText() { throw new Error('denied'); } }; } });
    const dialog = fixture.open(undefined, async value => { result = value; });
    dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
    assert.equal(result.status, supported ? 'denied' : 'unsupported');
  }
});

test('rich sharing preserves bytes and uses Web Share only after an explicit click', async () => {
  const calls = [];
  const fixture = harness({ setup({ context }) { context.navigator.canShare = () => true; context.navigator.share = async data => calls.push(data); } });
  const file = { name: 'note.txt', mediaType: 'text/plain', data: 'aGVsbG8=' };
  const dialog = fixture.open(request('share', { title: 'Note', text: 'Read this', files: [file] }));
  assert.equal(calls.length, 0);
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].files[0].name, file.name);
  assert.equal(await calls[0].files[0].text(), 'hello');
});

test('download fallback is honestly labelled, explicit, bounded, and cleans object URLs', async () => {
  let outcome;
  const fixture = harness();
  const dialog = fixture.open(request('share', { title: 'Note', files: [{ name: 'note.txt', mediaType: 'text/plain', data: 'aGVsbG8=' }] }), async value => { outcome = value; });
  assert.equal(fixture.document.downloads.length, 0);
  assert.match(dialog.querySelector('[data-gfw-service-status]').textContent, /not sharing/);
  dialog.querySelector('[data-gfw-service-download]').click();
  assert.equal(fixture.document.downloads[0].name, 'note.txt');
  assert.equal(await fixture.objects.values().next().value.text(), 'hello');
  fixture.tick(0);
  assert.equal(fixture.objects.size, 0);
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.equal(outcome.status, 'unsupported');
  assert.match(outcome.message, /not actual sharing/);
});

test('oversized and traversal share payloads never produce downloads', async () => {
  for (const files of [[{ name: '../private', mediaType: 'text/plain', data: 'aA==' }], Array.from({ length: 5 }, () => ({ name: 'note.txt', mediaType: 'text/plain', data: 'aA==' })),
    [{ name: 'large.txt', mediaType: 'text/plain', data: Buffer.alloc(1048577).toString('base64') }]]) {
    const fixture = harness(); let outcome;
    const dialog = fixture.open(request('share', { files }), async value => { outcome = value; });
    assert.equal(dialog.querySelector('[data-gfw-service-download]'), null);
    dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
    assert.equal(outcome.status, 'failed');
  }
});

test('services share a foreground owner with presentations and files', () => {
  const fixture = harness();
  fixture.open();
  assert.throws(() => fixture.window.__gofastr_presentations.open({ root: fixture.root, presentation: {}, presentationId: 'p' }), /foreground/);
  assert.throws(() => fixture.window.__gofastr_files.open({ root: fixture.root, files: {}, fileId: 'f' }), /modal/);
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.window.__gofastr_foreground.has(), false);
  fixture.window.__gofastr_foreground.claim({});
  assert.throws(() => fixture.open(undefined, undefined, 'different-id'), /foreground/);
});

test('uncertain settlement exposes completion-only recovery and never another perform', async () => {
  let copies = 0, writes = 0, reads = 0;
  const fixture = harness({ setup({ context }) { context.navigator.clipboard = { async writeText() { copies++; } }; } });
  const dialog = fixture.open(undefined, async () => { writes++; throw Object.assign(new Error('Unknown result'), { complete: async () => { reads++; } }); });
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.equal(dialog.querySelector('[data-gfw-service-cancel]').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-service-perform]').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-service-complete]').hidden, false);
  dialog.querySelector('[data-gfw-service-complete]').click(); await flush();
  assert.deepEqual([copies, writes, reads], [1, 1, 1]);
  assert.equal(dialog.isConnected, false);
});

test('service command settlement uses opaque identity and latest values/revision with no write replay', async () => {
  let actionId, copies = 0, serviceId, checks = 0;
  const fixture = await connected(async command => {
    if (command.kind === 'action') { actionId = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionId) return { serviceId: 'issued-private', response: { service: request('clipboard', { text: 'Copy' }) } };
    if (command.kind === 'service-result') { serviceId = command.requestId; throw new Error('Lost write response'); }
    if (command.kind === 'complete' && command.requestId === serviceId) { checks++; if (checks < 3) throw new Error('Lost completion'); return { response: { announcement: 'Observed once' } }; }
    return {};
  }, { setup({ context }) { context.navigator.clipboard = { async writeText() { copies++; } }; } });
  const dialog = await fixture.launch();
  fixture.field.value = 'Newer text'; fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  const write = fixture.commands.find(command => command.kind === 'service-result');
  assert.equal(write.serviceId, 'issued-private');
  assert.equal(write.values.title, 'Newer text');
  assert.equal(write.revision, 1);
  assert.notEqual(write.requestId, actionId);
  assert.equal(dialog.querySelector('[data-gfw-service-complete]').hidden, false);
  fixture.field.value = 'Newest during recovery'; fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  dialog.querySelector('[data-gfw-service-complete]').click(); await flush();
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  if (dialog.isConnected) { dialog.querySelector('[data-gfw-service-complete]').click(); await flush(); }
  assert.equal(fixture.commands.filter(command => command.kind === 'service-result').length, 1);
  assert.equal(copies, 1);
  assert.equal(fixture.field.value, 'Newest during recovery');
  assert.equal(fixture.status.textContent, 'Observed once');
});

test('service cancellation reaches Go and releases ownership before a chained presentation', async () => {
  let actionId, responseId;
  const fixture = await connected(async command => {
    if (command.kind === 'action') { actionId = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionId) return { serviceId: 'service-1', response: { service: request('clipboard', { text: 'Private' }) } };
    if (command.kind === 'service-result') { responseId = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === responseId) return { presentationId: 'presentation-2', response: { presentation: { id: 'next', kind: 'alert', title: 'Cancelled honestly', choices: [{ id: 'ok', label: 'OK' }] } } };
    return { response: {} };
  });
  const service = await fixture.launch();
  service.querySelector('[data-gfw-service-cancel]').click(); await flush();
  assert.equal(fixture.commands.find(command => command.kind === 'service-result').serviceResult.status, 'cancelled');
  assert.equal(service.isConnected, false);
  assert.ok(fixture.document.querySelector('[data-gfw-presentation]'));
  assert.equal(fixture.window.__gofastr_foreground.has(), true);
  fixture.document.querySelector('[data-gfw-choice="ok"]').click(); await flush();
  assert.equal(fixture.window.__gofastr_foreground.has(), false);
  assert.equal(fixture.commands.filter(command => command.kind === 'action').length, 1);
});

test('recorder is explicit and implements pause/resume/stop/preview/accept with bounded bytes', async () => {
  let mic, outcome;
  const fixture = harness({ setup(value) { mic = microphone(value); } });
  const dialog = fixture.open(request('audio', { title: 'Record', maxSeconds: 60, maxBytes: 1048576 }), async value => { outcome = value; });
  assert.equal(mic.prompts, 0);
  assert.equal(dialog.querySelector('[data-gfw-audio-preview]').hidden, true);
  dialog.querySelector('[data-gfw-audio-record]').click(); await flush();
  assert.equal(mic.prompts, 1);
  assert.equal(mic.recorders[0].state, 'recording');
  dialog.querySelector('[data-gfw-audio-pause]').click(); assert.equal(mic.recorders[0].state, 'paused');
  dialog.querySelector('[data-gfw-audio-resume]').click(); assert.equal(mic.recorders[0].state, 'recording');
  dialog.querySelector('[data-gfw-audio-stop]').click(); await flush();
  assert.ok(mic.tracks[0].stopped);
  assert.equal(dialog.querySelector('[data-gfw-audio-preview]').hidden, false);
  assert.equal(outcome, undefined);
  dialog.querySelector('[data-gfw-audio-accept]').click(); await flush();
  assert.equal(outcome.status, 'completed');
  assert.equal(outcome.file.mediaType, 'audio/webm');
  assert.ok(outcome.durationMillis > 0 && outcome.durationMillis <= 60000);
  assert.equal(fixture.objects.size, 0);
  assert.equal(mic.recorders[0].onstop, null);
});

test('audio cancellation, background interruption, departure and late microphone grants clean resources', async () => {
  for (const reason of ['cancel', 'background', 'departure', 'late']) {
    let mic, outcome, grant;
    const fixture = harness({ setup(value) {
      mic = microphone(value);
      if (reason === 'late') value.context.navigator.mediaDevices.getUserMedia = () => new Promise(resolve => { grant = resolve; });
    } });
    const dialog = fixture.open(request('audio', { title: 'Record' }), async value => { outcome = value; });
    dialog.querySelector('[data-gfw-audio-record]').click(); await flush();
    if (reason === 'background') { fixture.document.hidden = true; fixture.document.dispatchEvent({ type: 'visibilitychange' }); }
    else if (reason === 'departure') { fixture.root.remove(); fixture.mutate(); }
    else dialog.querySelector('[data-gfw-service-cancel]').click();
    if (grant) grant({ getTracks: () => mic.tracks });
    await flush();
    assert.ok(mic.tracks[0].stopped, reason);
    assert.equal(fixture.objects.size, 0, reason);
    if (reason !== 'departure') { assert.equal(outcome.status, 'cancelled'); assert.equal(outcome.file, undefined); }
  }
});

test('recorder hard time/byte limits never automatically accept a recording', async () => {
  for (const limit of ['seconds', 'bytes']) {
    let mic, outcome;
    const fixture = harness({ setup(value) { mic = microphone(value); } });
    const dialog = fixture.open(request('audio', { title: 'Record', maxSeconds: 1, maxBytes: limit === 'bytes' ? 16 : 1048576 }), async value => { outcome = value; });
    dialog.querySelector('[data-gfw-audio-record]').click(); await flush();
    assert.equal(fixture.tick(1000), true); await flush();
    assert.ok(mic.tracks[0].stopped);
    if (limit === 'bytes') { assert.equal(outcome.status, 'failed'); assert.equal(outcome.file, undefined); }
    else { assert.equal(outcome, undefined); assert.equal(dialog.querySelector('[data-gfw-audio-accept]').hidden, false); fixture.adapter.close(fixture.root); }
  }
});

test('cancelling an unaccepted preview revokes its URL and transfers no audio bytes', async () => {
  let outcome;
  const fixture = harness({ setup: microphone });
  const dialog = fixture.open(request('audio', { title: 'Record' }), async value => { outcome = value; });
  dialog.querySelector('[data-gfw-audio-record]').click(); await flush();
  dialog.querySelector('[data-gfw-audio-stop]').click(); await flush();
  assert.equal(fixture.objects.size, 1);
  dialog.querySelector('[data-gfw-service-cancel]').click(); await flush();
  assert.equal(outcome.status, 'cancelled');
  assert.equal(outcome.file, undefined);
  assert.equal(fixture.objects.size, 0);
});

test('unsupported insecure audio never asks for microphone access', async () => {
  let mic, outcome;
  const fixture = harness({ setup(value) { mic = microphone(value); value.window.isSecureContext = false; } });
  assert.equal((await fixture.adapter.capabilities())['audio/v1'], 'unavailable');
  const dialog = fixture.open(request('audio', { title: 'Record' }), async value => { outcome = value; });
  assert.equal(dialog.querySelector('[data-gfw-audio-record]'), null);
  dialog.querySelector('[data-gfw-service-perform]').click(); await flush();
  assert.equal(outcome.status, 'unsupported');
  assert.equal(mic.prompts, 0);
});

test('microphone track interruptions cancel rather than committing a partial recording', async () => {
  for (const event of ['onended', 'onmute']) {
    let mic, outcome;
    const fixture = harness({ setup(value) { mic = microphone(value); } });
    const dialog = fixture.open(request('audio', { title: 'Record' }), async value => { outcome = value; });
    dialog.querySelector('[data-gfw-audio-record]').click(); await flush();
    mic.tracks[0][event](); await flush();
    assert.equal(outcome.status, 'cancelled');
    assert.equal(outcome.file, undefined);
    assert.equal(mic.tracks[0].onended, null);
    assert.equal(mic.tracks[0].onmute, null);
    assert.equal(fixture.timers.size, 0);
  }
});

test('saved bounded audio has local native controls and releases playback on interruption/departure', () => {
  const fixture = harness();
  fixture.adapter.attachPlayback(fixture.screen, { id: 'saved-audio', name: 'Voice note', audio: { file: audioFile(), durationMillis: 1000 } });
  const player = fixture.document.querySelector('[data-gfw-audio-playback]');
  assert.equal(player.hasAttribute('controls'), true);
  assert.equal(player.getAttribute('preload'), 'none');
  assert.equal(fixture.objects.size, 1);
  fixture.mutate(); assert.equal(fixture.objects.size, 1);
  fixture.document.hidden = true; fixture.document.dispatchEvent({ type: 'visibilitychange' });
  assert.equal(fixture.objects.size, 0); assert.ok(player.pauses);
  fixture.document.hidden = false; fixture.document.dispatchEvent({ type: 'visibilitychange' });
  assert.equal(fixture.objects.size, 1);
  fixture.root.remove(); fixture.mutate(); assert.equal(fixture.objects.size, 0);
});
