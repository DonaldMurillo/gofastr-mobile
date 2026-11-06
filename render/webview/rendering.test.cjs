const assert = require('node:assert/strict');
const { test } = require('node:test');
const { connected, harness, flush, plain } = require('./test-dom.cjs');

const name = 'gofastr.media/v1';
const moduleName = 'gfn-extension-' + 'a'.repeat(32);
const mediaScript = '../../extensions/media/media.js';
const image = (id = 'photo') => ({ id, alt: 'Private <photo>', data: '/9j/AA==', width: 2, height: 1, destination: 'https://untrusted.invalid/image' });
const file = (id = 'file') => ({ id, name: '<b>brief.txt', mediaType: 'text/plain', size: 12, destination: 'javascript:untrusted()' });
const audio = () => {
  const content = Buffer.alloc(44); content.write('RIFF'); content.write('WAVE', 8);
  const file = { id: 'voice', name: 'voice.wav', mediaType: 'audio/wav', size: content.length, destination: '/untrusted' };
  return { ...file, audio: { file: { name: file.name, mediaType: file.mediaType, data: content.toString('base64') }, durationMillis: 1000 } };
};
const snapshot = (payload = { images: [image()], attachments: [file()] }, destinations = { 'image.0': '/items', 'file.0': '/items' }, id = 'media') => ({ id, name, payload: JSON.stringify(payload), destinations });
const loadMedia = fixture => { fixture.load(mediaScript); return fixture.window.__gofastr_component_extension_definitions[0]; };
const mountMedia = (initial = snapshot()) => {
  const fixture = harness();
  loadMedia(fixture);
  const opened = [];
  const host = fixture.window.__gofastr_rendering.create(fixture.root);
  const update = components => host.reconcile(components, { names: [name], open: (component, key) => opened.push({ component, key }) });
  update([initial]);
  return { ...fixture, host, update, opened };
};

test('component facts come only from selected compiled descriptors and validate each function and capability', () => {
  const fixture = harness();
  const descriptor = loadMedia(fixture);
  const facts = fixture.window.__gofastr_rendering.capabilities;
  assert.deepEqual(plain(facts()), {});
  assert.deepEqual(plain(facts([name])), { [name]: 'available' });
  for (const bad of [{ ...descriptor, mount: null }, { ...descriptor, capabilities: null },
    { ...descriptor, capabilities: () => ({ wrong: 'available' }) }, { ...descriptor, capabilities: () => ({ [name]: 'yes' }) }]) {
    fixture.window.__gofastr_component_extension_definitions = [bad];
    assert.throws(() => facts([name]), /adapter|capabilities/);
  }
  fixture.window.__gofastr_component_extension_definitions = [descriptor, descriptor];
  assert.throws(() => facts([name]), /missing or invalid/);
  fixture.window.__gofastr_component_extension_definitions = [];
  assert.throws(() => facts([name]), /Required rendering adapter/);
});

test('generic host mounts by ID, forwards whole updates, preserves order and disposes once on removal/name change/retirement', () => {
  const fixture = harness();
  const calls = [], callbacks = [];
  const descriptor = { name, capabilities: () => ({ [name]: 'available' }), mount(container, component, context) {
    calls.push(['mount', component]); callbacks.push(context.open);
    container.append(fixture.document.createElement('span'));
    return { update(next) { calls.push(['update', next]); }, dispose() { calls.push(['dispose', component.id]); } };
  } };
  fixture.window.__gofastr_component_extension_definitions = [descriptor];
  const host = fixture.window.__gofastr_rendering.create(fixture.root);
  const opened = [], options = { names: [name], open: (component, key) => opened.push([component.destinations[key], key]) };
  const first = snapshot({}, { open: '/items' }), second = snapshot({}, {}, 'second');
  host.reconcile([first, second], options);
  const container = fixture.root.querySelector('[data-gfw-component="media"]');
  const next = { ...first, payload: '{"changed":true}', destinations: { open: '/items?new=1' } };
  host.reconcile([second, next], options);
  assert.equal(fixture.root.querySelector('[data-gfw-component="media"]'), container);
  assert.deepEqual(calls.filter(item => item[0] === 'mount').map(item => item[1].id), ['media', 'second']);
  assert.equal(calls.find(item => item[0] === 'update' && item[1].id === 'media')[1], next);
  callbacks[0]('open'); callbacks[0]('/items'); callbacks[0]('__proto__');
  assert.deepEqual(opened, [['/items?new=1', 'open']]);
  host.reconcile([next], options);
  const replacement = { ...next, name: 'other.library/v1' };
  fixture.window.__gofastr_component_extension_definitions.push({ ...descriptor, name: replacement.name, capabilities: () => ({ [replacement.name]: 'available' }) });
  host.reconcile([replacement], { ...options, names: [name, replacement.name] });
  callbacks[0]('open');
  host.dispose(); host.dispose(); callbacks.at(-1)('open');
  assert.deepEqual(calls.filter(item => item[0] === 'dispose').map(item => item[1]), ['second', 'media', 'media']);
  assert.equal(opened.length, 1);
});

