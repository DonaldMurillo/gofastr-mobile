const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { randomUUID } = require('node:crypto');
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
    for (const listener of this.listeners.get(event.type) || []) listener(event);
    return !event.defaultPrevented;
  }
}

class Element extends Target {
  attributes = new Map();
  children = [];
  dataset = {};
  disabled = false;
  checked = false;
  open = false;
  textContent = '';
  constructor(document, tag) {
    super();
    this.document = document;
    this.tagName = tag.toUpperCase();
  }
  get isConnected() { return this === this.document.body || !!this.parent?.isConnected; }
  get id() { return this.getAttribute('id'); }
  get value() { return this.currentValue ?? this.getAttribute('value') ?? ''; }
  set value(value) { this.currentValue = value; }
  get type() { return this.getAttribute('type') || ''; }
  set innerHTML(_) { throw new Error('Untrusted HTML is not supported'); }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    if (name.startsWith('data-')) this.dataset[name.slice(5).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())] = String(value);
  }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  hasAttribute(name) { return this.attributes.has(name); }
  removeAttribute(name) { this.attributes.delete(name); }
  append(...children) {
    for (const child of children) {
      child.parent = this;
      this.children.push(child);
    }
  }
  remove() {
    if (this.parent) this.parent.children = this.parent.children.filter((child) => child !== this);
    this.parent = null;
  }
  contains(node) { return node === this || this.children.some((child) => child.contains(node)); }
  matches(selector) {
    return selector.split(',').some((part) => {
      const match = part.trim().match(/^([a-z][a-z0-9]*)?(?:\[([^=\]]+)(?:="([^"]*)")?\])?$/);
      return !!match && (!match[1] || this.tagName === match[1].toUpperCase()) &&
        (!match[2] || (this.hasAttribute(match[2]) && (match[3] === undefined || this.getAttribute(match[2]) === match[3])));
    });
  }
  closest(selector) { return this.matches(selector) ? this : this.parent?.closest(selector) || null; }
  querySelectorAll(selector) {
    return this.children.flatMap((child) => [...(child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]);
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  showModal() { this.open = true; }
  close() { this.open = false; }
  getBoundingClientRect() { return { left: 100, right: 400, top: 100, bottom: 500 }; }
  focus() {
    this.document.activeElement = this;
    this.document.dispatchEvent({ type: 'focusin', target: this });
  }
  click() { if (!this.disabled) this.dispatchEvent({ type: 'click', target: this }); }
}

class Form extends Element {}

const flush = () => new Promise((resolve) => setImmediate(resolve));
const choice = (identifier, role = '', disabled = false) => ({ id: identifier, label: identifier, role, disabled });
const definition = (extra = {}) => ({ id: 'go-continuation', kind: 'dialog', title: 'Choose <not HTML>', message: 'Message <script>unsafe()</script>',
  dismissible: true, choices: [choice('apply'), choice('cancel', 'cancel'), choice('delete', 'destructive'), choice('unavailable', '', true)], ...extra });

const harness = () => {
  const document = new Target();
  document.createElement = (tag) => new (tag === 'form' ? Form : Element)(document, tag);
  document.body = document.createElement('body');
  document.documentElement = document.body;
  document.activeElement = document.body;
  document.querySelectorAll = (selector) => document.body.querySelectorAll(selector);
  document.querySelector = (selector) => document.body.querySelector(selector);
  const window = new Target();
  window.__gofastr = { loadedModules: {}, invalidate() {} };
  const context = { document, window, HTMLFormElement: Form, navigator: {}, crypto: { randomUUID }, URL, URLSearchParams,
    history: { state: { gfwAnnotated: true }, replaceState() { throw new Error('Presentations must not change history'); } },
    location: { href: 'https://app.test/items/42', protocol: 'https:', host: 'app.test' },
    MutationObserver: class { observe() {} }, setTimeout, clearTimeout,
    CustomEvent: class { constructor(type, options) { this.type = type; Object.assign(this, options); } } };
  runInNewContext(readFileSync(join(__dirname, 'presentation.js'), 'utf8'), context);
  const root = document.createElement('div');
  root.setAttribute('data-gfw-screen', '');
  const screen = document.createElement('section');
  screen.setAttribute('data-gfm-platform', 'ios');
  const opener = document.createElement('button');
  screen.append(opener);
  root.append(screen);
  document.body.append(root);
  opener.focus();
  return { context, document, window, root, screen, opener, adapter: window.__gofastr_presentations,
    open(presentation = definition(), respond = async () => {}) {
      window.__gofastr_presentations.open({ root, opener, presentation, presentationId: 'issued-instance', respond });
      return document.querySelector('[data-gfw-presentation]');
    } };
};

test('all presentation kinds are labelled modal overlays with safe plain text', () => {
  const fixture = harness();
  for (const kind of ['alert', 'confirmation', 'menu', 'dialog', 'sheet']) {
    const dialog = fixture.open(definition({ kind }));
    assert.equal(fixture.screen.contains(dialog), true);
    assert.equal(dialog.open, true);
    assert.equal(dialog.dataset.gfwPresentation, kind);
    assert.equal(dialog.dataset.gfwPresentationId, 'issued-instance');
    assert.equal(dialog.getAttribute('role'), ['alert', 'confirmation'].includes(kind) ? 'alertdialog' : 'dialog');
    assert.equal(dialog.getAttribute('aria-modal'), 'true');
    assert.equal(dialog.querySelector('h2').textContent, 'Choose <not HTML>');
    assert.equal(dialog.querySelector('p').textContent, 'Message <script>unsafe()</script>');
    assert.equal(dialog.getAttribute('aria-labelledby'), dialog.querySelector('h2').id);
    assert.equal(dialog.getAttribute('aria-describedby'), dialog.querySelector('p').id);
    assert.equal(dialog.querySelectorAll('[data-gfw-choice]').length, 4);
    assert.equal(dialog.querySelector('[data-gfw-choice="unavailable"]').disabled, true);
    assert.equal(fixture.document.querySelectorAll('[data-gfw-presentation]').length, 1);
  }
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.document.activeElement, fixture.opener);
  assert.equal(fixture.document.querySelector('[data-gfw-presentation]'), null);
});

test('selection uses native radio or checkbox controls with detail labels', async () => {
  const fixture = harness();
  for (const multiple of [false, true]) {
    let submitted;
    const dialog = fixture.open(definition({ selection: { multiple, selected: ['first'], min: 1, max: 2,
      options: [{ id: 'first', label: 'First <plain>', detail: 'Detail <plain>' }, { id: 'second', label: 'Second', disabled: true }] } }),
    async (choiceId, selected) => { submitted = { choiceId, selected: [...selected] }; });
    const [first, second] = dialog.querySelectorAll('[data-gfw-option]');
    assert.equal(first.type, multiple ? 'checkbox' : 'radio');
    assert.equal(first.checked, true);
    assert.equal(second.disabled, true);
    assert.equal(fixture.document.activeElement, first);
    assert.equal(first.getAttribute('aria-labelledby'), dialog.querySelector('[id="' + first.getAttribute('aria-labelledby') + '"]').id);
    assert.equal(dialog.querySelector('[id="' + first.getAttribute('aria-describedby') + '"]').textContent, 'Detail <plain>');
    dialog.querySelector('[data-gfw-choice="apply"]').click();
    await flush();
    assert.deepEqual(submitted, { choiceId: 'apply', selected: ['first'] });
    assert.equal(second.disabled, true);
  }
});

test('responses lock controls, suppress duplicate submits, and preserve rejected selections', async () => {
  const fixture = harness();
  let rejectResponse;
  let calls = 0;
  const dialog = fixture.open(definition({ selection: { multiple: true, selected: ['first'], options: [{ id: 'first', label: 'First' }] } }),
    () => { calls++; return new Promise((_, reject) => { rejectResponse = reject; }); });
  const button = dialog.querySelector('[data-gfw-choice="apply"]');
  button.click();
  button.click();
  assert.equal(calls, 1);
  assert.equal(dialog.getAttribute('aria-busy'), 'true');
  assert.ok(dialog.querySelectorAll('button, input').every((control) => control.disabled));
  rejectResponse(new Error('Choose at least two options.'));
  await flush();
  assert.equal(dialog.open, true);
  assert.equal(dialog.querySelector('[data-gfw-option]').checked, true);
  assert.equal(button.disabled, false);
  assert.equal(dialog.querySelector('[data-gfw-choice="unavailable"]').disabled, true);
  assert.equal(dialog.querySelector('[data-gfw-presentation-status]').textContent, 'Choose at least two options.');
  assert.equal(dialog.querySelector('[data-gfw-presentation-status]').getAttribute('aria-live'), 'assertive');
  button.click();
  assert.equal(calls, 2);
  rejectResponse(new Error('Still invalid'));
  await flush();
});

test('Escape and backdrop cancellation obey dismissible without inventing a choice', async () => {
  const fixture = harness();
  const responses = [];
  for (const dismissible of [false, true]) {
    const dialog = fixture.open(definition({ dismissible }), async (choiceId, selected) => responses.push([choiceId, [...selected]]));
    const escape = { type: 'keydown', key: 'Escape' };
    dialog.dispatchEvent(escape);
    await flush();
    assert.equal(escape.defaultPrevented, true);
    dialog.dispatchEvent({ type: 'pointerdown', clientX: 10, clientY: 10 });
    dialog.dispatchEvent({ type: 'click', clientX: 10, clientY: 10 });
    await flush();
    dialog.dispatchEvent({ type: 'pointerdown', clientX: 150, clientY: 150 });
    dialog.dispatchEvent({ type: 'click', clientX: 10, clientY: 10 });
    await flush();
    assert.equal(responses.length, dismissible ? 2 : 0);
  }
  assert.deepEqual(responses, [['', []], ['', []]]);
});

test('focus stays contained and chained overlays restore the original opener', async () => {
  const fixture = harness();
  const editor = fixture.document.createElement('textarea');
  editor.value = 'Unsaved local buffer';
  fixture.root.append(editor);
  const dialog = fixture.open(definition({ kind: 'menu' }), async (_, __, opener) => {
    fixture.adapter.close(fixture.root, false);
    fixture.adapter.open({ root: fixture.root, opener, presentation: definition({ kind: 'confirmation' }), presentationId: 'next-instance', respond: async () => {} });
  });
  const buttons = dialog.querySelectorAll('button').filter((button) => !button.disabled);
  buttons[buttons.length - 1].focus();
  dialog.dispatchEvent({ type: 'keydown', key: 'Tab' });
  assert.equal(fixture.document.activeElement, buttons[0]);
  dialog.dispatchEvent({ type: 'keydown', key: 'Tab', shiftKey: true });
  assert.equal(fixture.document.activeElement, buttons[buttons.length - 1]);
  fixture.opener.focus();
  assert.equal(fixture.document.activeElement, buttons[0]);
  buttons[0].click();
  await flush();
  assert.equal(dialog.isConnected, false);
  assert.equal(fixture.document.querySelectorAll('[data-gfw-presentation]').length, 1);
  assert.equal(fixture.document.querySelector('[data-gfw-presentation]').dataset.gfwPresentation, 'confirmation');
  assert.equal(editor.value, 'Unsaved local buffer');
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.document.activeElement, fixture.opener);
});

