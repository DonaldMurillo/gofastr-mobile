const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const { test } = require('node:test');
const { runInNewContext } = require('node:vm');

class Target {
  listeners = new Map();
  addEventListener(type, listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(listener);
  }
  dispatchEvent(event) {
    event.target ||= this;
    event.preventDefault ||= () => { event.defaultPrevented = true; };
    event.stopPropagation ||= () => {};
    event.stopImmediatePropagation ||= () => { event.stopped = true; };
    for (const listener of this.listeners.get(event.type) || []) {
      listener(event);
      if (event.stopped) break;
    }
    return !event.defaultPrevented;
  }
}

class Element extends Target {
  attributes = new Map();
  children = [];
  dataset = {};
  disabled = false;
  hidden = false;
  open = false;
  textContent = '';
  clicks = 0;
  constructor(document, tag) { super(); this.document = document; this.tagName = tag.toUpperCase(); }
  get isConnected() { return this === this.document.body || !!this.parent?.isConnected; }
  get id() { return this.getAttribute('id'); }
  get value() { return this.currentValue ?? this.getAttribute('value') ?? ''; }
  set value(value) { this.currentValue = value; }
  get type() { return this.getAttribute('type') || ''; }
  get download() { return this.getAttribute('download'); }
  set innerHTML(_) { throw new Error('Untrusted HTML must not execute'); }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    if (name.startsWith('data-')) this.dataset[name.slice(5).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())] = String(value);
  }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  hasAttribute(name) { return this.attributes.has(name); }
  removeAttribute(name) { this.attributes.delete(name); }
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter((child) => child !== this); this.parent = null; }
  contains(node) { return node === this || this.children.some((child) => child.contains(node)); }
  matches(selector) {
    return selector.split(',').some((part) => {
      const match = part.trim().match(/^([a-z][a-z0-9]*)?(?:\[([^=\]]+)(?:="([^"]*)")?\])?$/);
      return !!match && (!match[1] || this.tagName === match[1].toUpperCase()) &&
        (!match[2] || (this.hasAttribute(match[2]) && (match[3] === undefined || this.getAttribute(match[2]) === match[3])));
    });
  }
  closest(selector) { return this.matches(selector) ? this : this.parent?.closest(selector) || null; }
  querySelectorAll(selector) { return this.children.flatMap((child) => [...(child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]); }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  showModal() { this.open = true; }
  close() { this.open = false; }
  getBoundingClientRect() { return { left: 100, right: 400, top: 100, bottom: 500 }; }
  focus() { this.document.activeElement = this; this.document.dispatchEvent({ type: 'focusin', target: this }); }
  click() {
    if (this.disabled) return;
    this.clicks++;
    const event = { type: 'click', target: this, button: 0 };
    this.document.dispatchEvent(event);
    if (!event.stopped) this.dispatchEvent(event);
    if (!event.defaultPrevented && this.hasAttribute('data-gfw-file-download-link')) {
      const navigation = { type: 'navigate', cancelable: true, destination: { url: this.getAttribute('href') } };
      this.document.navigation.dispatchEvent(navigation);
      this.document.downloads.push(navigation);
    }
  }
}
class Form extends Element {}
const flush = async () => { for (let turn = 0; turn < 5; turn++) await new Promise((resolve) => setImmediate(resolve)); };
const plain = (value) => JSON.parse(JSON.stringify(value));
const request = (extra = {}) => ({ id: 'go-continuation-not-an-issued-id', pick: { title: 'Choose <plain text>', extensions: ['.txt'], maxBytes: 1048576 }, ...extra });
const exported = (extra = {}) => ({ id: 'export-go-key', export: { name: 'note.txt', mediaType: 'text/plain', data: 'aGVsbG8=', ...extra } });
const presentation = () => ({ id: 'go-presentation', kind: 'dialog', title: 'Choose', dismissible: true, choices: [{ id: 'done', label: 'Done' }] });

const photoRequest = (extra = {}) => ({ id: 'photo-go-key', photo: { title: 'Choose photo', source: 'library', maxBytes: 1048576, maxDimension: 2048, ...extra } });
const jpeg = (width = 40, height = 30, size = 32, orientation = 1) => {
  const frame = Buffer.from([0xff, 0xc0, 0, 17, 8, height >> 8, height & 255, width >> 8, width & 255, 3, 1, 0x11, 0, 2, 0x11, 0, 3, 0x11, 0]);
  const exif = Buffer.from([0xff, 0xe1, 0, 34, 69, 120, 105, 102, 0, 0, 73, 73, 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, orientation, 0, 0, 0, 0, 0, 0, 0]);
  const head = Buffer.concat([Buffer.from([0xff, 0xd8]), ...(orientation === 1 ? [] : [exif]), frame, Buffer.from([0xff, 0xda, 0, 2])]);
  return Buffer.concat([head, Buffer.alloc(Math.max(0, size - head.length - 2)), Buffer.from([0xff, 0xd9])]);
};
const png = (width = 40, height = 30) => {
  const bytes = Buffer.alloc(45);
  Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]).copy(bytes);
  bytes.writeUInt32BE(13, 8);
  bytes.write('IHDR', 12);
  bytes.writeUInt32BE(width, 16);
  bytes.writeUInt32BE(height, 20);
  bytes.write('IEND', 37);
  return bytes;
};
const photoPrimitives = (context, document, options) => {
  const decoded = [];
  const draws = [];
  const encodes = [];
  const canvases = [];
  const createElement = document.createElement;
  document.createElement = (tag) => {
    const node = createElement(tag);
    if (tag === 'canvas') {
      canvases.push(node);
      node.getContext = () => options.noContext ? null : { fillRect() {}, drawImage: (...args) => draws.push(args) };
      if (!options.noEncoder) node.toBlob = (callback, type, quality) => {
        encodes.push({ width: node.width, height: node.height, type, quality });
        const value = options.encode ? options.encode(node, quality, callback) : new Blob([jpeg(node.width, node.height)], { type });
        if (value !== undefined) callback(value);
      };
    }
    return node;
  };
  if (options.htmlImage) context.Image = class {
    naturalWidth = options.width || 40;
    naturalHeight = options.height || 30;
    set src(value) { this.currentSource = value; if (value && !options.pendingImage) setImmediate(() => this.onload?.()); }
  };
  else if (!options.noDecoder) context.createImageBitmap = async (source, settings) => {
    if (options.decodeError) throw Object.assign(new Error('Decode refused'), { name: options.decodeError });
    if (options.beforeDecode) await options.beforeDecode();
    const bitmap = { width: options.width || settings.resizeWidth, height: options.height || settings.resizeHeight, closed: 0, close() { this.closed++; } };
    decoded.push({ bitmap, source, settings: plain(settings) });
    return bitmap;
  };
  return { decoded, draws, encodes, canvases };
};

