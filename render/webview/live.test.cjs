const assert = require('node:assert/strict');
const { test } = require('node:test');
const { harness, connected, flush, plain } = require('./test-dom.cjs');
const list = () => ({ header: 'Notes', searchKey: 'q', selection: { key: 'selected', selected: ['keep'], maximum: 4 },
  rows: [{ id: 'keep', title: 'Keep selected', subtitle: 'Original', destination: '/items' }, { id: 'gone', title: 'Removed next', destination: '/items' }], refreshable: true });

const controller = (extra = {}) => {
  const fixture = harness();
  const container = fixture.document.createElement('div'); container.setAttribute('data-gfw-list', ''); fixture.root.append(container);
  let controllerIdentity = 'current', allowed = true;
  const calls = [], signals = [];
  const live = fixture.window.__gofastr_live.create({ root: fixture.root, version: 'epoch:1', allowed: () => allowed,
    identity: () => controllerIdentity,
    watch: async (version, signal) => { calls.push(version); signals.push(signal); return { dataVersion: version, ...await extra.watch?.(signal) }; },
    refresh: extra.refresh || (async () => 'epoch:2'),
  });
  return { ...fixture, live, calls, signals, identity(value) { controllerIdentity = value; }, allowed(value) { allowed = value; } };
};

test('live controller watches only visible foreground-authorized lists and cancels without waiting', async () => {
  let resolve;
  const fixture = controller({ watch: () => new Promise(value => { resolve = value; }) });
  await flush(); fixture.tick(50); await flush();
  assert.deepEqual(fixture.calls, ['epoch:1']);
  assert.equal(fixture.root.dataset.gfwLiveState, 'watching');
  fixture.live.suspend();
  assert.equal(fixture.signals[0].aborted, true);
  resolve({ changed: true, dataVersion: 'epoch:2' }); await flush();
  assert.equal(fixture.root.dataset.gfwLiveState, 'idle');
  fixture.document.hidden = true; fixture.live.resume(); fixture.tick(50); await flush();
  assert.equal(fixture.calls.length, 1);
  fixture.live.close();
  assert.equal(fixture.document.listeners.get('visibilitychange').size, 1);
});

test('watch changes coalesce into cancellable reads, with stale identity guards', async () => {
  let resolve, refreshes = 0;
  const fixture = controller({ watch: async () => ({ changed: true, dataVersion: 'epoch:2' }), refresh: async () => {
    refreshes++; return new Promise(value => { resolve = value; });
  } });
  await flush(); fixture.tick(50); await flush();
  assert.equal(refreshes, 1);
  assert.equal(fixture.root.dataset.gfwLiveState, 'loading');
  let commits = 0;
  fixture.identity('new query'); resolve(() => { commits++; return 'epoch:2'; }); await flush();
  assert.equal(commits, 0);
  assert.equal(fixture.root.dataset.gfwLiveState, 'idle');
  fixture.live.close();
});

test('connected but hidden retained lists do not watch until visible again', async () => {
  const fixture = controller();
  fixture.root.hidden = true;
  await flush(); fixture.tick(50); await flush();
  assert.equal(fixture.calls.length, 0);
  fixture.root.hidden = false;
  fixture.tick(250); await flush();
  assert.equal(fixture.calls.length, 1);
  fixture.live.close();
});

test('a staged obsolete read disposes its unused screen and a hidden root aborts its watch', async () => {
  let disposed = 0, resolve;
  const fixture = controller({ refresh: () => new Promise(value => { resolve = value; }), watch: () => new Promise(() => {}) });
  const reading = fixture.live.refresh();
  fixture.identity('superseding query');
  resolve(Object.assign(() => assert.fail('No stale commit'), { dispose() { disposed++; } }));
  await reading;
  assert.equal(disposed, 1);
  fixture.tick(50); await flush();
  fixture.root.hidden = true; fixture.mutate();
  assert.equal(fixture.signals[0].aborted, true);
  fixture.live.close();
});

test('read failure, retry and cancel operate on reads only and reject late results', async () => {
  let refreshes = 0, resolve;
  const fixture = controller({ refresh: async () => {
    refreshes++;
    if (refreshes === 1) throw new Error('Read unavailable');
    return new Promise(value => { resolve = value; });
  } });
  await fixture.live.refresh();
  assert.equal(fixture.root.dataset.gfwLiveState, 'error');
  const retry = fixture.root.querySelector('[data-gfw-live-retry]');
  assert.equal(retry.hidden, false);
  retry.click(); await flush();
  assert.equal(fixture.root.dataset.gfwLiveState, 'loading');
  fixture.root.querySelector('[data-gfw-live-cancel]').click();
  assert.equal(fixture.root.dataset.gfwLiveState, 'cancelled');
  let commits = 0; resolve(() => { commits++; return 'epoch:2'; }); await flush();
  assert.equal(commits, 0);
  assert.equal(refreshes, 2);
  assert.equal(fixture.calls.length, 0);
  fixture.live.close();
});