test('missing, unavailable, broken and oversized components fail visibly rather than rendering old media fields', () => {
  const fixture = harness();
  const host = fixture.window.__gofastr_rendering.create(fixture.root);
  assert.throws(() => host.reconcile([snapshot()]), /Required rendering adapter/);
  const failure = fixture.root.querySelector('[data-gfw-component-error]');
  assert.equal(failure.getAttribute('role'), 'alert');
  assert.match(failure.textContent, /Component unavailable/);
  let disposed = 0, staleOpen;
  fixture.window.__gofastr_component_extension_definitions = [{ name, capabilities: () => ({ [name]: 'available' }), mount(_container, _component, context) {
    staleOpen = context.open;
    return { dispose() { disposed++; } };
  } }];
  assert.throws(() => host.reconcile([snapshot()], { names: [name] }), /Invalid mounted/);
  assert.equal(disposed, 1);
  staleOpen('file.0');
  assert.throws(() => host.reconcile([{ ...snapshot(), payload: 'é'.repeat(1024 * 1024 + 1) }], { names: [name] }), /oversized/);
  assert.throws(() => host.reconcile([snapshot(), snapshot()], { names: [name] }), /Invalid/);
  fixture.window.__gofastr_component_extension_definitions[0].capabilities = () => ({ [name]: 'unavailable' });
  assert.throws(() => host.reconcile([snapshot()], { names: [name] }), /unavailable/);
  let mounts = 0;
  fixture.window.__gofastr_component_extension_definitions[0] = { name, capabilities: () => ({ [name]: 'fallback' }), mount() { mounts++; } };
  assert.throws(() => host.reconcile([snapshot()], { names: [name] }), /unavailable/);
  assert.equal(mounts, 0);
  assert.equal(fixture.root.querySelector('[data-gfw-component-error]').getAttribute('role'), 'alert');
});

test('real media descriptor renders safe text, data-only images and opaque keyboard/click navigation keys', () => {
  const initial = snapshot({ images: [image()], attachments: [file()], preview: { file: file('detail'), caption: '<img onerror=evil()>', previewTitle: '<b>Preview', text: '<script>evil()</script>\nPlain café' } },
    { 'image.0': '/items', 'file.0': '/items', preview: '/items' });
  const fixture = mountMedia(initial);
  const photo = fixture.root.querySelector('[data-gfw-image]');
  const attachment = fixture.root.querySelector('[data-gfw-attachment]');
  assert.equal(photo.tagName, 'A');
  assert.equal(photo.getAttribute('href'), null);
  assert.equal(attachment.getAttribute('href'), null);
  assert.equal(photo.querySelector('img').src, 'data:image/jpeg;base64,' + image().data);
  const preview = fixture.root.querySelector('[data-gfw-file-preview]');
  assert.equal(preview.contains(photo), true);
  assert.equal(preview.contains(attachment), true);
  assert.equal(preview.querySelector('[data-gfw-image]').querySelector('img').src, 'data:image/jpeg;base64,' + image().data);
  assert.equal(attachment.querySelector('strong').textContent, '<b>brief.txt');
  assert.equal(fixture.root.querySelector('pre').textContent, '<script>evil()</script>\nPlain café');
  assert.equal(fixture.root.querySelectorAll('script').length, 0);
  assert.equal(fixture.root.querySelectorAll('iframe').length, 0);
  photo.click(); attachment.click(); fixture.root.querySelector('[data-gfw-preview]').dispatchEvent({ type: 'keydown', key: 'Enter' });
  assert.deepEqual(fixture.opened.map(item => item.key), ['image.0', 'file.0', 'preview']);
  fixture.update([{ ...initial, destinations: {} }]);
  photo.click(); attachment.click();
  assert.equal(fixture.opened.length, 3);
  assert.equal(photo.getAttribute('tabindex'), null);
  fixture.host.dispose();
});