const harness = (photoOptions) => {
  const document = new Target();
  const created = [];
  document.createElement = (tag) => { const node = new (tag === 'form' ? Form : Element)(document, tag); created.push(node); return node; };
  document.body = document.createElement('body');
  document.documentElement = document.body;
  document.activeElement = document.body;
  document.querySelectorAll = (selector) => document.body.querySelectorAll(selector);
  document.querySelector = (selector) => document.body.querySelector(selector);
  const window = new Target();
  const navigations = [];
  const urls = [];
  const revoked = [];
  const timers = new Map();
  window.__gofastr = { loadedModules: {}, invalidate() {}, navigate: (...args) => navigations.push(args) };
  window.navigation = new Target();
  document.navigation = window.navigation;
  document.downloads = [];
  const context = { document, window, HTMLFormElement: Form, navigator: {}, crypto: { randomUUID }, Blob, Uint8Array, TextEncoder, atob, btoa,
    URL: class extends URL {
      static createObjectURL(blob) { const url = 'blob:runtime-generated-' + urls.length; urls.push({ url, blob }); return url; }
      static revokeObjectURL(url) { revoked.push(url); }
    },
    history: { state: { gfwAnnotated: true }, replaceState() { throw new Error('Files must not change history'); } },
    location: { href: 'https://app.test/items/42', protocol: 'https:', host: 'app.test' },
    MutationObserver: class { constructor(callback) { document.scan = callback; } observe() {} },
    setTimeout(callback, delay) { const timer = {}; timers.set(timer, { callback, delay }); return timer; }, clearTimeout(timer) { timers.delete(timer); },
    CustomEvent: class { constructor(type, options) { this.type = type; Object.assign(this, options); } } };
  const photos = photoOptions && photoPrimitives(context, document, photoOptions);
  for (const module of ['presentation.js', 'photos.js', 'files.js', 'components.js']) runInNewContext(readFileSync(join(__dirname, module), 'utf8'), context);
  const root = document.createElement('div');
  root.setAttribute('data-gfw-screen', '');
  const screen = document.createElement('section');
  screen.setAttribute('data-gfm-platform', 'ios');
  const opener = document.createElement('button');
  screen.append(opener);
  root.append(screen);
  document.body.append(root);
  opener.focus();
  return { context, document, window, root, screen, opener, created, urls, revoked, timers, navigations, photos, adapter: window.__gofastr_files,
    open(files = request(), respond = async () => {}) {
      window.__gofastr_files.open({ root, files, fileId: 'opaque-issued', opener, respond });
      return document.querySelector('[data-gfw-file]');
    } };
};

const select = (dialog, contents = 'hello', extra = {}) => {
  const blob = new Blob([contents]);
  const input = dialog.querySelector('[data-gfw-file-input]');
  input.files = [{ name: 'note.txt', type: 'text/plain', size: blob.size, slice: (...args) => blob.slice(...args), ...extra }];
  input.dispatchEvent({ type: 'change' });
};

const connected = async (files = request(), handler = async () => ({ response: {} }), photoOptions) => {
  const fixture = harness(photoOptions);
  const form = fixture.document.createElement('form');
  form.setAttribute('data-gfw-form', '');
  const field = fixture.document.createElement('input');
  field.setAttribute('data-gfw-field', 'title');
  field.setAttribute('type', 'text');
  field.value = field.defaultValue = 'Saved title';
  form.append(field);
  const live = fixture.document.createElement('p');
  live.setAttribute('data-gfw-status', '');
  fixture.screen.append(form, live);
  fixture.opener.setAttribute('form', 'gfw-form');
  fixture.opener.value = 'show';
  fixture.root.dataset.gfwConfig = JSON.stringify({ root: '/items/42', route: '/items/42', routes: [{ path: '/items/{id}', actions: ['show'] }] });
  const commands = [];
  let actionId;
  fixture.window.__gofastr_transport = async (_, options) => {
    const command = JSON.parse(options.body);
    commands.push(command);
    let reply;
    if (command.kind === 'hello') reply = { connection: 'connection', contract: {} };
    else if (command.kind === 'open') reply = { instance: 'screen-instance' };
    else if (command.kind === 'action') { actionId = command.requestId; reply = { response: {} }; }
    else if (command.kind === 'complete' && command.requestId === actionId) reply = { fileId: 'opaque-issued', response: { files } };
    else reply = await handler(command, fixture);
    return { ok: true, json: async () => ({ version: 1, revision: command.revision, ...reply }) };
  };
  runInNewContext(readFileSync(join(__dirname, 'webview.js'), 'utf8'), fixture.context);
  await flush();
  return { ...fixture, form, field, live, commands,
    async launch() { fixture.document.dispatchEvent({ type: 'submit', target: form, submitter: fixture.opener }); await flush(); return fixture.document.querySelector('[data-gfw-file]'); } };
};