const connectedHarness = async (onCommand, options = {}) => {
  const fixture = harness();
  const form = fixture.document.createElement('form');
  form.setAttribute('data-gfw-form', '');
  const field = fixture.document.createElement('input');
  field.setAttribute('data-gfw-field', 'title');
  field.setAttribute('type', 'text');
  field.value = field.defaultValue = 'Saved title';
  form.append(field);
  const live = fixture.document.createElement('p');
  live.setAttribute('data-gfw-status', '');
  fixture.root.append(form, live);
  fixture.opener.setAttribute('form', 'gfw-form');
  fixture.opener.value = 'show';
  fixture.root.dataset.gfwConfig = JSON.stringify(options.config || { root: '/items/42', route: '/items/42', routes: [{ path: '/items/{id}', actions: ['show'] }] });
  const commands = [];
  fixture.window.__gofastr_transport = async (_, transportOptions) => {
    const command = JSON.parse(transportOptions.body);
    commands.push(command);
    let reply;
    if (command.kind === 'hello') reply = { connection: 'connection', contract: {} };
    else if (command.kind === 'open') reply = { instance: 'screen-instance', ...options.openReply };
    else reply = await onCommand(command, fixture);
    return { ok: true, json: async () => ({ version: 1, revision: command.revision, ...reply }) };
  };
  options.setup?.({ ...fixture, form, field, live });
  runInNewContext(readFileSync(join(__dirname, 'components.js'), 'utf8'), fixture.context);
  runInNewContext(readFileSync(join(__dirname, 'webview.js'), 'utf8'), fixture.context);
  await flush();
  return { ...fixture, form, field, live, commands, launch: async () => {
    fixture.document.dispatchEvent({ type: 'submit', target: form, submitter: fixture.opener });
    await flush();
    return fixture.document.querySelector('[data-gfw-presentation]');
  } };
};

