const { readFileSync } = require('node:fs');
const { randomUUID } = require('node:crypto');
const { join } = require('node:path');
const { runInNewContext } = require('node:vm');

class Target {
  listeners = new Map();
  addEventListener(type, listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type).add(listener);
  }
  removeEventListener(type, listener) { this.listeners.get(type)?.delete(listener); }
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
  checked = false;
  textContent = '';
  pauses = 0;
  constructor(document, tag) { super(); this.document = document; this.tagName = tag.toUpperCase(); }
  get isConnected() { return this === this.document.body || !!this.parent?.isConnected; }
  get parentElement() { return this.parent; }
  get id() { return this.getAttribute('id'); }
  get type() { return this.getAttribute('type') || ''; }
  get value() { return this.currentValue ?? this.getAttribute('value') ?? ''; }
  set value(value) { this.currentValue = value; }
  set innerHTML(_) { throw new Error('Untrusted HTML is forbidden'); }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    if (name.startsWith('data-')) this.dataset[name.slice(5).replace(/-([a-z])/g, (_, letter) => letter.toUpperCase())] = String(value);
    if (name === 'src') this.src = String(value);
  }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  hasAttribute(name) { return this.attributes.has(name); }
  removeAttribute(name) { this.attributes.delete(name); if (name === 'src') delete this.src; }
  append(...children) {
    for (const child of children) { child.remove(); child.parent = this; this.children.push(child); }
  }
  replaceChildren(...children) { for (const child of [...this.children]) child.remove(); this.append(...children); }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter(child => child !== this); this.parent = null; }
  contains(node) { return node === this || this.children.some(child => child.contains(node)); }
  matches(selector) {
    return selector.split(',').some(part => {
      const match = part.trim().match(/^([a-z][a-z0-9]*)?(?:\[([^=\]]+)(?:="([^"]*)")?\])?$/);
      return !!match && (!match[1] || this.tagName === match[1].toUpperCase()) &&
        (!match[2] || (this.hasAttribute(match[2]) && (match[3] === undefined || this.getAttribute(match[2]) === match[3])));
    });
  }
  closest(selector) { return this.matches(selector) ? this : this.parent?.closest(selector) || null; }
  querySelectorAll(selector) { return this.children.flatMap(child => [...(child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]); }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  showModal() { this.open = true; }
  close() { this.open = false; }
  focus() { this.document.activeElement = this; this.document.dispatchEvent({ type: 'focusin', target: this }); }
  click() {
    if (this.disabled) return;
    const event = { type: 'click', target: this, button: 0 };
    this.document.dispatchEvent(event);
    if (!event.stopped) this.dispatchEvent(event);
    if (this.tagName === 'A' && this.hasAttribute('download')) this.document.downloads.push({ name: this.getAttribute('download'), url: this.getAttribute('href') });
  }
  pause() { this.pauses++; }
  load() {}
  getBoundingClientRect() { return { width: 300, left: 0, right: 300, top: 0, bottom: 300 }; }
}
class Form extends Element {}
const flush = async () => { for (let turn = 0; turn < 8; turn++) await new Promise(resolve => setImmediate(resolve)); };
const plain = value => JSON.parse(JSON.stringify(value));
const harness = (options = {}) => {
  const document = new Target();
  document.createElement = tag => new (tag === 'form' ? Form : Element)(document, tag);
  document.createElementNS = (_, tag) => document.createElement(tag);
  document.body = document.createElement('body');
  document.documentElement = document.body;
  document.activeElement = document.body;
  document.hidden = false;
  document.downloads = [];
  document.querySelectorAll = selector => document.body.querySelectorAll(selector);
  document.querySelector = selector => document.body.querySelector(selector);
  const window = new Target();
  const timers = new Map();
  const objects = new Map();
  const revoked = [];
  const observers = [];
  let sequence = 0;
  class LocalURL extends URL {}
  LocalURL.createObjectURL = blob => { const url = 'blob:test/' + ++sequence; objects.set(url, blob); return url; };
  LocalURL.revokeObjectURL = url => { revoked.push(url); objects.delete(url); };
  window.isSecureContext = true;
  window.__gofastr = { loadedModules: {}, invalidate() {}, navigate: (...args) => navigations.push(args), _pushURL: (...args) => urls.push(args) };
  window.scrollY = 345;
  window.scrollTo = point => scrolls.push(point);
  const navigations = [], urls = [], scrolls = [];
  const context = { window, document, navigator: {}, HTMLFormElement: Form, Blob, File, TextEncoder, Uint8Array, URL: LocalURL, URLSearchParams,
    AbortController, atob, btoa, crypto: { randomUUID }, Date,
    history: { state: { gfwAnnotated: true }, replaceState() {}, back() {} },
    location: { href: 'https://app.test/items', protocol: 'https:', host: 'app.test' },
    MutationObserver: class { constructor(callback) { observers.push(callback); } observe() {} },
    setTimeout(callback, delay) { const key = {}; timers.set(key, { callback, delay }); return key; }, clearTimeout(key) { timers.delete(key); },
    CustomEvent: class { constructor(type, details) { this.type = type; Object.assign(this, details); } },
    localStorage: { getItem() { return null; }, setItem() {} }, sessionStorage: { getItem() { return null; }, setItem() {}, removeItem() {} } };
  const load = name => runInNewContext(readFileSync(join(__dirname, name), 'utf8'), context, { filename: name });
  const root = document.createElement('div'); root.setAttribute('data-gfw-screen', '');
  const screen = document.createElement('section'); screen.setAttribute('data-gfm-platform', 'ios');
  const opener = document.createElement('button'); opener.setAttribute('form', 'gfw-form'); opener.value = 'show';
  screen.append(opener); root.append(screen); document.body.append(root); opener.focus();
  const fixture = { context, window, document, root, screen, opener, timers, objects, revoked, load, navigations, urls, scrolls,
    mutate: () => observers.forEach(callback => callback()),
    tick(delay) { const entry = [...timers].find(([, timer]) => delay == null || timer.delay === delay); if (!entry) return false; timers.delete(entry[0]); entry[1].callback(); return true; } };
  options.setup?.(fixture);
  for (const name of ['foreground.js', 'services.js', 'presentation.js', 'files.js', 'components.js', 'live.js']) load(name);
  fixture.adapter = window.__gofastr_services;
  fixture.open = (request = { id: 'go-key', clipboard: { text: 'Private <text>' } }, respond = async () => {}, serviceId = 'opaque-service') => {
    fixture.adapter.open({ root, request, respond, serviceId, opener });
    return document.querySelector('[data-gfw-service]');
  };
  return fixture;
};
const connected = async (handle, options = {}) => {
  const fixture = harness(options);
  const { root, document, opener, context, window } = fixture;
  const form = document.createElement('form'); form.setAttribute('data-gfw-form', '');
  const field = document.createElement('input'); field.setAttribute('data-gfw-field', 'title'); field.setAttribute('type', 'text');
  field.value = field.defaultValue = 'Saved title';
  if (!options.list) form.append(field);
  const status = document.createElement('p'); status.setAttribute('data-gfw-status', '');
  root.append(form, status);
  const config = { root: '/items', route: '/items', routes: [{ path: '/items', actions: ['show'] }], ...options.config };
  if (options.list) {
    config.list = options.list;
    const list = document.createElement('div'); list.setAttribute('data-gfw-list', '');
    const body = document.createElement('div'); body.setAttribute('data-gfw-collection-content', ''); list.append(body); root.append(list);
  }
  root.dataset.gfwConfig = JSON.stringify(config);
  const commands = [], signals = [];
  let opens = 0;
  window.__gofastr_transport = async (_, transport) => {
    const command = JSON.parse(transport.body); commands.push(command); signals.push(transport.signal);
    let reply;
    if (command.kind === 'hello') reply = { connection: 'connection', contract: { capabilities: command.hello.capabilities } };
    else if (command.kind === 'open' && ++opens === 1) reply = { instance: 'instance-1', screen: { ...(options.list ? { list: options.list } : {}), ...options.openScreen }, ...options.openReply };
    else reply = await handle(command, fixture, transport.signal);
    return { ok: true, json: async () => ({ version: 1, revision: command.revision || 0, ...reply }) };
  };
  fixture.field = field; fixture.form = form; fixture.status = status; fixture.commands = commands; fixture.signals = signals;
  options.beforeRuntime?.(fixture);
  fixture.load('webview.js');
  await flush();
  fixture.launch = async () => { document.dispatchEvent({ type: 'submit', target: form, submitter: opener }); await flush(); return document.querySelector('[data-gfw-service]'); };
  fixture.depart = () => { root.remove(); fixture.mutate(); window.dispatchEvent({ type: 'gofastr:navigate' }); };
  return fixture;
};
module.exports = { Target, Element, harness, connected, flush, plain };