test('real media keyed updates preserve editor/IME/focus, unchanged images/audio and current index keys', () => {
  const voice = audio();
  const fixture = mountMedia(snapshot({ images: [image()], attachments: [voice] }));
  const editor = fixture.document.createElement('textarea'); editor.value = 'Unsaved composing café'; fixture.root.append(editor); editor.focus();
  let compositionEnds = 0; editor.addEventListener('compositionend', () => compositionEnds++);
  const photo = fixture.root.querySelector('[data-gfw-image]').querySelector('img');
  const player = fixture.root.querySelector('[data-gfw-audio-playback]');
  player.currentTime = 12;
  const originalURL = player.src;
  fixture.update([snapshot({ images: [image('added'), image()], attachments: [file('new'), voice] }, { 'image.1': '/items', 'file.1': '/items' })]);
  assert.equal(fixture.root.querySelector('[data-gfw-image="photo"]').querySelector('img'), photo);
  assert.equal(fixture.root.querySelector('[data-gfw-audio-playback]'), player);
  assert.equal(player.src, originalURL);
  assert.equal(player.currentTime, 12);
  assert.equal(player.pauses, 0);
  assert.equal(fixture.document.activeElement, editor);
  assert.equal(editor.value, 'Unsaved composing café');
  assert.equal(compositionEnds, 0);
  fixture.root.querySelector('[data-gfw-image="photo"]').click();
  fixture.root.querySelector('[data-gfw-attachment="voice"]').click();
  assert.deepEqual(fixture.opened.map(item => item.key), ['image.1', 'file.1']);
  fixture.update([]);
  assert.equal(player.pauses, 1);
  assert.equal(fixture.objects.size, 0);
  assert.deepEqual(fixture.revoked, [originalURL]);
  fixture.host.dispose();
});

test('private audio revokes bytes on background/removal and never records, downloads or automatically plays', () => {
  const fixture = mountMedia(snapshot({ attachments: [audio()] }));
  const player = fixture.root.querySelector('audio');
  assert.equal(player.hasAttribute('autoplay'), false);
  assert.equal(player.getAttribute('preload'), 'none');
  assert.equal(fixture.objects.size, 1);
  fixture.document.hidden = true; fixture.document.dispatchEvent({ type: 'visibilitychange' });
  assert.equal(fixture.objects.size, 0);
  assert.equal(player.src, undefined);
  fixture.document.hidden = false; fixture.document.dispatchEvent({ type: 'visibilitychange' });
  assert.equal(fixture.objects.size, 1);
  assert.equal(player.pauses, 1);
  fixture.window.dispatchEvent({ type: 'pagehide' });
  assert.equal(fixture.objects.size, 0);
  fixture.window.dispatchEvent({ type: 'pageshow' });
  assert.equal(fixture.objects.size, 1);
  fixture.host.dispose(); fixture.host.dispose();
  assert.equal(fixture.objects.size, 0);
  assert.equal(fixture.downloads?.length || fixture.document.downloads.length, 0);
  assert.equal(fixture.window.listeners.get('pageshow').size, 1);
});

test('real media rejects injected/oversized image, audio and text payloads before allocating resources', () => {
  for (const payload of [{ images: [{ ...image(), data: 'https://untrusted.invalid/photo' }] }, { images: [{ ...image(), data: 'PHN2Zz4=' }] },
    { images: [{ ...image(), width: 1025 }] }, { attachments: [{ ...audio(), audio: { ...audio().audio, file: { ...audio().audio.file, data: 'ZmlsZQ==' } } }] },
    { preview: { file: file(), text: 'é'.repeat(8193) } }, { preview: { file: file(), text: '<p>safe</p>\u0000' } },
    { attachments: [{ ...file(), name: '../private.txt' }] }, { images: [image(), image()] }, { attachments: Array.from({ length: 65 }, (_, index) => file('file-' + index)) }]) {
    const fixture = harness(); loadMedia(fixture);
    const host = fixture.window.__gofastr_rendering.create(fixture.root);
    assert.throws(() => host.reconcile([snapshot(payload)], { names: [name] }));
    assert.equal(fixture.objects.size, 0);
    assert.equal(fixture.root.querySelectorAll('img').length, 0);
    assert.ok(fixture.root.querySelector('[data-gfw-component-error]'));
    host.dispose();
  }
});