test('renderer completes actions, responds with opaque identity and latest buffers, and completes accepted responses', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'opaque-issued', response: { presentation: definition() } };
    if (command.kind === 'respond') return { response: {} };
    if (command.kind === 'complete') return { response: { announcement: 'Applied by Go' }, effectIds: { announcement: 'issued-announcement' } };
    return {};
  });
  const dialog = await fixture.launch();
  assert.equal(fixture.commands[0].hello.capabilities['presentation/v1'], 'available');
  assert.deepEqual(fixture.commands.map((command) => command.kind), ['hello', 'open', 'action', 'complete']);
  fixture.field.value = 'Newer editor buffer';
  fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  dialog.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  const response = fixture.commands.find((command) => command.kind === 'respond');
  assert.equal(response.presentationId, 'opaque-issued');
  assert.equal(response.choice, 'apply');
  assert.deepEqual(response.selected, []);
  assert.notEqual(response.requestId, actionRequest);
  assert.equal(response.values.title, 'Newer editor buffer');
  assert.equal(response.revision, 1);
  assert.deepEqual(fixture.commands.slice(-2).map((command) => command.kind), ['complete', 'ack']);
  assert.equal(fixture.commands.findLast((command) => command.kind === 'complete').requestId, response.requestId);
  assert.equal(fixture.document.querySelector('[data-gfw-presentation]'), null);
  assert.equal(fixture.field.value, 'Newer editor buffer');
  assert.equal(fixture.live.textContent, 'Applied by Go');
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('pre-admission validation keeps the overlay correctable without completing an unaccepted response', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'opaque-issued', response: { presentation: definition({ selection: {
      multiple: true, selected: [], min: 1, max: 1, options: [{ id: 'first', label: 'First' }, { id: 'second', label: 'Second' }],
    } }) } };
    if (command.kind === 'respond' && !command.selected.length) return { error: { code: 'validation', message: 'Choose at least one option.' } };
    return { response: {} };
  });
  const dialog = await fixture.launch();
  const apply = dialog.querySelector('[data-gfw-choice="apply"]');
  apply.click();
  await flush();
  assert.equal(fixture.commands.at(-1).kind, 'respond');
  assert.equal(dialog.isConnected, true);
  assert.equal(apply.disabled, false);
  assert.equal(dialog.querySelector('[data-gfw-presentation-status]').textContent, 'Choose at least one option.');
  const firstRequest = fixture.commands.at(-1).requestId;
  dialog.querySelector('[data-gfw-option="first"]').checked = true;
  apply.click();
  await flush();
  const secondResponse = fixture.commands.findLast((command) => command.kind === 'respond');
  assert.notEqual(secondResponse.requestId, firstRequest);
  assert.deepEqual(secondResponse.selected, ['first']);
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  assert.equal(dialog.isConnected, false);
});