test('renderer live refresh sends current query, replaces only list rows, preserves selection/scroll and retires old instance', async () => {
  const initial = list();
  let watchResolve;
  const fixture = await connected(async (command, _, signal) => {
    if (command.kind === 'watch') return new Promise(resolve => { watchResolve = resolve; signal.addEventListener('abort', () => resolve({ error: { code: 'cancelled', message: 'Cancelled' } })); });
    if (command.kind === 'open') return { instance: 'instance-2', screen: { route: '/items?q=current', live: { version: 'epoch:2' }, list: {
      ...initial, rows: [{ id: 'keep', title: 'Updated <plain>', subtitle: 'New', destination: '/items' }, { id: 'new', title: 'New note', destination: '/items' }], footer: '2 notes' } } };
    return {};
  }, { list: initial, openScreen: { live: { version: 'epoch:1' } }, beforeRuntime(value) {
    const input = value.document.createElement('input'); input.setAttribute('data-gfw-query', 'q'); input.value = 'current'; value.root.append(input);
    const rendered = value.window.__gofastr_live.rows({ route: '/items?q=current', list: initial });
    value.root.querySelector('[data-gfw-collection-content]').replaceChildren(...[...rendered.children]);
  } });
  const formBefore = fixture.form;
  fixture.tick(50); await flush();
  const watch = fixture.commands.find(command => command.kind === 'watch');
  assert.deepEqual(watch.values, { q: 'current' });
  assert.equal(watch.dataVersion, 'epoch:1');
  watchResolve({ changed: true, dataVersion: 'epoch:2' }); await flush();
  assert.equal(fixture.root.dataset.gfwInstance, 'instance-2');
  assert.equal(fixture.form, formBefore);
  assert.deepEqual(fixture.root.querySelectorAll('[data-gfw-row]').map(row => row.dataset.gfwRow), ['keep', 'new']);
  assert.equal(fixture.root.querySelector('[data-gfw-select="keep"]').checked, true);
  assert.equal(fixture.root.querySelector('[data-gfw-select="new"]').checked, false);
  assert.equal(fixture.root.querySelector('h2').textContent, 'Notes');
  assert.equal(fixture.scrolls.at(-1).top, 345);
  assert.equal(fixture.commands.some(command => command.kind === 'close' && command.instance === 'instance-1'), true);
  const count = fixture.commands.filter(command => command.kind === 'open').length;
  fixture.mutate(); await flush();
  assert.equal(fixture.commands.filter(command => command.kind === 'open').length, count);
  fixture.depart();
});

test('renderer aborts a long poll before navigation lifecycle commands, not after the watch resolves', async () => {
  let watchSignal;
  const fixture = await connected(async (command, _, signal) => {
    if (command.kind === 'watch') { watchSignal = signal; return new Promise(() => {}); }
    if (command.kind === 'leave') return { editor: { canLeave: true } };
    return {};
  }, { list: list(), openScreen: { live: { version: 'epoch:1' } } });
  fixture.tick(50); await flush();
  assert.equal(watchSignal.aborted, false);
  const opened = fixture.window.__gofastr_webview.openRoute('/items'); await flush();
  assert.equal(watchSignal.aborted, true);
  assert.equal(await opened, true);
  assert.equal(fixture.navigations.length, 1);
  assert.equal(fixture.commands.some(command => command.kind === 'leave'), true);
  fixture.depart();
});

test('renderer never watches or replaces an editor even if a live/list definition is present', async () => {
  const fixture = await connected(async () => assert.fail('No live command'), { list: list(), openScreen: { live: { version: 'epoch:1' }, form: { fields: [] } } });
  assert.equal(fixture.timers.size, 0);
  assert.equal(fixture.root.querySelector('[data-gfw-live-controls]'), null);
  fixture.depart();
});

test('initial protocol snapshot replaces stale cached navigation rows before watching its version', async () => {
  const fresh = { ...list(), rows: [{ id: 'newest', title: 'Current repository version', destination: '/items' }] };
  const fixture = await connected(async () => ({}), { list: list(), openScreen: { list: fresh, live: { version: 'epoch:new' } } });
  assert.deepEqual(fixture.root.querySelectorAll('[data-gfw-row]').map(row => row.dataset.gfwRow), ['newest']);
  fixture.depart();
});

