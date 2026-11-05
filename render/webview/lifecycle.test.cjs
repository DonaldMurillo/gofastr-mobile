const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const { runInNewContext } = require('node:vm');
const { randomUUID } = require('node:crypto');
const { test } = require('node:test');
const { connected, flush } = require('./test-dom.cjs');

const fixture = (options = {}) => {
  const listeners = new Map(), tasks = [], timers = new Map(), errors = [], commits = [];
  const entries = [{ url: '/notes', state: { native: 'preserve' } }];
  let index = 0, timerID = 0;
  const location = {};
  const update = () => {
    const next = new URL(entries[index].url, 'https://app.test');
    Object.assign(location, { href: next.href, pathname: next.pathname, search: next.search, hash: next.hash });
  };
  update();
  const dispatch = type => {
    const event = { type, stopped: false, stopImmediatePropagation() { this.stopped = true; } };
    for (const listener of [...(listeners.get(type) || [])].sort((first, second) => second.capture - first.capture)) {
      listener.callback(event);
      if (event.stopped) break;
    }
    if (type === 'popstate' && !event.stopped && !window.__gofastr._interceptPopstate?.()) commits.push(location.pathname + location.search);
  };
  const history = {
    get state() { return entries[index].state; },
    replaceState(state, _, url) { entries[index] = { state, url }; update(); },
    pushState(state, _, url) { entries.splice(index + 1, Infinity, { state, url }); index++; update(); },
    go(distance) {
      tasks.push(() => {
        const next = index + distance;
        if (next < 0 || next >= entries.length) return;
        index = next; update(); dispatch('popstate');
      });
    },
  };
  const window = {
    __gofastr: { loadedModules: {}, _pushURL: (url, { replace = false } = {}) => history[replace ? 'replaceState' : 'pushState']({}, '', url) },
    addEventListener(type, callback, capture = false) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push({ callback, capture: capture === true });
    },
    removeEventListener(type, callback) { listeners.set(type, (listeners.get(type) || []).filter(listener => listener.callback !== callback)); },
  };
  runInNewContext(readFileSync(join(__dirname, 'lifecycle.js'), 'utf8'), {
    window, history, location, crypto: { randomUUID }, URL, Promise,
    setTimeout(callback) { timers.set(++timerID, callback); return timerID; }, clearTimeout(id) { timers.delete(id); },
  });
  const barrier = window.__gofastr_lifecycle.createHistory({ required: () => true,
    authorize: async () => () => true, report: message => errors.push(message), ...options });
  const navigate = (url, options) => { window.__gofastr._pushURL(url, options); dispatch('gofastr:navigate'); };
  const tick = async () => {
    while (tasks.length) tasks.shift()();
    await flush();
  };
  return { barrier, window, history, location, errors, commits, entries, tasks, timers, navigate, tick, get index() { return index; } };
};

test('Back restores the editor before awaiting durable leave and replays the original entry only after acknowledgement', async () => {
  let acknowledge, attempts = 0;
  const current = fixture({ authorize: () => { attempts++; return new Promise(resolve => { acknowledge = resolve; }); } });
  current.navigate('/notes/one');
  current.history.go(-1); await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(current.index, 1);
  assert.equal(attempts, 1);
  assert.equal(current.barrier.pending, true);
  assert.deepEqual(current.commits, []);
  acknowledge(() => true); await current.tick(); await current.tick();
  assert.equal(current.location.pathname, '/notes');
  assert.equal(current.entries.length, 2);
  assert.deepEqual(current.commits, ['/notes']);
  assert.equal(current.barrier.pending, false);
});

test('cancelled confirmation settles the transaction and permits a later attempt', async () => {
  let allow = false;
  const current = fixture({ authorize: async () => allow ? () => true : null });
  current.navigate('/notes/one'); current.history.go(-1); await current.tick();
  assert.equal(current.barrier.pending, false);
  assert.equal(current.location.pathname, '/notes/one');
  allow = true; current.history.go(-1); await current.tick(); await current.tick();
  assert.equal(current.location.pathname, '/notes');
  assert.deepEqual(current.commits, ['/notes']);
});

test('typing after acknowledgement invalidates the history replay instead of losing the newer buffer', async () => {
  let safe = true;
  const current = fixture({ authorize: async () => () => safe });
  current.navigate('/notes/one'); current.history.go(-1); await current.tick();
  safe = false; await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(current.barrier.pending, false);
  assert.deepEqual(current.commits, []);
});