test('post-admission handler validation completes and closes rather than offering a duplicate continuation', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'opaque-issued', response: { presentation: definition() } };
    return { response: {}, error: { code: 'validation', message: 'Go handler already ran.' } };
  });
  const dialog = await fixture.launch();
  dialog.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  assert.equal(fixture.commands.at(-1).kind, 'complete');
  assert.equal(dialog.isConnected, false);
  assert.equal(fixture.live.textContent, 'Go handler already ran.');
});

test('renderer removes a consumed presentation before mounting its chained successor', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'respond') return { response: {} };
    return { presentationId: command.requestId === actionRequest ? 'menu-issued' : 'confirmation-issued',
      response: { presentation: definition({ kind: command.requestId === actionRequest ? 'menu' : 'confirmation' }) } };
  });
  const menu = await fixture.launch();
  menu.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  assert.equal(menu.isConnected, false);
  assert.equal(fixture.document.querySelectorAll('[data-gfw-presentation]').length, 1);
  const confirmation = fixture.document.querySelector('[data-gfw-presentation]');
  assert.equal(confirmation.dataset.gfwPresentation, 'confirmation');
  assert.equal(confirmation.dataset.gfwPresentationId, 'confirmation-issued');
  fixture.adapter.close(fixture.root);
  assert.equal(fixture.document.activeElement, fixture.opener);
});