test('selected modules finish loading and declare media before the first hello; omitted modules declare nothing', async () => {
  const without = await connected(async () => ({}), { beforeRuntime: loadMedia });
  assert.equal(Object.hasOwn(without.commands[0].hello.capabilities, name), false);
  without.depart();
  let loaded = false;
  const fixture = await connected(async () => ({}), { config: { extensions: [moduleName], componentExtensions: [name] }, openScreen: { components: [snapshot()] },
    beforeRuntime(value) { value.window.__gofastr.loadModule = async module => { assert.equal(module, moduleName); await flush(); loadMedia(value); loaded = true; }; } });
  await flush();
  assert.equal(loaded, true);
  assert.equal(fixture.commands[0].kind, 'hello');
  assert.equal(fixture.commands[0].hello.capabilities[name], 'available');
  assert.ok(fixture.root.querySelector('[data-gfw-image]'));
  fixture.depart();
});

test('missing required adapter fails visibly before hello and an unselected component never silently mounts', async () => {
  const missing = await connected(async () => ({}), { config: { componentExtensions: [name] } });
  assert.equal(missing.commands.length, 0);
  assert.match(missing.status.textContent, /Required rendering adapter/);
  assert.equal(missing.form.dataset.gfwError, 'true');
  missing.depart();
  const unselected = await connected(async () => ({}), { openScreen: { components: [snapshot()] }, beforeRuntime: loadMedia });
  assert.equal(unselected.commands[0].hello.capabilities[name], undefined);
  assert.match(unselected.status.textContent, /Required rendering adapter/);
  assert.ok(unselected.root.querySelector('[data-gfw-component-error]'));
  unselected.depart();
});

test('media navigation resolves only the Go event map and uses current editor leave/checkpoint/IME guards', async () => {
  let canLeave = true;
  const fixture = await connected(async command => command.kind === 'leave' ? { editor: { canLeave } } : {}, {
    config: { componentExtensions: [name] }, openScreen: { form: { fields: [] }, components: [snapshot()] }, beforeRuntime: loadMedia });
  const link = fixture.root.querySelector('[data-gfw-attachment]');
  fixture.field.value = 'Unsaved current value'; fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  fixture.document.dispatchEvent({ type: 'compositionstart', target: fixture.field });
  link.click(); await flush();
  assert.equal(fixture.navigations.length, 0);
  fixture.document.dispatchEvent({ type: 'compositionend', target: fixture.field }); await flush();
  canLeave = false; link.click(); await flush();
  assert.equal(fixture.navigations.length, 0);
  assert.equal(fixture.commands.findLast(command => command.kind === 'leave').values.title, 'Unsaved current value');
  canLeave = true; link.click(); await flush();
  assert.equal(fixture.navigations[0][0], '/items');
  assert.equal(fixture.commands.some(command => JSON.stringify(command).includes('javascript:untrusted')), false);
  fixture.depart();
});

test('inline list refresh propagates components and preserves their keyed instances until retirement', async () => {
  const list = { header: 'Items', rows: [], refreshable: true };
  let watch;
  const fixture = await connected(async (command, _, signal) => {
    if (command.kind === 'watch') return new Promise(resolve => { watch = resolve; signal.addEventListener('abort', () => resolve({})); });
    if (command.kind === 'open') return { instance: 'instance-2', screen: { route: '/items', list, live: { version: 'v2' }, components: [snapshot({ attachments: [audio()] })] } };
    return {};
  }, { list, config: { componentExtensions: [name] }, openScreen: { list, live: { version: 'v1' }, components: [snapshot({ attachments: [audio()] })] }, beforeRuntime: loadMedia });
  const player = fixture.root.querySelector('audio'), source = player.src;
  fixture.tick(50); await flush(); watch({ changed: true, dataVersion: 'v2' }); await flush();
  assert.equal(fixture.root.dataset.gfwInstance, 'instance-2');
  assert.equal(fixture.root.querySelector('audio'), player);
  assert.equal(player.src, source);
  assert.equal(player.pauses, 0);
  assert.equal(JSON.parse(fixture.root.dataset.gfwConfig).components[0].name, name);
  fixture.depart();
  assert.equal(fixture.objects.size, 0);
});