test('checkpoint failures keep history and the editor intact without replaying the authorization', async () => {
  let attempts = 0;
  const current = fixture({ authorize: async () => { attempts++; throw new Error('Draft disk unavailable'); } });
  current.navigate('/notes/one'); current.history.go(-1); await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(current.barrier.pending, false);
  assert.equal(attempts, 1);
  assert.deepEqual(current.errors, ['Draft disk unavailable']);
  assert.deepEqual(current.commits, []);
});

test('multistep history traversal and Forward preserve entry numbering and unrelated state', async () => {
  const current = fixture();
  current.navigate('/notes/one'); current.navigate('/files'); current.navigate('/settings');
  current.history.go(-3); await current.tick(); await current.tick();
  assert.equal(current.index, 0);
  assert.equal(current.history.state.native, 'preserve');
  current.history.go(2); await current.tick(); await current.tick();
  assert.equal(current.index, 2);
  assert.deepEqual(current.commits, ['/notes', '/files']);
  assert.equal(current.entries.length, 4);
});

test('replace navigations do not invent another history position', async () => {
  const current = fixture();
  current.navigate('/notes/one'); current.navigate('/notes/two', { replace: true });
  assert.equal(current.history.state.gfwHistory.position, 1);
  current.history.go(-1); await current.tick(); await current.tick();
  assert.equal(current.location.pathname, '/notes');
  assert.equal(current.entries.length, 2);
});

test('explicit Back consumes a one-shot checked permission without asking to keep the same draft twice', async () => {
  let attempts = 0;
  const current = fixture({ authorize: async () => { attempts++; return () => true; } });
  current.navigate('/notes/one');
  assert.equal(current.barrier.traverse(-1, () => true), true);
  await current.tick();
  assert.equal(current.location.pathname, '/notes');
  assert.equal(attempts, 0);
  current.history.go(1); await current.tick(); await current.tick();
  assert.equal(attempts, 1);
});

test('a reloaded renderer preserves the current app history lineage for safe Back', async () => {
  const current = fixture();
  current.navigate('/notes/one');
  const previous = { ...current.history.state.gfwHistory };
  current.barrier.close();
  const reloaded = current.window.__gofastr_lifecycle.createHistory({ required: () => true,
    authorize: async () => () => true, report: message => current.errors.push(message) });
  assert.deepEqual({ ...current.history.state.gfwHistory }, previous);
  current.history.go(-1); await current.tick(); await current.tick();
  assert.equal(current.location.pathname, '/notes');
  assert.deepEqual(current.errors, []);
  reloaded.close();
});

test('one-shot history permissions cannot bypass a new edit or uncertain action', async () => {
  let safe = true, attempts = 0;
  const current = fixture({ authorize: async () => { attempts++; return null; } });
  current.navigate('/notes/one');
  current.barrier.traverse(-1, () => safe);
  safe = false; await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(attempts, 1);
  assert.deepEqual(current.commits, []);
});

test('additional Back while confirmation waits cancels the traversal without creating or dropping entries', async () => {
  let acknowledge, attempts = 0;
  const current = fixture({ authorize: () => { attempts++; return new Promise(resolve => { acknowledge = resolve; }); } });
  current.navigate('/notes/one'); current.navigate('/files');
  current.history.go(-1); await current.tick();
  current.history.go(-1); await current.tick();
  acknowledge(() => true); await current.tick();
  assert.equal(current.location.pathname, '/files');
  assert.equal(current.entries.length, 3);
  assert.equal(current.barrier.pending, false);
  assert.equal(attempts, 1);
  assert.deepEqual(current.commits, []);
});

test('untagged entries fail closed without running leave or navigating to an unverified destination', async () => {
  let attempts = 0;
  const current = fixture({ authorize: async () => { attempts++; return () => true; } });
  current.navigate('/notes/one'); delete current.entries[0].state.gfwHistory;
  current.history.go(-1); await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(current.barrier.pending, false);
  assert.equal(attempts, 0);
  assert.match(current.errors[0], /cannot be safely restored/);
  assert.deepEqual(current.commits, []);
});