test('renderer completes gesture and explicit cancellation without dispatching a replacement action', async () => {
  for (const dismiss of ['escape', 'backdrop', 'choice']) {
    let actionRequest;
    const fixture = await connectedHarness(async (command) => {
      if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
      if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'cancel-issued', response: { presentation: definition() } };
      return { response: {} };
    });
    const dialog = await fixture.launch();
    if (dismiss === 'escape') dialog.dispatchEvent({ type: 'keydown', key: 'Escape' });
    else if (dismiss === 'backdrop') {
      dialog.dispatchEvent({ type: 'pointerdown', clientX: 10, clientY: 10 });
      dialog.dispatchEvent({ type: 'click', clientX: 10, clientY: 10 });
    } else dialog.querySelector('[data-gfw-choice="cancel"]').click();
    await flush();
    const response = fixture.commands.find((command) => command.kind === 'respond');
    assert.equal(response.choice, dismiss === 'choice' ? 'cancel' : '');
    assert.equal(response.presentationId, 'cancel-issued');
    assert.deepEqual(response.selected, []);
    assert.deepEqual(fixture.commands.slice(-2).map((command) => command.kind), ['respond', 'complete']);
    assert.equal(fixture.commands.filter((command) => command.kind === 'action').length, 1);
    assert.equal(dialog.isConnected, false);
    assert.equal(fixture.document.activeElement, fixture.opener);
  }
});

test('a lost response is observed read-only instead of replaying the Go continuation', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'opaque-issued', response: { presentation: definition() } };
    if (command.kind === 'respond') throw new Error('Response transport was lost');
    return { response: { announcement: 'Observed completed response' } };
  });
  const dialog = await fixture.launch();
  dialog.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  assert.equal(fixture.commands.filter((command) => command.kind === 'respond').length, 1);
  assert.equal(fixture.commands.slice(-2).every((command) => command.kind === 'complete'), true);
  assert.equal(dialog.isConnected, false);
  assert.equal(fixture.live.textContent, 'Observed completed response');
});

test('invalid_request before admission is correctable and never completes an unaccepted request', async () => {
  let actionRequest;
  const fixture = await connectedHarness(async (command) => {
    if (command.kind === 'action') { actionRequest = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionRequest) return { presentationId: 'opaque-issued', response: { presentation: definition() } };
    return { error: { code: 'invalid_request', message: 'Choose an available action.' } };
  });
  const dialog = await fixture.launch();
  dialog.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  assert.equal(fixture.commands.at(-1).kind, 'respond');
  assert.equal(dialog.isConnected, true);
  assert.equal(dialog.querySelector('[data-gfw-choice="apply"]').disabled, false);
  assert.equal(dialog.querySelector('[data-gfw-presentation-status]').textContent, 'Choose an available action.');
});

