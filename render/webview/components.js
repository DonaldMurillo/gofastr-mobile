(() => {
  'use strict';
  const fileIcon = (document, kind) => {
    const paths = {
      file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6Zm0 0v6h6M8 13h8M8 17h6',
      photo: 'M4 3h16v18H4zM4 16l5-5 4 4 3-3 4 4M16 7h.01',
      audio: 'M3 10v4m4-8v12m5-16v20m5-16v12m4-8v4',
    };
    const icon = document.createElement('span');
    icon.className = 'gfw-file-icon';
    icon.dataset.gfwFileIcon = Object.hasOwn(paths, kind) ? kind : 'file';
    icon.setAttribute('aria-hidden', 'true');
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    for (const [name, value] of Object.entries({ viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.6', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' })) svg.setAttribute(name, value);
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', paths[icon.dataset.gfwFileIcon]);
    svg.append(path);
    icon.append(svg);
    return icon;
  };
  const fieldValue = (field, defaults = false) => {
    if (field.type === 'checkbox') {
      if (defaults) return field.dataset.gfwInitial ?? String(!!field.defaultChecked);
      return field.checked ? 'true' : field.dataset.gfwOptionalEmpty === 'true' ? '' : 'false';
    }
    if (defaults && field.tagName === 'SELECT') {
      return Array.from(field.options).find(option => option.defaultSelected)?.value ?? field.options[0]?.value ?? '';
    }
    return defaults ? field.defaultValue : field.value;
  };
  const setField = (field, value) => {
    if (field.disabled) return;
    if (field.type === 'checkbox') {
      field.checked = value === 'true';
      field.dataset.gfwOptionalEmpty = String(value === '');
    } else if (field.value !== value) field.value = value;
  };
  const serialize = (fields, marker = 'gfwField', visibleOnly = false, defaults = false) => Object.fromEntries(
    Array.from(fields).filter(field => !visibleOnly || field.type !== 'hidden')
      .map(field => [field.dataset[marker], fieldValue(field, defaults)]));

  const localRoute = (config, destination, base) => {
    if (typeof destination !== 'string' || destination.length > 4096 || !destination.startsWith('/') ||
        destination.startsWith('//') || /[\\#\u0000-\u0020\u007f]/.test(destination)) return null;
    const path = destination.split('?')[0];
    let segments;
    try { segments = path.split('/').map(segment => decodeURIComponent(segment)); } catch (_) { return null; }
    if (segments.some(segment => segment === '.' || segment === '..' || /[\\/\u0000-\u0020\u007f]/.test(segment)) || path.includes('//')) return null;
    const parsed = new URL(destination, base);
    const origin = new URL(base);
    if (parsed.origin !== origin.origin) return null;
    const matched = path === '/' || config.routes.some(route => {
      const expected = route.path.split('/');
      return expected.length === segments.length && expected.every((segment, index) =>
        segment.startsWith('{') && segment.endsWith('}') ? segments[index] !== '' : segment === segments[index]);
    });
    return matched ? parsed.pathname + parsed.search : null;
  };

  const reconcile = (selected, rows, maximum = 0) => {
    const allowed = new Set((Array.isArray(rows) ? rows : []).filter(row => !row.disabled).map(row => row.id));
    const identifiers = Array.from(new Set((Array.isArray(selected) ? selected : []).filter(identifier => typeof identifier === 'string' && allowed.has(identifier))));
    return maximum > 0 ? identifiers.slice(0, maximum) : identifiers;
  };
  const queryRoute = (config, current, base, reset = true) => {
    const destination = new URL(config.route, base);
    const list = config.list;
    if (!list) return destination.pathname + destination.search;
    for (const [key, value] of Object.entries(current)) destination.searchParams.set(key, value);
    if (reset && list.paging) destination.searchParams.delete(list.paging.key);
    if (list.selection) destination.searchParams.delete(list.selection.key);
    return destination.pathname + destination.search;
  };
  const listValues = (config, original, filters, selected) => {
    const result = { ...original, ...filters };
    if (config.list?.selection) {
      const selection = config.list.selection;
      result[selection.key] = JSON.stringify(reconcile(selected, config.list.rows, selection.maximum));
    }
    return result;
  };
  const collectionIdentity = (config, base) => {
    if (!config.list) return null;
    const destination = new URL(config.route, base);
    if (config.list.paging) destination.searchParams.delete(config.list.paging.key);
    if (config.list.selection) destination.searchParams.delete(config.list.selection.key);
    destination.searchParams.sort();
    return { key: config.root + ':' + destination.pathname, query: destination.search };
  };

  const tabSignature = config => JSON.stringify([config.root, config.routes.map(route => route.path),
    (config.tabs || []).map(tab => [tab.id, tab.route])]);
  const emptyMetadata = config => ({ version: 1, signature: tabSignature(config), active: '', contexts: Object.create(null) });
  const restoreMetadata = (config, raw, base) => {
    const empty = emptyMetadata(config);
    if (typeof raw !== 'string' || raw.length > 32768 || !config.tabs?.length || config.tabs.length > 16) return empty;
    let saved;
    try { saved = JSON.parse(raw); } catch (_) { return empty; }
    if (saved?.version !== 1 || saved.signature !== empty.signature || typeof saved.contexts !== 'object' || !saved.contexts) return empty;
    const restored = empty;
    restored.active = config.tabs.some(tab => tab.id === saved.active) ? saved.active : '';
    for (const tab of config.tabs) {
      if (!localRoute(config, tab.route, base)) continue;
      const entry = Object.hasOwn(saved.contexts, tab.id) && saved.contexts[tab.id];
      const route = entry && localRoute(config, entry.route, base);
      if (!route) continue;
      restored.contexts[tab.id] = { route, scroll: Number.isFinite(entry.scroll) ? Math.max(0, Math.min(entry.scroll, 10000000)) : 0,
        focus: typeof entry.focus === 'string' && entry.focus.length <= 256 ? entry.focus : '' };
    }
    return restored;
  };
  const metadataKey = config => {
    let hash = 2166136261;
    for (const character of tabSignature(config)) hash = Math.imul(hash ^ character.charCodeAt(0), 16777619) >>> 0;
    return 'gofastr:webview:tabs:v1:' + hash.toString(16);
  };

  const gestureStart = ({ x, y, width, pointerId, primary = true, button = 0, blocked = false }) => {
    if (!primary || button !== 0 || blocked || x < 28 || width < 80) return null;
    return { x, y, width, pointerId, phase: 'tracking', dx: 0, side: '' };
  };
  const gestureMove = (gesture, { x, y, pointerId }, sides) => {
    if (!gesture || gesture.pointerId !== pointerId || gesture.phase === 'cancelled') return gesture;
    const dx = x - gesture.x;
    const vertical = Math.abs(y - gesture.y);
    const horizontal = Math.abs(dx);
    if (gesture.phase === 'horizontal' && vertical > Math.max(24, horizontal * 1.2)) return { ...gesture, phase: 'cancelled', dx: 0 };
    if (gesture.phase === 'tracking' && vertical > 10 && vertical >= horizontal / 1.3) return { ...gesture, phase: 'cancelled', dx: 0 };
    if (gesture.phase === 'tracking' && (horizontal < 12 || horizontal <= vertical * 1.3)) return gesture;
    const side = dx > 0 ? 'leading' : 'trailing';
    if (!sides[side]?.length) return { ...gesture, phase: 'cancelled', dx: 0 };
    return { ...gesture, phase: 'horizontal', side, dx: Math.sign(dx) * Math.min(horizontal, gesture.width * 0.9) };
  };
  const gestureEnd = (gesture, actions, cancelled = false) => {
    if (!gesture || cancelled || gesture.phase !== 'horizontal' || Math.abs(gesture.dx) < 56) return { kind: 'cancel' };
    const side = actions[gesture.side] || [];
    const action = side[0];
    if (action?.fullSwipe && action.role !== 'destructive' && Math.abs(gesture.dx) >= Math.max(168, gesture.width * 0.72)) {
      return { kind: 'action', id: action.id, side: gesture.side };
    }
    return side.length ? { kind: 'menu', side: gesture.side } : { kind: 'cancel' };
  };

  const api = { fileIcon, fieldValue, setField, serialize, localRoute, reconcile, queryRoute, listValues, collectionIdentity, tabSignature,
    emptyMetadata, restoreMetadata, metadataKey, gestureStart, gestureMove, gestureEnd };
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    window.__gofastr_components = api;
    if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-components'] = true;
  }
})();