test('picker is a labelled, themed, explicit single-file input and safe singleton', () => {
  const fixture = harness();
  const dialog = fixture.open();
  const input = dialog.querySelector('[data-gfw-file-input]');
  assert.equal(dialog.open, true);
  assert.equal(dialog.getAttribute('role'), 'dialog');
  assert.equal(dialog.getAttribute('aria-modal'), 'true');
  assert.equal(dialog.getAttribute('aria-labelledby'), dialog.querySelector('h2').id);
  assert.equal(dialog.getAttribute('aria-describedby'), dialog.querySelector('p').id);
  assert.equal(dialog.querySelector('h2').textContent, 'Choose <plain text>');
  assert.equal(fixture.screen.contains(dialog), true);
  assert.equal(input.type, 'file');
  assert.equal(input.hasAttribute('multiple'), false);
  assert.equal(input.getAttribute('accept'), '.txt');
  assert.equal(input.clicks, 0);
  assert.equal(fixture.document.activeElement, input);
  assert.throws(() => fixture.open(), /open modal/);
  assert.throws(() => fixture.window.__gofastr_presentations.open({ root: fixture.root, presentation: presentation() }), /open file/);
  runInNewContext(readFileSync(join(__dirname, 'files.js'), 'utf8'), fixture.context);
  assert.equal(fixture.adapter, fixture.window.__gofastr_files);
  fixture.adapter.close(fixture.root);
  fixture.window.__gofastr_presentations.open({ root: fixture.root, presentation: presentation() });
  assert.throws(() => fixture.open(), /open modal/);
});

test('Tab, Shift+Tab, Escape, focus containment and restoration preserve the opener', async () => {
  const fixture = harness();
  let result;
  const dialog = fixture.open(request(), async (value) => { result = plain(value); });
  const input = dialog.querySelector('input');
  const cancel = dialog.querySelector('[data-gfw-file-cancel]');
  input.dispatchEvent({ type: 'focus' });
  fixture.opener.focus();
  assert.equal(fixture.document.activeElement, input);
  dialog.dispatchEvent({ type: 'keydown', key: 'Tab', shiftKey: true });
  assert.equal(fixture.document.activeElement, cancel);
  dialog.dispatchEvent({ type: 'keydown', key: 'Tab' });
  assert.equal(fixture.document.activeElement, input);
  dialog.dispatchEvent({ type: 'keydown', key: 'Escape' });
  await flush();
  assert.deepEqual(result, { status: 'cancelled' });
  assert.equal(fixture.document.activeElement, fixture.opener);
  assert.equal(dialog.isConnected, false);
  fixture.opener.remove();
  fixture.open();
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.document.activeElement, fixture.root);
  assert.equal(fixture.root.hasAttribute('tabindex'), false);
});

test('picks transfer bounded bytes, not paths, and preserve case-insensitive extension policy', async () => {
  const fixture = harness();
  let result;
  let slice;
  const dialog = fixture.open(request({ pick: { title: 'Pick', maxBytes: 5, extensions: ['.TXT'] } }), async (value) => { result = plain(value); });
  select(dialog, 'hello', { name: 'Note.TxT', slice: (...args) => { slice = args; return new Blob(['hello']); } });
  await flush();
  assert.deepEqual(slice, [0, 6]);
  assert.deepEqual(result, { status: 'completed', file: { name: 'Note.TxT', mediaType: 'text/plain', data: 'aGVsbG8=' } });
  assert.equal(dialog.isConnected, false);
});

test('exactly 1 MiB and empty files work without an unbounded read', async () => {
  for (const size of [0, 1048576]) {
    const fixture = harness();
    let result;
    const dialog = fixture.open(request(), async (value) => { result = plain(value); });
    select(dialog, new Uint8Array(size), { type: '' });
    await flush();
    assert.equal(result.status, 'completed');
    assert.equal(Buffer.from(result.file.data, 'base64').length, size);
    assert.equal(result.file.mediaType, 'application/octet-stream');
  }
});

test('oversized, multiple, unsafe, mismatched and wrong-extension picks return no bytes', async () => {
  for (const extra of [{ size: 1048577 }, { name: '../secret.txt' }, { name: 'https://host/note.txt' }, { name: 'file:note.txt' },
    { name: 'note.exe' }, { name: 'note\u0000.txt' }, { size: -1 }, { size: 1.5 }, { size: 4 }]) {
    const fixture = harness();
    let result;
    const dialog = fixture.open(request(), async (value) => { result = plain(value); });
    select(dialog, 'hello', extra);
    await flush();
    assert.deepEqual(result, { status: 'denied' });
  }
  const fixture = harness();
  let result;
  const dialog = fixture.open(request(), async (value) => { result = plain(value); });
  const input = dialog.querySelector('input');
  input.files = [{ name: 'one.txt' }, { name: 'two.txt' }];
  input.dispatchEvent({ type: 'change' });
  await flush();
  assert.deepEqual(result, { status: 'denied' });
});

test('read failures classify permission, cancellation, unsupported and failure without bytes', async () => {
  for (const [name, expected] of [['NotAllowedError', 'denied'], ['SecurityError', 'denied'], ['AbortError', 'cancelled'],
    ['NotSupportedError', 'unsupported'], ['NotReadableError', 'failed']]) {
    const fixture = harness();
    let result;
    const dialog = fixture.open(request(), async (value) => { result = plain(value); });
    select(dialog, 'hello', { slice: () => ({ arrayBuffer: async () => { throw Object.assign(new Error(name), { name }); } }) });
    await flush();
    assert.deepEqual(result, { status: expected });
  }
});

test('picker dismissal and explicit cancellation report cancelled exactly once', async () => {
  for (const trigger of ['cancel', 'change', 'button', 'escape']) {
    const fixture = harness();
    const results = [];
    const dialog = fixture.open(request(), async (result) => results.push(plain(result)));
    if (trigger === 'button') dialog.querySelector('[data-gfw-file-cancel]').click();
    else if (trigger === 'escape') dialog.dispatchEvent({ type: 'keydown', key: 'Escape' });
    else dialog.querySelector('input').dispatchEvent({ type: trigger });
    dialog.querySelector('[data-gfw-file-cancel]').click();
    await flush();
    assert.deepEqual(results, [{ status: 'cancelled' }]);
  }
});

test('pending or detached reads cannot submit twice or report bytes after screen disposal', async () => {
  const fixture = harness();
  const results = [];
  let finish;
  const dialog = fixture.open(request(), async (result) => results.push(result));
  select(dialog, 'hello', { slice: () => ({ arrayBuffer: () => new Promise((resolve) => { finish = resolve; }) }) });
  assert.equal(dialog.getAttribute('aria-busy'), 'true');
  dialog.querySelector('input').dispatchEvent({ type: 'change' });
  dialog.querySelector('[data-gfw-file-cancel]').click();
  assert.equal(results.length, 0);
  fixture.root.remove();
  fixture.adapter.close(fixture.root, false);
  finish(new TextEncoder().encode('hello').buffer);
  await flush();
  assert.equal(results.length, 0);
});

