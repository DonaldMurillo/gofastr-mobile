const assert = require('node:assert/strict');
const { test } = require('node:test');
const components = require('./components.js');

const base = 'https://app.test/records';
const config = () => ({ root: '/records', route: '/records?q=old&tag=work&page=2&selected=%5B%22one%22%5D',
  routes: [{ path: '/records', actions: ['bulk', 'pin', 'trash'] }, { path: '/records/{id}', actions: ['save'] }, { path: '/files', actions: [] }],
  tabs: [{ id: 'records', route: '/records' }, { id: 'files', route: '/files' }],
  list: { filters: [{ key: 'tag' }], selection: { key: 'selected', maximum: 2 }, paging: { key: 'page', next: '3' },
    rows: [{ id: 'one' }, { id: 'two' }, { id: 'locked', disabled: true }] } });
const field = (key, value, extra = {}) => ({ dataset: { gfwField: key }, value, defaultValue: value, type: 'text', ...extra });

test('controls serialize booleans from checked, native dates/times, empty optional values, and readonly text', () => {
  const fields = [field('pinned', 'not-the-checkbox-value', { type: 'checkbox', checked: true, defaultChecked: false }),
    field('date', '2026-11-05', { type: 'date' }), field('time', '09:07', { type: 'time' }), field('tag', ''),
    field('readonly', 'still a live value', { readOnly: true }), field('disabled', 'saved', { disabled: true })];
  assert.deepEqual(components.serialize(fields), { pinned: 'true', date: '2026-11-05', time: '09:07', tag: '', readonly: 'still a live value', disabled: 'saved' });
  fields[0].checked = false;
  assert.equal(components.serialize(fields).pinned, 'false');
  assert.equal(components.serialize(fields, 'gfwField', false, true).pinned, 'false');
});

test('toggle restoration preserves optional empty until a real change and never changes disabled controls', () => {
  const toggle = field('toggle', 'true', { type: 'checkbox', checked: true, defaultChecked: true });
  components.setField(toggle, '');
  assert.equal(toggle.checked, false);
  assert.equal(components.fieldValue(toggle), '');
  components.setField(toggle, 'false');
  assert.equal(components.fieldValue(toggle), 'false');
  components.setField(toggle, 'true');
  assert.equal(toggle.checked, true);
  toggle.disabled = true;
  components.setField(toggle, 'false');
  assert.equal(toggle.checked, true);
  const readonly = field('readonly', 'first', { readOnly: true });
  components.setField(readonly, 'recovered');
  assert.equal(readonly.value, 'recovered');
});

test('select baselines use defaultSelected instead of nonexistent select.defaultValue', () => {
  const select = field('tag', 'new', { tagName: 'SELECT', defaultValue: undefined,
    options: [{ value: 'old', defaultSelected: true }, { value: 'new' }] });
  assert.equal(components.fieldValue(select, true), 'old');
  assert.equal(components.fieldValue(select), 'new');
  assert.deepEqual(components.serialize([field('hidden', 'secret', { type: 'hidden' }), select], 'gfwField', true), { tag: 'new' });
});

test('query changes reset selection/page and paging keeps current filters and search', () => {
  const definition = config();
  const changed = new URL(components.queryRoute(definition, { q: 'new value', tag: '' }, base), base);
  assert.equal(changed.searchParams.get('q'), 'new value');
  assert.equal(changed.searchParams.get('tag'), '');
  assert.equal(changed.searchParams.has('selected'), false);
  assert.equal(changed.searchParams.has('page'), false);
  const paged = new URL(components.queryRoute(definition, { q: 'new', tag: 'work', page: '3' }, base, false), base);
  assert.equal(paged.searchParams.get('page'), '3');
  assert.equal(paged.searchParams.get('tag'), 'work');
  assert.equal(paged.searchParams.has('selected'), false);
});

test('stable selection reconciles row IDs, disabled/deleted rows, duplicates, and maximum', () => {
  const definition = config();
  assert.deepEqual(components.reconcile(['two', 'locked', 'one', 'two', 'gone'], definition.list.rows, 1), ['two']);
  assert.deepEqual(components.reconcile(['two', 'one'], [...definition.list.rows].reverse(), 2), ['two', 'one']);
  assert.deepEqual(components.reconcile(['one'], [{ id: 'one', disabled: true }], 2), []);
  assert.deepEqual(components.reconcile(null, definition.list.rows, 2), []);
});

test('registered actions serialize selected IDs as one JSON string plus live filters, never row indices', () => {
  const values = components.listValues(config(), { q: 'old', tag: 'old', retained: 'value' }, { q: 'current', tag: 'work' }, ['two', 'locked', 'one']);
  assert.equal(values.selected, '["two","one"]');
  assert.equal(values.q, 'current');
  assert.equal(values.tag, 'work');
  assert.equal(values.retained, 'value');
});

test('paging shares selection context while a changed filter/search uses a different query identity', () => {
  const first = config();
  const second = { ...first, route: '/records?selected=%5B%5D&tag=work&q=old&page=5' };
  assert.deepEqual(components.collectionIdentity(first, base), components.collectionIdentity(second, base));
  second.route = '/records?tag=personal&q=old';
  assert.notEqual(components.collectionIdentity(first, base).query, components.collectionIdentity(second, base).query);
});