test('IME defers checkpoints, preserves live text and checkbox state, and refuses navigation while composing', async () => {
  const timers = new Map();
  let toggle;
  const fixture = await connectedHarness(async command => ({ editor: { baseline: { title: 'Saved title', pinned: 'false' }, values: command.values, dirty: true, canLeave: false } }), {
    openReply: { editor: { baseline: { title: 'Saved title', pinned: 'false' }, values: { title: 'Saved title', pinned: 'false' } } },
    setup({ document, form, context, window }) {
      context.setTimeout = (callback, delay) => { const timer = {}; timers.set(timer, { callback, delay }); return timer; };
      context.clearTimeout = timer => timers.delete(timer);
      window.__gofastr.navigate = () => assert.fail('A composing editor must not navigate');
      toggle = document.createElement('input');
      toggle.setAttribute('data-gfw-field', 'pinned');
      toggle.setAttribute('type', 'checkbox');
      toggle.dataset.gfwInitial = 'false';
      toggle.checked = false;
      form.append(toggle);
    },
  });
  fixture.document.dispatchEvent({ type: 'compositionstart', target: fixture.field });
  fixture.field.value = 'Unfinished composition';
  fixture.document.dispatchEvent({ type: 'input', target: fixture.field, isComposing: true });
  toggle.checked = true;
  fixture.document.dispatchEvent({ type: 'input', target: toggle });
  assert.equal(timers.size, 0);
  assert.equal(await fixture.window.__gofastr_webview.openRoute('/items/other'), false);
  assert.equal(fixture.commands.some(command => command.kind === 'checkpoint'), false);
  assert.equal(fixture.field.value, 'Unfinished composition');
  fixture.document.dispatchEvent({ type: 'compositionend', target: fixture.field });
  assert.equal(timers.size, 1);
  for (const { callback, delay } of timers.values()) { assert.equal(delay, 250); callback(); }
  await flush();
  const checkpoint = fixture.commands.find(command => command.kind === 'checkpoint');
  assert.equal(checkpoint.values.title, 'Unfinished composition');
  assert.equal(checkpoint.values.pinned, 'true');
  assert.equal(toggle.checked, true);
});

test('row menu actions include stable row ID, JSON selection and current filters, then refresh without inventing navigation', async () => {
  let actionID;
  let rowButton;
  const navigations = [];
  const fixture = await connectedHarness(async command => {
    if (command.kind === 'action') { actionID = command.requestId; return { response: {} }; }
    if (command.kind === 'complete' && command.requestId === actionID) return { response: { announcement: 'Pinned by Go' } };
    return {};
  }, {
    config: { root: '/items', route: '/items?tag=work&q=needle&page=2', routes: [{ path: '/items', actions: ['pin'] }],
      list: { rows: [{ id: 'stable-two', leadingActions: [{ id: 'pin', label: 'Pin', fullSwipe: true }] }, { id: 'stable-one' }],
        selection: { key: 'selected', selected: ['stable-one'], maximum: 2 }, filters: [{ key: 'tag' }], paging: { key: 'page', next: '3' } } },
    setup({ document, root, window }) {
      window.__gofastr.navigate = (path, options) => navigations.push({ path, ...options });
      for (const [marker, key, value] of [['data-gfw-filter', 'tag', 'work'], ['data-gfw-query', 'q', 'needle']]) {
        const control = document.createElement('input');
        control.setAttribute(marker, key);
        control.value = value;
        root.append(control);
      }
      rowButton = document.createElement('button');
      rowButton.setAttribute('form', 'gfw-form');
      rowButton.setAttribute('data-gfw-row-action', 'pin');
      rowButton.setAttribute('data-gfw-row-id', 'stable-two');
      rowButton.value = 'pin';
      root.append(rowButton);
    },
  });
  fixture.document.dispatchEvent({ type: 'submit', target: fixture.form, submitter: rowButton });
  await flush();
  const action = fixture.commands.find(command => command.kind === 'action');
  assert.equal(action.action, 'pin');
  assert.equal(action.values.row, 'stable-two');
  assert.equal(action.values.selected, '["stable-one"]');
  assert.equal(action.values.tag, 'work');
  assert.equal(action.values.q, 'needle');
  assert.equal(fixture.commands.filter(command => command.kind === 'action').length, 1);
  assert.deepEqual(navigations, [{ path: '/items?tag=work&q=needle&page=2', replace: true, force: true }]);
});