test('export requires Download activation, uses only a Blob URL, revokes it and sends no bytes', async () => {
  const fixture = harness();
  let result;
  let finish;
  const dialog = fixture.open(exported(), (value) => { result = plain(value); return new Promise((resolve) => { finish = resolve; }); });
  assert.equal(fixture.urls.length, 0);
  dialog.querySelector('[data-gfw-file-download]').click();
  dialog.querySelector('[data-gfw-file-download]').click();
  assert.deepEqual(result, { status: 'completed' });
  assert.equal(fixture.urls.length, 1);
  assert.equal(await fixture.urls[0].blob.text(), 'hello');
  assert.equal(fixture.urls[0].blob.type, 'text/plain');
  const link = fixture.created.find((node) => node.hasAttribute('data-gfw-file-download-link'));
  assert.equal(link.download, 'note.txt');
  assert.equal(link.getAttribute('href'), fixture.urls[0].url);
  assert.equal(link.clicks, 1);
  assert.equal(fixture.document.downloads[0].defaultPrevented, undefined);
  assert.equal(link.isConnected, false);
  assert.match(dialog.querySelector('[data-gfw-file-status]').textContent, /Download initiated/);
  finish();
  await flush();
  for (const timer of fixture.timers.values()) timer.callback();
  assert.deepEqual(fixture.revoked, [fixture.urls[0].url]);
  assert.equal(fixture.navigations.length, 0);
});

test('exports accept the exact bound but refuse oversized, malformed base64 and URL/path names', async () => {
  for (const [file, expected] of [
    [exported({ data: '' }), 'completed'],
    [exported({ data: Buffer.alloc(1048576).toString('base64') }), 'completed'],
    [exported({ data: Buffer.alloc(1048577).toString('base64') }), 'failed'],
    [exported({ data: 'aG Vs' }), 'failed'], [exported({ data: 'Zh==' }), 'failed'], [exported({ data: 'data:text/plain,aA==' }), 'failed'],
    [exported({ name: '/tmp/note.txt' }), 'failed'], [exported({ name: 'https://host/note.txt' }), 'failed'],
    [exported({ name: 'C:\\note.txt' }), 'failed'], [exported({ mediaType: 'not a media type' }), 'failed'],
    [request({ export: exported().export }), 'failed'], [request({ pick: { title: 'Pick', maxBytes: 1048577 } }), 'failed']]) {
    const fixture = harness();
    let result;
    const dialog = fixture.open(file, async (value) => { result = plain(value); });
    dialog.querySelector('[data-gfw-file-download]').click();
    await flush();
    assert.deepEqual(result, { status: expected });
    assert.equal(fixture.urls.length, expected === 'completed' ? 1 : 0);
  }
});

test('cleanup from a rejected export cannot revoke a newer explicit download', async () => {
  const fixture = harness();
  let attempts = 0;
  let finish;
  const dialog = fixture.open(exported(), async () => {
    attempts++;
    if (attempts === 1) throw Object.assign(new Error('Rejected before admission'), { fileRetryable: true });
    await new Promise((resolve) => { finish = resolve; });
  });
  const download = dialog.querySelector('[data-gfw-file-download]');
  download.click();
  await flush();
  const oldCleanup = [...fixture.timers.values()][0];
  download.click();
  assert.equal(fixture.urls.length, 2);
  assert.deepEqual(fixture.revoked, [fixture.urls[0].url]);
  oldCleanup.callback();
  assert.deepEqual(fixture.revoked, [fixture.urls[0].url]);
  finish();
  await flush();
  for (const timer of fixture.timers.values()) timer.callback();
  assert.deepEqual(fixture.revoked, fixture.urls.map((entry) => entry.url));
});

test('missing file primitives offer an honest unsupported result, never simulated success', async () => {
  const fixture = harness();
  fixture.context.URL.createObjectURL = undefined;
  let result;
  const dialog = fixture.open(exported(), async (value) => { result = plain(value); });
  assert.match(dialog.querySelector('[data-gfw-file-status]').textContent, /unavailable/);
  dialog.querySelector('[data-gfw-file-download]').click();
  await flush();
  assert.deepEqual(result, { status: 'unsupported' });
  assert.equal(fixture.urls.length, 0);
});