test('an unavailable history restoration times out without admitting departure', async () => {
  const current = fixture();
  current.navigate('/notes/one'); current.history.go(-1);
  current.tasks.shift()();
  current.tasks.length = 0;
  [...current.timers.values()][0](); await current.tick();
  assert.equal(current.location.pathname, '/notes/one');
  assert.equal(current.barrier.pending, false);
  assert.match(current.errors[0], /did not finish/);
});

test('unguarded collection history is delegated unchanged and close restores the router hook', async () => {
  const current = fixture({ required: () => false });
  current.navigate('/files'); current.history.go(-1); await current.tick();
  assert.deepEqual(current.commits, ['/notes']);
  current.barrier.close();
  current.navigate('/settings');
  assert.equal(current.history.state.gfwHistory, undefined);
});

test('backgrounding checkpoints the latest editor without starting a save or reopening its session', async () => {
  const current = await connected(async command => ({ editor: { dirty: true, draftStored: true, pending: false, unknown: false } }), { openScreen: { form: {} } });
  current.field.value = 'Background draft';
  current.document.dispatchEvent({ type: 'input', target: current.field });
  current.document.hidden = true;
  current.document.dispatchEvent({ type: 'visibilitychange' });
  await flush();
  const checkpoint = current.commands.filter(command => command.kind === 'checkpoint').at(-1);
  assert.equal(checkpoint.values.title, 'Background draft');
  assert.deepEqual(current.commands.map(command => command.kind), ['hello', 'open', 'checkpoint']);
  current.document.hidden = false;
  current.document.dispatchEvent({ type: 'visibilitychange' }); await flush();
  assert.equal(current.commands.filter(command => command.kind === 'open').length, 1);
  current.depart();
});

test('a late same-revision checkpoint cannot relock a completed save', async () => {
  let releaseAction, releaseCheckpoint;
  const clean = { baseline: { title: 'Saved title' }, values: { title: 'Saved title' }, pending: false, unknown: false, dirty: false };
  const current = await connected(command => {
    if (command.kind === 'action') return new Promise(resolve => { releaseAction = resolve; });
    if (command.kind === 'checkpoint') return new Promise(resolve => { releaseCheckpoint = resolve; });
    return { editor: clean, response: { announcement: 'Saved' } };
  }, { openScreen: { form: {} } });
  await current.launch();
  current.document.hidden = true;
  current.document.dispatchEvent({ type: 'visibilitychange' }); await flush();
  assert.equal(typeof releaseCheckpoint, 'function');
  releaseAction({ response: {} }); await flush();
  releaseCheckpoint({ editor: { ...clean, pending: true } }); await flush();
  assert.equal(JSON.parse(current.root.dataset.gfwEditor).pending, false);
  current.depart();
});

test('background checkpoints wait for completion canonicalization rather than restoring an old buffer', async () => {
  let releaseCompletion;
  const current = await connected(command => {
    if (command.kind === 'action') return { response: {} };
    if (command.kind === 'complete') return new Promise(resolve => { releaseCompletion = resolve; });
    return { editor: { baseline: { title: 'Canonical title' }, values: { title: 'Canonical title' }, dirty: false } };
  }, { openScreen: { form: {} } });
  current.field.value = '  Canonical title  ';
  current.document.dispatchEvent({ type: 'input', target: current.field });
  await current.launch();
  current.document.hidden = true;
  current.document.dispatchEvent({ type: 'visibilitychange' }); await flush();
  assert.equal(current.commands.filter(command => command.kind === 'checkpoint').length, 0);
  releaseCompletion({ editor: { baseline: { title: 'Canonical title' }, values: { title: 'Canonical title' }, dirty: false }, response: {} });
  await flush();
  assert.equal(current.commands.filter(command => command.kind === 'checkpoint').length, 1);
  assert.equal(current.commands.at(-1).values.title, 'Canonical title');
  current.depart();
});

test('completion revision mismatch is bounded without replaying the write', async () => {
  const current = await connected(command => command.kind === 'action' ? { response: {} } :
    { error: { code: 'stale', message: 'Newer checkpoint' } }, { openScreen: { form: {} } });
  await current.launch();
  assert.equal(current.commands.filter(command => command.kind === 'action').length, 1);
  assert.equal(current.commands.filter(command => command.kind === 'complete').length, 2);
  assert.match(current.status.textContent, /could not be confirmed/);
  await current.launch();
  assert.equal(current.commands.filter(command => command.kind === 'action').length, 1);
  current.depart();
});