test('tab switching checkpoints/keeps through the existing leave policy and uses only GoFastr navigation', async () => {
  const navigations = [];
  const fixture = await connectedHarness(async command => {
    if (command.kind === 'leave') return { editor: { baseline: { title: 'Saved title' }, values: command.values,
      dirty: true, canLeave: command.choice === 'keep', draftStored: true } };
    return {};
  }, {
    config: { root: '/items', route: '/items/42', routes: [{ path: '/items', actions: [] }, { path: '/items/{id}', actions: ['show'] }, { path: '/files', actions: [] }],
      tabs: [{ id: 'items', route: '/items' }, { id: 'files', route: '/files' }], activeTab: 'items' },
    setup({ document, root, window }) {
      window.__gofastr.navigate = (path, options) => navigations.push({ path, ...options });
      window.confirm = () => true;
      const tab = document.createElement('a');
      tab.setAttribute('href', '/files');
      tab.setAttribute('data-gfw-tab', 'files');
      root.append(tab);
    },
  });
  fixture.field.value = 'New unsaved text';
  fixture.document.dispatchEvent({ type: 'input', target: fixture.field });
  fixture.document.dispatchEvent({ type: 'click', target: fixture.root.querySelector('[data-gfw-tab]'), button: 0 });
  await flush();
  const leaves = fixture.commands.filter(command => command.kind === 'leave');
  assert.deepEqual(leaves.map(command => command.choice || ''), ['', 'keep']);
  assert.equal(leaves[1].values.title, 'New unsaved text');
  assert.deepEqual(navigations, [{ path: '/files', force: true }]);
  assert.equal(fixture.field.value, 'New unsaved text');
});

test('local route entry rejects remote paths and pending/unknown editors without opening or dispatching actions', async () => {
  for (const blocked of ['pending', 'unknown']) {
    const navigations = [];
    const fixture = await connectedHarness(async () => ({}), {
      openReply: { editor: { baseline: { title: 'Saved title' }, values: { title: 'Saved title' }, [blocked]: true } },
      setup({ window }) { window.__gofastr.navigate = path => navigations.push(path); },
    });
    assert.equal(await fixture.window.__gofastr_webview.openRoute('//evil.test/items/42'), false);
    assert.equal(await fixture.window.__gofastr_webview.openRoute('/items/other'), false);
    assert.equal(fixture.commands.some(command => command.kind === 'leave' || command.kind === 'action'), false);
    assert.deepEqual(navigations, []);
  }
});

test('query edits clear page/selection and discard an older delayed leave result before requesting navigation', async () => {
  const pending = [];
  const navigations = [];
  let queryForm;
  let search;
  const fixture = await connectedHarness(async command => {
    if (command.kind === 'leave') return new Promise(resolve => pending.push(resolve));
    return {};
  }, {
    config: { root: '/items', route: '/items?q=old&page=2', routes: [{ path: '/items', actions: [] }],
      list: { searchKey: 'q', rows: [{ id: 'one' }], selection: { key: 'selected', selected: ['one'], maximum: 2 }, paging: { key: 'page', next: '3' } } },
    setup({ document, root, window }) {
      window.__gofastr.navigate = (path, options) => navigations.push({ path, ...options });
      queryForm = document.createElement('form');
      queryForm.setAttribute('data-gfw-search', '');
      search = document.createElement('input');
      search.setAttribute('data-gfw-query', 'q');
      queryForm.append(search);
      root.append(queryForm);
    },
  });
  search.value = 'first';
  fixture.document.dispatchEvent({ type: 'submit', target: queryForm });
  await flush();
  search.value = 'latest';
  fixture.document.dispatchEvent({ type: 'submit', target: queryForm });
  await flush();
  assert.equal(pending.length, 1);
  pending[0]({});
  await flush();
  assert.deepEqual(navigations, [{ path: '/items?q=latest', replace: true, force: true }]);
  assert.equal(fixture.commands.some(command => command.kind === 'action'), false);
});