test('wire results use issued identity, fresh requestId and latest values/revision then complete', async () => {
  const fixture = await connected();
  const dialog = await fixture.launch();
  assert.equal(fixture.commands[0].hello.capabilities['files/v1'], 'fallback');
  assert.deepEqual(fixture.commands.map((command) => command.kind), ['hello', 'open', 'action', 'complete']);
  fixture.field.value = 'Unsaved newer buffer';
  fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  select(dialog);
  await flush();
  const command = fixture.commands.find((entry) => entry.kind === 'file-result');
  assert.equal(command.fileId, 'opaque-issued');
  assert.notEqual(command.fileId, request().id);
  assert.notEqual(command.requestId, fixture.commands.find((entry) => entry.kind === 'action').requestId);
  assert.equal(command.revision, 1);
  assert.equal(command.values.title, 'Unsaved newer buffer');
  assert.deepEqual(command.fileResult, { status: 'completed', file: { name: 'note.txt', mediaType: 'text/plain', data: 'aGVsbG8=' } });
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  assert.equal(fixture.commands.at(-1).requestId, command.requestId);
  assert.equal(fixture.field.value, 'Unsaved newer buffer');
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('competing actions, refresh, search, navigation and unload cannot bypass an active file task', async () => {
  const fixture = await connected(exported());
  await fixture.launch();
  const before = fixture.commands.length;
  fixture.document.dispatchEvent({ type: 'submit', target: fixture.form, submitter: fixture.opener });
  const search = fixture.document.createElement('form');
  search.setAttribute('data-gfw-search', '');
  fixture.screen.append(search);
  fixture.document.dispatchEvent({ type: 'submit', target: search });
  for (const name of ['data-gfw-refresh', 'data-gfw-discard']) {
    const button = fixture.document.createElement(name === 'data-gfw-refresh' ? 'a' : 'button');
    button.setAttribute(name, '');
    button.setAttribute('href', '/items/42');
    fixture.screen.append(button);
    const click = { type: 'click', target: button, button: 0 };
    fixture.document.dispatchEvent(click);
    assert.equal(click.defaultPrevented, true);
  }
  const navigation = { type: 'gofastr:beforenavigate', target: fixture.root, detail: { path: '/items/43' } };
  fixture.document.dispatchEvent(navigation);
  assert.equal(navigation.defaultPrevented, true);
  const browserNavigation = { type: 'navigate', cancelable: true };
  fixture.window.navigation.dispatchEvent(browserNavigation);
  assert.equal(browserNavigation.defaultPrevented, true);
  const unload = { type: 'beforeunload' };
  fixture.window.dispatchEvent(unload);
  assert.equal(unload.defaultPrevented, true);
  await flush();
  assert.equal(fixture.commands.length, before);
  assert.equal(fixture.navigations.length, 0);
});

test('export announces initiation, not a guaranteed save, while generated download links bypass app routing', async () => {
  const fixture = await connected(exported());
  const dialog = await fixture.launch();
  dialog.querySelector('[data-gfw-file-download]').click();
  await flush();
  assert.match(fixture.live.textContent, /Download initiated/);
  assert.match(fixture.live.textContent, /cannot confirm an external disk save/);
  assert.equal(fixture.document.downloads.length, 1);
  assert.equal(fixture.document.downloads[0].defaultPrevented, undefined);
  assert.deepEqual(fixture.commands.find((command) => command.kind === 'file-result').fileResult, { status: 'completed' });
  assert.equal(fixture.navigations.length, 0);
});

test('lost write acknowledgement recovers with complete only and observes newer revisions', async () => {
  let stale = true;
  const fixture = await connected(request(), async (command) => {
    if (command.kind === 'file-result') throw new Error('Lost acknowledgement');
    if (command.kind === 'complete' && stale) { stale = false; return { error: { code: 'stale', message: 'Read current revision' } }; }
    return { response: {} };
  });
  const dialog = await fixture.launch();
  select(dialog);
  await flush();
  assert.equal(fixture.commands.filter((command) => command.kind === 'file-result').length, 1);
  assert.equal(fixture.commands.filter((command) => command.kind === 'complete').length, 4);
  assert.equal(dialog.isConnected, false);
});

test('uncertain completion locks writes and offers only same-request read-only recovery', async () => {
  let unavailable = true;
  const fixture = await connected(exported(), async (command) => {
    if (command.kind === 'complete' && unavailable) throw new Error('Completion transport failed');
    return { response: {} };
  });
  const dialog = await fixture.launch();
  dialog.querySelector('[data-gfw-file-download]').click();
  await flush();
  const write = fixture.commands.find((command) => command.kind === 'file-result');
  assert.equal(dialog.isConnected, true);
  assert.equal(dialog.querySelector('[data-gfw-file-download]').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-file-cancel]').disabled, true);
  const complete = dialog.querySelector('[data-gfw-file-complete]');
  assert.equal(complete.hidden, false);
  unavailable = false;
  complete.click();
  await flush();
  assert.equal(fixture.commands.filter((command) => command.kind === 'file-result').length, 1);
  assert.equal(fixture.commands.at(-1).requestId, write.requestId);
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  assert.equal(fixture.urls.length, 1);
  assert.equal(dialog.isConnected, false);
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('pending completion keeps read-only recovery and never resubmits the accepted file result', async () => {
  let pending = true;
  const fixture = await connected(request(), async (command) => command.kind === 'complete' && pending ?
    { error: { code: 'pending', message: 'No completed action is available' } } : { response: {} });
  const dialog = await fixture.launch();
  select(dialog);
  await flush();
  assert.equal(dialog.querySelector('[data-gfw-file-input]').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-file-complete]').hidden, false);
  pending = false;
  dialog.querySelector('[data-gfw-file-complete]').click();
  await flush();
  assert.equal(fixture.commands.filter((command) => command.kind === 'file-result').length, 1);
  assert.equal(dialog.isConnected, false);
});

test('read-only errors and empty completion replies never masquerade as rejected writes', async () => {
  for (const rejection of [{ error: { code: 'invalid_request', message: 'Current values cannot be observed' } }, {}]) {
    const fixture = await connected(request(), async (command) => {
      if (command.kind === 'file-result') throw new Error('Acknowledgement lost after admission');
      return rejection;
    });
    const dialog = await fixture.launch();
    select(dialog);
    await flush();
    assert.equal(dialog.isConnected, true);
    assert.equal(dialog.querySelector('[data-gfw-file-input]').disabled, true);
    assert.equal(dialog.querySelector('[data-gfw-file-cancel]').disabled, true);
    const complete = dialog.querySelector('[data-gfw-file-complete]');
    assert.equal(complete.hidden, false);
    complete.click();
    await flush();
    assert.equal(fixture.commands.filter((command) => command.kind === 'file-result').length, 1);
    assert.equal(fixture.commands.at(-1).kind, 'complete');
  }
});

test('only known pre-admission rejection allows correction; accepted validation consumes the file task', async () => {
  for (const admitted of [false, true]) {
    const fixture = await connected(request(), async () => ({ ...(admitted ? { response: {} } : {}), error: { code: 'validation', message: 'Correct the file' } }));
    const dialog = await fixture.launch();
    select(dialog);
    await flush();
    assert.equal(dialog.isConnected, !admitted);
    assert.equal(fixture.commands.at(-1).kind, admitted ? 'complete' : 'file-result');
    if (!admitted) {
      assert.equal(dialog.querySelector('input').disabled, false);
      assert.equal(dialog.querySelector('[data-gfw-file-status]').textContent, 'Correct the file');
    }
  }
});

test('consumed files chain to a presentation and back without competing modals or losing focus', async () => {
  const fixture = await connected(request(), async (command) => {
    if (command.kind === 'respond') return { response: {} };
    if (command.kind === 'complete' && fixture.commands.some((entry) => entry.kind === 'respond' && entry.requestId === command.requestId)) {
      return { fileId: 'next-file-id', response: { files: exported() } };
    }
    if (command.kind === 'complete') return { presentationId: 'next-presentation', response: { presentation: presentation() } };
    return { response: {} };
  });
  const dialog = await fixture.launch();
  select(dialog);
  await flush();
  assert.equal(dialog.isConnected, false);
  const next = fixture.document.querySelector('[data-gfw-presentation]');
  assert.ok(next);
  assert.equal(fixture.document.querySelectorAll('[data-gfw-file]').length, 0);
  next.querySelector('[data-gfw-choice]').click();
  await flush();
  assert.equal(next.isConnected, false);
  const final = fixture.document.querySelector('[data-gfw-file]');
  assert.equal(final.dataset.gfwFileId, 'next-file-id');
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('photo inputs retain file selectors and explicitly distinguish library from a possible camera chooser', () => {
  for (const source of ['library', 'camera']) {
    const fixture = harness({});
    const dialog = fixture.open(photoRequest({ source }));
    const input = dialog.querySelector('[data-gfw-file-input]');
    assert.equal(dialog.dataset.gfwFile, 'photo');
    assert.equal(dialog.dataset.gfwFileId, 'opaque-issued');
    assert.equal(dialog.dataset.gfwPhotoSource, source);
    assert.equal(input.dataset.gfwPhotoSource, source);
    assert.equal(input.getAttribute('accept'), 'image/*');
    assert.equal(input.getAttribute('capture'), source === 'camera' ? 'environment' : null);
    assert.equal(input.hasAttribute('multiple'), false);
    assert.equal(input.clicks, 0);
    assert.equal(fixture.adapter.photoCapability(), 'fallback');
    if (source === 'camera') {
      assert.match(dialog.querySelector('p').textContent, /may offer a camera/);
      assert.match(dialog.querySelector('label').textContent, /may offer camera/);
    }
    assert.throws(() => fixture.open(request()), /open modal/);
  }
});

test('photo request validates exactly one operation and every frozen policy boundary before reading', async () => {
  for (const definition of [
    { ...photoRequest(), pick: request().pick }, { ...photoRequest(), export: exported().export },
    { ...photoRequest(), pick: false }, { id: 'bad-photo', photo: false }, { id: 'bad-photo', photo: [] },
    ...[{ maxBytes: 0 }, { maxBytes: 1048577 }, { maxBytes: 1.5 }, { maxDimension: 0 }, { maxDimension: 2049 },
      { maxDimension: 2.5 }, { source: 'video' }, { title: 1 }, { title: '' }].map(photoRequest),
  ]) {
    const fixture = harness({});
    let result;
    const dialog = fixture.open(definition, async (value) => { result = plain(value); });
    assert.equal(dialog.querySelector('[data-gfw-file-input]'), null);
    dialog.querySelector('[data-gfw-file-download]').click();
    await flush();
    assert.deepEqual(result, { status: 'failed' });
    assert.equal(fixture.photos.decoded.length, 0);
  }
  const fixture = harness({});
  assert.ok(fixture.open(photoRequest({ maxBytes: 1, maxDimension: 1 })).querySelector('input'));
});

test('photo capabilities are honestly unavailable without image primitives and preserve legacy files', async () => {
  for (const options of [undefined, { noDecoder: true }, { noEncoder: true }, { noContext: true }]) {
    const fixture = await connected(photoRequest(), undefined, options);
    const dialog = await fixture.launch();
    assert.equal(fixture.commands[0].hello.capabilities['files/v1'], 'fallback');
    assert.equal(fixture.commands[0].hello.capabilities['photos/v1'], 'unavailable');
    assert.equal(fixture.commands[0].hello.capabilities['camera/v1'], 'unavailable');
    assert.equal(dialog.querySelector('input'), null);
    dialog.querySelector('[data-gfw-file-download]').click();
    await flush();
    assert.deepEqual(fixture.commands.find((command) => command.kind === 'file-result').fileResult, { status: 'unsupported' });
  }
});

test('normalization verifies MIME and container rather than trusting extensions, and bounds before reading', async () => {
  const fixture = harness({});
  const normalize = fixture.window.__gofastr_photos.normalize;
  const policy = photoRequest().photo;
  let reads = 0;
  for (const size of [0, -1, 20 * 1024 * 1024 + 1, 1.5]) {
    await assert.rejects(normalize({ name: 'photo.jpg', type: 'image/jpeg', size, slice() { reads++; } }, policy), { name: 'SecurityError' });
  }
  assert.equal(reads, 0);
  for (const [type, contents] of [
    ['image/svg+xml', '<svg/>'], ['text/plain', png()], ['image/png', '<svg/>'], ['image/jpeg', png()],
    ['', jpeg()], ['image/gif', 'GIF89a'], ['image/heic', 'not supported'],
  ]) {
    await assert.rejects(normalize(new Blob([contents], { type }), policy), { name: 'NotSupportedError' });
  }
  for (const contents of [png(0, 30), png(16385, 1), png(10000, 10000)]) {
    await assert.rejects(normalize(new Blob([contents], { type: 'image/png' }), policy), { name: 'SecurityError' });
  }
  assert.equal(fixture.photos.decoded.length, 0);
  const source = new Blob([jpeg(40, 30, 20 * 1024 * 1024)], { type: 'image/jpeg' });
  const slices = [];
  const result = await normalize({ size: source.size, type: source.type, slice(...args) { slices.push(args); return source.slice(...args); } }, policy);
  assert.equal(result.mediaType, 'image/jpeg');
  assert.deepEqual(slices, [[0, 256 * 1024], [0, source.size, 'image/jpeg']]);
  assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
});

test('native EXIF orientation precedes bounded bitmap resizing and canvas replaces all source metadata', async () => {
  for (const orientation of [1, 2, 3, 4, 5, 6, 7, 8]) {
    const fixture = harness({});
    const source = new Blob([jpeg(4000, 3000, 128, orientation)], { type: 'image/jpeg' });
    const result = await fixture.window.__gofastr_photos.normalize(source, photoRequest().photo);
    assert.deepEqual(fixture.photos.decoded[0].settings, { imageOrientation: 'from-image', resizeWidth: orientation >= 5 ? 1536 : 2048,
      resizeHeight: orientation >= 5 ? 2048 : 1536, resizeQuality: 'high' });
    assert.equal(result.name, 'photo.jpg');
    assert.equal(result.mediaType, 'image/jpeg');
    assert.deepEqual(Buffer.from(result.data, 'base64'), jpeg(orientation >= 5 ? 1536 : 2048, orientation >= 5 ? 2048 : 1536));
    assert.equal(Buffer.from(result.data, 'base64').includes(Buffer.from('Exif')), false);
    assert.equal(fixture.photos.draws.length, 1);
    assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
    assert.equal(fixture.photos.canvases.at(-1).width, 0);
    assert.equal(fixture.photos.canvases.at(-1).height, 0);
  }
});

test('PNG eXIf orientation is inspected even after image data without reading that data', async () => {
  const fixture = harness({});
  const start = png(4000, 3000).subarray(0, 33);
  const exif = jpeg(40, 30, 128, 6).subarray(12, 38);
  const chunk = Buffer.alloc(exif.length + 12);
  chunk.writeUInt32BE(exif.length, 0);
  chunk.write('eXIf', 4);
  exif.copy(chunk, 8);
  const imageData = Buffer.alloc(4096 + 12);
  imageData.writeUInt32BE(4096, 0);
  imageData.write('IDAT', 4);
  const contents = Buffer.concat([start, imageData, chunk, png().subarray(33)]);
  const source = new Blob([contents], { type: 'image/png' });
  const reads = [];
  await fixture.window.__gofastr_photos.normalize({ size: source.size, type: source.type, slice(...args) {
    if (args.length === 2) reads.push(args);
    return source.slice(...args);
  } }, photoRequest().photo);
  assert.equal(fixture.photos.decoded[0].settings.resizeWidth, 1536);
  assert.equal(fixture.photos.decoded[0].settings.resizeHeight, 2048);
  assert.ok(reads.every(([start, end]) => end <= 41 || start >= 33 + imageData.length));
});

test('photo encoding accepts exactly 1 MiB and terminates an impossible budget within 36 encodes', async () => {
  const fixture = harness({ encode: (canvas) => new Blob([jpeg(canvas.width, canvas.height, 1048576)], { type: 'image/jpeg' }) });
  const result = await fixture.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest().photo);
  assert.equal(Buffer.from(result.data, 'base64').length, 1048576);
  const impossible = harness({ encode: () => ({ type: 'image/jpeg', size: 2, arrayBuffer() { throw new Error('Oversized output is not read'); } }) });
  await assert.rejects(impossible.window.__gofastr_photos.normalize(new Blob([png(2048, 2048)], { type: 'image/png' }), photoRequest({ maxBytes: 1 }).photo), { name: 'SecurityError' });
  assert.equal(impossible.photos.encodes.length, 36);
  assert.equal(impossible.photos.encodes.at(-1).width, 1);
  assert.equal(impossible.photos.decoded[0].bitmap.closed, 1);
});

test('JPEG normalization reduces quality then dimensions and never reads oversized encoded candidates', async () => {
  let candidateReads = 0;
  const fixture = harness({ encode: (canvas, quality) => canvas.width > 20 || quality > 0.65 ?
    { type: 'image/jpeg', size: 101, arrayBuffer() { candidateReads++; throw new Error('Do not read oversized output'); } } :
    new Blob([jpeg(canvas.width, canvas.height, 100)], { type: 'image/jpeg' }) });
  const result = await fixture.window.__gofastr_photos.normalize(new Blob([png(80, 60)], { type: 'image/png' }), photoRequest({ maxBytes: 100, maxDimension: 40 }).photo);
  assert.equal(candidateReads, 0);
  assert.equal(Buffer.from(result.data, 'base64').length, 100);
  assert.deepEqual(fixture.photos.encodes.map((entry) => [entry.width, entry.height, entry.quality]),
    [[40, 30, 0.9], [40, 30, 0.65], [40, 30, 0.4], [20, 15, 0.9], [20, 15, 0.65]]);
  assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
});

test('impossible byte limits, invalid encoders, decoded allocations, and corrupt headers reject with cleanup', async () => {
  for (const [options, policy, error] of [
    [{}, { maxBytes: 1, maxDimension: 1 }, 'SecurityError'],
    [{ encode: () => null }, {}, 'Error'],
    [{ encode: () => new Blob(['png'], { type: 'image/png' }) }, {}, 'NotSupportedError'],
    [{ encode: () => new Blob([], { type: 'image/jpeg' }) }, {}, 'Error'],
    [{ encode: () => new Blob([jpeg(1, 1)], { type: 'image/jpeg' }) }, {}, 'SecurityError'],
    [{ width: 5000, height: 5000 }, {}, 'SecurityError'],
  ]) {
    const fixture = harness(options);
    await assert.rejects(fixture.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest(policy).photo), { name: error });
    assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
  }
  const fixture = harness({});
  for (const contents of [jpeg().subarray(0, 10), Buffer.from([255, 216, 255, 225, 255, 255]), Buffer.alloc(33)]) {
    await assert.rejects(fixture.window.__gofastr_photos.normalize(new Blob([contents], { type: 'image/jpeg' }), photoRequest().photo), { name: 'NotSupportedError' });
  }
});

test('the HTML image fallback cleans URLs, and refuses large source allocations before decoding', async () => {
  const fixture = harness({ htmlImage: true });
  const result = await fixture.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest().photo);
  assert.equal(result.name, 'photo.jpg');
  assert.equal(fixture.urls.length, 1);
  assert.deepEqual(fixture.revoked, [fixture.urls[0].url]);
  await assert.rejects(fixture.window.__gofastr_photos.normalize(new Blob([png(5000, 5000)], { type: 'image/png' }), photoRequest().photo), { name: 'SecurityError' });
  assert.equal(fixture.urls.length, 1);
});

test('cancelling normalization ignores late bitmap and canvas completions and immediately releases resources', async () => {
  let resume;
  const fixture = harness({ beforeDecode: () => new Promise((resolve) => { resume = resolve; }) });
  const operation = fixture.window.__gofastr_photos.job();
  const result = fixture.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest().photo, operation);
  await flush();
  operation.cancel();
  const rejected = assert.rejects(result, { name: 'AbortError' });
  resume();
  await rejected;
  assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
  assert.equal(fixture.photos.encodes.length, 0);
  let encode;
  const pending = harness({ encode: (_, __, callback) => { encode = callback; } });
  const encoding = pending.window.__gofastr_photos.job();
  const output = pending.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest().photo, encoding);
  await flush();
  encoding.cancel();
  assert.equal(pending.photos.decoded[0].bitmap.closed, 1);
  assert.equal(pending.photos.canvases.at(-1).width, 0);
  encode(new Blob([jpeg()], { type: 'image/jpeg' }));
  await assert.rejects(output, { name: 'AbortError' });
  const fallback = harness({ htmlImage: true, pendingImage: true });
  const loading = fallback.window.__gofastr_photos.job();
  const imageResult = fallback.window.__gofastr_photos.normalize(new Blob([png()], { type: 'image/png' }), photoRequest().photo, loading);
  await flush();
  loading.cancel();
  await assert.rejects(imageResult, { name: 'AbortError' });
  assert.deepEqual(fallback.revoked, [fallback.urls[0].url]);
});

test('photo outcomes never carry content on unsupported, denied, cancelled or failed decoding', async () => {
  for (const [name, status] of [['NotSupportedError', 'unsupported'], ['SecurityError', 'denied'], ['NotAllowedError', 'denied'], ['AbortError', 'cancelled'], ['EncodingError', 'failed']]) {
    const fixture = harness({ decodeError: name });
    let result;
    const dialog = fixture.open(photoRequest(), async (value) => { result = plain(value); });
    select(dialog, png(), { name: 'image.png', type: 'image/png' });
    await flush();
    assert.deepEqual(result, { status });
    assert.equal(dialog.isConnected, false);
  }
});

test('duplicate photo change, cancellation, and disposal cannot leak bytes or submit twice', async () => {
  for (const disposal of [false, true]) {
    let resume;
    let results = 0;
    let result;
    const fixture = harness({ beforeDecode: () => new Promise((resolve) => { resume = resolve; }) });
    const dialog = fixture.open(photoRequest(), async (value) => { results++; result = plain(value); });
    select(dialog, png(), { type: 'image/png' });
    await flush();
    const input = dialog.querySelector('input');
    input.dispatchEvent({ type: 'change' });
    assert.equal(input.disabled, true);
    assert.equal(dialog.querySelector('[data-gfw-file-cancel]').disabled, false);
    if (disposal) fixture.adapter.close(fixture.root, false);
    else dialog.querySelector('[data-gfw-file-cancel]').click();
    resume();
    await flush();
    assert.equal(results, disposal ? 0 : 1);
    if (!disposal) assert.deepEqual(result, { status: 'cancelled' });
    assert.equal(fixture.photos.decoded.length, 1);
    assert.equal(fixture.photos.decoded[0].bitmap.closed, 1);
    assert.equal(fixture.photos.encodes.length, 0);
  }
});

test('photo wire uses the shared file-result with latest editor state and exports exactly the normalized JPEG', async () => {
  let resume;
  let normalized;
  let photoRequestId;
  const fixture = await connected(photoRequest(), async (command) => {
    if (command.kind === 'file-result' && command.fileResult.file) { normalized = command.fileResult.file; photoRequestId = command.requestId; }
    if (command.kind === 'complete' && command.requestId === photoRequestId) return { fileId: 'export-issued', response: { files: { id: 'export-photo', export: normalized } } };
    return { response: {} };
  }, { beforeDecode: () => new Promise((resolve) => { resume = resolve; }) });
  const dialog = await fixture.launch();
  assert.equal(fixture.commands[0].hello.capabilities['photos/v1'], 'fallback');
  assert.equal(fixture.commands[0].hello.capabilities['camera/v1'], 'fallback');
  select(dialog, png(80, 60), { type: 'image/png' });
  await flush();
  fixture.field.value = 'New photo draft';
  fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  resume();
  await flush();
  const write = fixture.commands.find((command) => command.kind === 'file-result');
  assert.equal(write.fileId, 'opaque-issued');
  assert.notEqual(write.requestId, fixture.commands.find((command) => command.kind === 'action').requestId);
  assert.equal(write.revision, 1);
  assert.equal(write.values.title, 'New photo draft');
  assert.deepEqual(write.fileResult, { status: 'completed', file: { name: 'photo.jpg', mediaType: 'image/jpeg', data: jpeg(80, 60).toString('base64') } });
  assert.equal(fixture.commands.at(-1).requestId, write.requestId);
  const exportDialog = fixture.document.querySelector('[data-gfw-file]');
  assert.equal(exportDialog.dataset.gfwFile, 'export');
  assert.equal(exportDialog.dataset.gfwFileId, 'export-issued');
  exportDialog.querySelector('[data-gfw-file-download]').click();
  await flush();
  assert.deepEqual(Buffer.from(await fixture.urls[0].blob.arrayBuffer()), Buffer.from(normalized.data, 'base64'));
  assert.equal(fixture.field.value, 'New photo draft');
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('photo acknowledgement and completion losses retain one-write complete-only recovery', async () => {
  let unavailable = true;
  const fixture = await connected(photoRequest({ source: 'camera' }), async (command) => {
    if (command.kind === 'file-result') throw new Error('Acknowledgement lost');
    if (unavailable) throw new Error('Completion lost');
    return { response: {} };
  }, {});
  const dialog = await fixture.launch();
  select(dialog, png(), { type: 'image/png' });
  await flush();
  const write = fixture.commands.find((command) => command.kind === 'file-result');
  assert.equal(dialog.querySelector('input').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-file-cancel]').disabled, true);
  unavailable = false;
  dialog.querySelector('[data-gfw-file-complete]').click();
  await flush();
  assert.equal(fixture.commands.filter((command) => command.kind === 'file-result').length, 1);
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  assert.equal(fixture.commands.at(-1).requestId, write.requestId);
  assert.equal(fixture.photos.encodes.length, 1);
  assert.equal(fixture.document.activeElement, fixture.opener);
  assert.equal(dialog.isConnected, false);
});