test('local navigation accepts only registered local routes and never a remote URL, traversal, or encoded separator', () => {
  const definition = config();
  for (const route of ['/records', '/records/one?tag=work', '/files', '/']) assert.equal(components.localRoute(definition, route, base), route);
  for (const route of ['https://evil.test/records', '//evil.test/records', '/missing', '/records/../files', '/records/%2f', '/records/%5c', '/records/%2e%2e', '/records#write', '/records/%00', '/records/%', '/records//one', '/records\\one']) {
    assert.equal(components.localRoute(definition, route, base), null, route);
  }
});

test('restart metadata restores bounded safe routes/scroll/focus only, never fields/actions/effects', () => {
  const definition = config();
  const raw = JSON.stringify({ ...components.emptyMetadata(definition), active: 'files', actions: ['trash'], values: { body: 'private' },
    contexts: { files: { route: '/files', scroll: 500, focus: 'search', values: { body: 'private' }, action: 'trash' },
      records: { route: '/records/one', scroll: -50, focus: 'title', effects: ['camera'] }, unknown: { route: '/records' } } });
  const restored = JSON.parse(JSON.stringify(components.restoreMetadata(definition, raw, base)));
  assert.equal(restored.active, 'files');
  assert.deepEqual(restored.contexts.files, { route: '/files', scroll: 500, focus: 'search' });
  assert.deepEqual(restored.contexts.records, { route: '/records/one', scroll: 0, focus: 'title' });
  assert.equal(Object.hasOwn(restored.contexts, 'unknown'), false);
  assert.equal(JSON.stringify(restored).includes('private'), false);
  assert.equal(Object.hasOwn(restored, 'actions'), false);
});

test('restoration rejects corrupt, oversized, definition-skewed, or remote metadata', () => {
  const definition = config();
  for (const raw of ['{bad', 'x'.repeat(32769), '{}', null, JSON.stringify({ ...components.emptyMetadata(definition), version: 2 })]) {
    assert.equal(components.restoreMetadata(definition, raw, base).active, '');
  }
  const raw = JSON.stringify({ ...components.emptyMetadata(definition), active: 'records',
    contexts: { records: { route: '//evil.test/records', scroll: 1 }, files: { route: '/files', scroll: 1e15, focus: 'x'.repeat(257) } } });
  const restored = components.restoreMetadata(definition, raw, base);
  assert.equal(Object.hasOwn(restored.contexts, 'records'), false);
  assert.equal(restored.contexts.files.scroll, 10000000);
  assert.equal(restored.contexts.files.focus, '');
  const changed = { ...definition, tabs: [{ id: 'other', route: '/files' }] };
  assert.equal(components.restoreMetadata(changed, raw, base).active, '');
  assert.notEqual(components.metadataKey(changed), components.metadataKey(definition));
});

const sides = { leading: [{ id: 'pin', fullSwipe: true, role: '' }], trailing: [{ id: 'trash', fullSwipe: false, role: 'destructive' }] };
const start = extra => components.gestureStart({ x: 80, y: 100, width: 400, pointerId: 1, ...extra });
const move = (gesture, x, y = 100, pointerId = 1) => components.gestureMove(gesture, { x, y, pointerId }, sides);

test('pointer tracking yields at the native Back edge and rejects nonprimary/disabled starts', () => {
  for (const extra of [{ x: 27 }, { primary: false }, { button: 1 }, { blocked: true }, { width: 50 }]) assert.equal(start(extra), null);
  assert.equal(start({ x: 28 }).phase, 'tracking');
});

test('horizontal thresholds, safe opt-in full swipe and cancellation are deterministic and local', () => {
  const initial = start();
  assert.equal(move(initial, 85).phase, 'tracking');
  assert.equal(components.gestureEnd(move(initial, 120), sides).kind, 'cancel');
  assert.deepEqual(components.gestureEnd(move(initial, 180), sides), { kind: 'menu', side: 'leading' });
  assert.deepEqual(components.gestureEnd(move(initial, 380), sides), { kind: 'action', id: 'pin', side: 'leading' });
  assert.equal(components.gestureEnd(move(initial, 380), sides, true).kind, 'cancel');
  assert.equal(move(initial, 400, 100, 2), initial);
});

test('vertical scroll cancels without reclaiming the gesture and unavailable sides never activate', () => {
  const cancelled = move(start(), 84, 125);
  assert.equal(cancelled.phase, 'cancelled');
  assert.equal(move(cancelled, 400), cancelled);
  assert.equal(components.gestureMove(start(), { x: 200, y: 100, pointerId: 1 }, { trailing: [] }).phase, 'cancelled');
});

test('destructive and non-opt-in actions never dispatch from full swipe', () => {
  const trailing = move(start(), -240);
  const malicious = { ...sides, trailing: [{ id: 'trash', fullSwipe: true, role: 'destructive' }] };
  assert.deepEqual(components.gestureEnd(trailing, malicious), { kind: 'menu', side: 'trailing' });
  assert.equal(components.gestureEnd(move(start(), 450), { leading: [{ id: 'pin' }] }).kind, 'menu');
});