test('retained roots reopen after paging and same-config refresh without losing cancel selections', async () => {
  const replies = new Map();
  const navigations = [];
  const config = { root: '/items', route: '/items', routes: [{ path: '/items', actions: ['show'] }],
    list: { rows: [{ id: 'one' }, { id: 'two' }], selection: { key: 'selected', maximum: 20 }, paging: { key: 'page', next: '2' } } };
  let paging;
  let checkboxes;
  let undo;
  const fixture = await connectedHarness(async command => {
    if (command.kind === 'action') replies.set(command.requestId, { presentationId: 'issued-' + command.requestId, response: { presentation: definition() } });
    if (command.kind === 'respond') replies.set(command.requestId, { response: { announcement: command.choice === 'cancel' ? 'Cancelled' : 'Archived' } });
    if (command.kind === 'complete') return replies.get(command.requestId);
    return {};
  }, { config, setup({ document, root, form, window }) {
    window.__gofastr.navigate = (path, options) => navigations.push({ path, ...options });
    paging = document.createElement('a');
    paging.setAttribute('data-gfw-load-more', '');
    paging.setAttribute('href', '/items?page=2');
    undo = document.createElement('input');
    undo.setAttribute('data-gfw-field', 'undo');
    undo.setAttribute('type', 'hidden');
    undo.value = undo.defaultValue = '';
    form.append(undo);
    checkboxes = config.list.rows.map(row => {
      const checkbox = document.createElement('input');
      checkbox.setAttribute('type', 'checkbox');
      checkbox.setAttribute('data-gfw-select', row.id);
      root.append(checkbox);
      return checkbox;
    });
    root.append(paging);
  } });
  fixture.document.dispatchEvent({ type: 'click', target: paging, button: 0 });
  await flush();
  assert.deepEqual(navigations, [{ path: '/items?page=2', replace: false, force: true }]);
  config.route = '/items?page=2';
  config.list.rows.push({ id: 'three' });
  fixture.root.dataset.gfwConfig = JSON.stringify(config);
  fixture.window.dispatchEvent({ type: 'gofastr:navigate', detail: { prevPath: '/items' } });
  await flush();
  assert.equal(fixture.commands.filter(command => command.kind === 'open').length, 2);
  assert.equal(fixture.commands.filter(command => command.kind === 'close').length, 1);
  for (const checkbox of checkboxes) {
    assert.equal(checkbox.disabled, false);
    checkbox.checked = true;
    fixture.document.dispatchEvent({ type: 'change', target: checkbox });
    assert.equal(checkbox.checked, true);
  }
  const cancelled = await fixture.launch();
  assert.equal(fixture.commands.filter(command => command.kind === 'action').at(-1).values.selected, '["one","two"]');
  cancelled.querySelector('[data-gfw-choice="cancel"]').click();
  await flush();
  fixture.window.dispatchEvent({ type: 'gofastr:navigate', detail: { prevPath: config.route } });
  await flush();
  assert.equal(fixture.commands.filter(command => command.kind === 'open').length, 3);
  assert.equal(checkboxes.every(checkbox => checkbox.checked && !checkbox.disabled), true);
  const applied = await fixture.launch();
  assert.equal(fixture.commands.filter(command => command.kind === 'action').at(-1).values.selected, '["one","two"]');
  applied.querySelector('[data-gfw-choice="apply"]').click();
  await flush();
  undo.value = undo.defaultValue = 'fresh-Go-undo-token';
  fixture.window.dispatchEvent({ type: 'gofastr:navigate', detail: { prevPath: config.route } });
  await flush();
  await fixture.launch();
  const latest = fixture.commands.filter(command => command.kind === 'action').at(-1);
  assert.equal(latest.values.undo, 'fresh-Go-undo-token');
  assert.equal(fixture.commands.filter(command => command.kind === 'respond').length, 2);
  assert.equal(fixture.commands.filter(command => command.kind === 'open').length, 4);
});

test('late retired-instance open cannot overwrite a newer retained-root instance', async () => {
  const fixture = await connectedHarness(async () => ({}));
  const transport = fixture.window.__gofastr_transport;
  const closed = [];
  let release;
  let opens = 0;
  fixture.window.__gofastr_transport = async (path, options) => {
    const command = JSON.parse(options.body);
    if (command.kind === 'close') closed.push(command.instance);
    if (command.kind !== 'open') return transport(path, options);
    opens++;
    if (opens === 1) return new Promise(resolve => { release = resolve; });
    return { ok: true, json: async () => ({ version: 1, instance: 'newest-instance' }) };
  };
  fixture.window.dispatchEvent({ type: 'gofastr:navigate', detail: { prevPath: '/items/42' } });
  await flush();
  assert.equal(fixture.root.dataset.gfwInstance, undefined);
  fixture.window.dispatchEvent({ type: 'gofastr:navigate', detail: { prevPath: '/items/42' } });
  await flush();
  assert.equal(fixture.root.dataset.gfwInstance, 'newest-instance');
  release({ ok: true, json: async () => ({ version: 1, instance: 'retired-instance', editor: { restored: true, values: { title: 'Old draft' } } }) });
  await flush();
  assert.equal(fixture.root.dataset.gfwInstance, 'newest-instance');
  assert.equal(fixture.field.value, 'Saved title');
  assert.equal(closed.includes('retired-instance'), true);
});