test('same-route retained-root refresh updates without remounting media and disposes when the route retires', async () => {
  const initial = snapshot({ attachments: [audio()] });
  const fixture = await connected(async command => command.kind === 'open' ? { instance: 'instance-2', screen: { components: [initial] } } : {}, {
    config: { componentExtensions: [name] }, openScreen: { components: [initial] }, beforeRuntime: loadMedia });
  const player = fixture.root.querySelector('audio'), source = player.src;
  fixture.window.dispatchEvent({ type: 'gofastr:navigate' }); await flush();
  assert.equal(fixture.root.querySelector('audio'), player);
  assert.equal(player.src, source);
  assert.equal(player.pauses, 0);
  fixture.depart();
  assert.equal(fixture.objects.size, 0);
});

test('a retained-root component refresh preserves active IME composition until its actual compositionend', async () => {
  const initial = snapshot();
  const fixture = await connected(async command => command.kind === 'open' ? { instance: 'instance-2', screen: { form: {}, components: [initial] } } : { editor: { canLeave: true } }, {
    config: { componentExtensions: [name] }, openScreen: { form: {}, components: [initial] }, beforeRuntime: loadMedia });
  fixture.field.value = 'Still composing café'; fixture.field.focus();
  fixture.document.dispatchEvent({ type: 'compositionstart', target: fixture.field });
  fixture.window.dispatchEvent({ type: 'gofastr:navigate' }); await flush();
  fixture.root.querySelector('[data-gfw-attachment]').click(); await flush();
  assert.equal(fixture.navigations.length, 0);
  assert.equal(fixture.document.activeElement === fixture.field, true);
  assert.equal(fixture.field.value, 'Still composing café');
  fixture.document.dispatchEvent({ type: 'compositionend', target: fixture.field }); await flush();
  fixture.root.querySelector('[data-gfw-attachment]').click(); await flush();
  assert.equal(fixture.navigations.length, 1);
  fixture.depart();
});

test('the generic navigation guard rejects remote or unregistered destinations even in a tampered event map', async () => {
  for (const destination of ['https://untrusted.invalid', 'javascript:evil()', '//untrusted.invalid', '/not-registered', '/items/%2e%2e/private']) {
    const fixture = await connected(async () => ({ editor: { canLeave: true } }), {
      config: { componentExtensions: [name] }, openScreen: { components: [snapshot(undefined, { 'file.0': destination })] }, beforeRuntime: loadMedia });
    fixture.root.querySelector('[data-gfw-attachment]').click(); await flush();
    assert.equal(fixture.navigations.length, 0);
    assert.match(fixture.status.textContent, /not an app screen/);
    fixture.depart();
  }
});

test('detail composition nests photo and attachment previews without resetting unchanged audio when updated', () => {
  const payload = { images: [image()], attachments: [audio()], preview: { file: file(), text: 'Plain detail' } };
  const fixture = mountMedia(snapshot(payload));
  const preview = fixture.root.querySelector('[data-gfw-file-preview]');
  const gallery = fixture.root.querySelector('[data-gfw-images]');
  const player = fixture.root.querySelector('audio'), source = player.src;
  assert.equal(preview.contains(gallery), true);
  assert.equal(preview.contains(player), true);
  fixture.update([snapshot({ ...payload, preview: { ...payload.preview, caption: 'Updated detail caption' } })]);
  assert.equal(preview.contains(gallery), true);
  assert.equal(fixture.root.querySelector('audio') === player, true);
  assert.equal(player.src, source);
  assert.equal(player.pauses, 0);
  assert.equal(fixture.root.querySelectorAll('[data-gfw-image]').length, 1);
  fixture.host.dispose();
});