test('paging updates hidden route values so later actions cannot submit the preceding page', async () => {
  const initial = { ...list(), paging: { key: 'page', next: '2', label: 'Load more' } };
  let opens = 0;
  const fixture = await connected(async command => {
    if (command.kind === 'leave') return { editor: { canLeave: true } };
    if (command.kind === 'open') return { instance: 'fresh-' + ++opens, screen: { route: '/items?page=2', list: { ...initial, paging: { key: 'page', next: '' } }, values: { page: '2' } } };
    return { response: {} };
  }, { list: initial, config: { route: '/items?page=1' }, openScreen: { route: '/items?page=1', values: { page: '1' } }, beforeRuntime(value) {
    const page = value.document.createElement('input'); page.setAttribute('type', 'hidden'); page.setAttribute('data-gfw-field', 'page'); page.value = '1'; value.form.append(page);
  } });
  fixture.root.querySelector('[data-gfw-load-more]').click(); await flush();
  assert.equal(fixture.form.querySelector('[data-gfw-field="page"]').value, '2');
  await fixture.launch();
  assert.equal(fixture.commands.find(command => command.kind === 'action').values.page, '2');
  fixture.depart();
});

test('collection refresh adopts and retires opaque action values with its current toolbar', async () => {
  let refreshes = 0;
  const fixture = await connected(async command => {
    if (command.kind === 'open') {
      refreshes++;
      return { instance: 'fresh-' + refreshes, screen: { route: '/items', list: list(), values: refreshes === 1 ? { undo: 'receipt-opaque' } : {},
        actions: refreshes === 1 ? [{ id: 'undo', label: 'Undo last change', commit: true }] : [] } };
    }
    return {};
  }, { list: list(), beforeRuntime(value) {
    const toolbar = value.document.createElement('div'); toolbar.setAttribute('class', 'gfw-selection-toolbar');
    const count = value.document.createElement('span'); count.setAttribute('data-gfw-selection-count', '');
    toolbar.append(count); value.root.append(toolbar);
  } });
  fixture.root.querySelector('[data-gfw-live-retry]').click(); await flush();
  assert.equal(fixture.form.querySelector('[data-gfw-field="undo"]').value, 'receipt-opaque');
  const toolbar = fixture.root.querySelector('[data-gfw-selection-count]').parentElement;
  const action = toolbar.querySelector('button');
  assert.equal(action.textContent, 'Undo last change');
  assert.equal(action.getAttribute('formaction'), '/_mobile/actions?route=%2Fitems&action=undo');
  fixture.root.querySelector('[data-gfw-live-retry]').click(); await flush();
  assert.equal(fixture.form.querySelector('[data-gfw-field="undo"]'), null);
  assert.equal(toolbar.querySelector('button'), null);
  fixture.depart();
});

test('renderer ignores a late refresh after retirement and closes its unused instance', async () => {
  let resolve;
  const fixture = await connected(async command => {
    if (command.kind === 'open') return new Promise(value => { resolve = value; });
    return {};
  }, { list: list() });
  fixture.root.querySelector('[data-gfw-live-retry]').click(); await flush();
  fixture.depart();
  resolve({ instance: 'late-instance', screen: { route: '/items', list: list() } }); await flush();
  assert.equal(fixture.commands.some(command => command.kind === 'close' && command.instance === 'late-instance'), true);
  assert.notEqual(fixture.root.dataset.gfwInstance, 'late-instance');
});

test('refreshed semantic rows keep menus, disabled states, safe text and paging query', () => {
  const fixture = harness();
  const rendered = fixture.window.__gofastr_live.rows({ route: '/items?q=latest', list: { header: 'Notes <safe>', selection: { key: 'selected' }, rows: [
    { id: 'one', title: '<script>bad()</script>', metadata: 'Pinned · Ideas <safe>', icon: 'file', disabled: true, destination: '/items', trailingActions: [{ id: 'delete', label: 'Delete', role: 'destructive', fullSwipe: true }] }], paging: { key: 'page', next: '2' } } });
  const row = rendered.querySelector('[data-gfw-row]');
  assert.equal(row.dataset.gfwDisabled, 'true');
  assert.equal(row.querySelector('[data-gfw-select]').disabled, true);
  assert.equal(row.querySelector('a'), null);
  const action = row.querySelector('[data-gfw-row-action]');
  assert.equal(action.disabled, true);
  assert.equal(action.dataset.gfwFullSwipe, undefined);
  assert.equal(row.querySelectorAll('span').find(node => node.getAttribute('class') === 'gfw-row-metadata').textContent, 'Pinned · Ideas <safe>');
  assert.equal(row.querySelector('[aria-hidden="true"]').dataset.gfwFileIcon, 'file');
  assert.equal(row.querySelector('path').getAttribute('d').startsWith('M14 2H6'), true);
  assert.equal(row.querySelector('summary').textContent, '•••');
  assert.equal(rendered.querySelector('[data-gfw-load-more]').getAttribute('href'), '/items?q=latest&page=2');
  assert.equal(rendered.querySelector('h2').textContent, 'Notes <safe>');
});
