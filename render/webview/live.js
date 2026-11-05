(() => {
  'use strict';
  if (window.__gofastr_live) return;
  const element = (tag, text, attributes = {}) => {
    const node = document.createElement(tag);
    if (text) node.textContent = text;
    for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
    return node;
  };
  const rows = screen => {
    const list = screen.list;
    const body = element('div', '', { 'data-gfw-collection-content': '' });
    if (!list.rows?.length) {
      const empty = element('div', '', { class: 'gfm-empty', role: 'status' });
      empty.append(element('h2'), element('p', list.empty || 'No items.'));
      body.append(empty);
    }
    else {
      const section = element('section', '', { class: 'gfm-list-section', 'aria-label': list.header || 'Items' });
      section.append(element('h2', list.header || '', { class: 'gfm-section-heading' }));
      const items = element('ul', '', { class: 'gfm-list' });
      for (const row of list.rows) {
        const item = element('li', '', { class: 'gfw-row', 'data-gfw-row': row.id });
        if (row.disabled) item.dataset.gfwDisabled = 'true';
        const content = element('div', '', { class: 'gfw-row-content' });
        if (list.selection) {
          const input = element('input', '', { type: 'checkbox', 'data-gfw-select': row.id, 'aria-label': 'Select ' + row.title,
            form: 'gfw-form', name: '_gfw_selection', value: row.id });
          input.disabled = !!row.disabled;
          content.append(input);
        }
        const link = element(row.disabled || !row.destination ? 'div' : 'a', '', { class: 'gfm-row' });
        if (!row.disabled && row.destination) link.setAttribute('href', row.destination);
        if (row.disabled) link.setAttribute('aria-disabled', 'true');
        const copy = element('div', '', { class: 'gfm-row-copy' });
        copy.append(element('h2', row.title), element('p', row.subtitle));
        if (row.metadata) copy.append(element('span', row.metadata, { class: 'gfw-row-metadata' }));
        if (row.icon) link.append(window.__gofastr_components.fileIcon(document, row.icon));
        link.append(copy); content.append(link);
        const actions = [...(row.leadingActions || []), ...(row.trailingActions || [])];
        if (actions.length) {
          const menu = element('details', '', { 'data-gfw-row-menu': row.id, class: 'gfw-row-menu' });
          menu.append(element('summary', '•••', { 'aria-label': 'Actions for ' + row.title }));
          const buttons = element('div');
          for (const side of ['leading', 'trailing']) for (const action of row[side + 'Actions'] || []) {
            const query = new URLSearchParams({ route: screen.route, action: action.id, row: row.id });
            const control = element('button', action.label, { type: 'submit', form: 'gfw-form', name: 'action', value: action.id,
              'data-gfw-row-action': action.id, 'data-gfw-row-id': row.id, 'data-gfw-side': side, 'data-gfw-role': action.role || '',
              formaction: '/_mobile/actions?' + query });
            if (action.fullSwipe && action.role !== 'destructive') control.dataset.gfwFullSwipe = 'true';
            control.disabled = !!row.disabled;
            buttons.append(control);
          }
          menu.append(buttons); content.append(menu);
        }
        item.append(content); items.append(item);
      }
      section.append(items); body.append(section);
    }
    if (list.paging?.next) {
      const destination = new URL(screen.route, location.href);
      destination.searchParams.set(list.paging.key, list.paging.next);
      body.append(element('a', list.paging.label || 'Load more', { class: 'gfw-load-more', 'data-gfw-load-more': '', href: destination.pathname + destination.search }));
    }
    if (list.footer) body.append(element('p', list.footer, { class: 'gfm-footer' }));
    return body;
  };
  const create = ({ root, version, ready, allowed, identity, watch, refresh }) => {
    let generation = 0;
    let controller;
    let timer;
    let retired = false;
    let cancelled = false;
    let failure = false;
    let loading = false;
    let token = version || '';
    const panel = root.querySelector('[data-gfw-live-controls]') || element('div', '', { 'data-gfw-live-controls': '', role: 'group', 'aria-label': 'Collection loading' });
    let message = panel.querySelector('[data-gfw-live-status]');
    let retry = panel.querySelector('[data-gfw-live-retry]');
    let cancel = panel.querySelector('[data-gfw-live-cancel]');
    if (!message) {
      message = element('p', '', { 'data-gfw-live-status': '', role: 'status', 'aria-live': 'polite' });
      retry = element('button', 'Retry read', { type: 'button', 'data-gfw-live-retry': '' });
      cancel = element('button', 'Cancel read', { type: 'button', 'data-gfw-live-cancel': '' });
      panel.append(message, retry, cancel);
      root.querySelector('[data-gfw-list]')?.append(panel);
    }
    const show = (state, text = '') => {
      root.dataset.gfwLiveState = state;
      message.textContent = text;
      message.setAttribute('aria-live', state === 'error' ? 'assertive' : 'polite');
      retry.hidden = !['error', 'cancelled'].includes(state);
      cancel.hidden = !['watching', 'loading'].includes(state);
      cancel.textContent = state === 'watching' ? 'Pause' : 'Cancel';
      panel.hidden = state === 'idle';
      root.querySelector('[data-gfw-list]')?.setAttribute('aria-busy', String(state === 'loading'));
    };
    const abort = () => {
      generation++;
      clearTimeout(timer);
      controller?.abort();
      controller = null;
    };
    const visible = () => {
      if (retired || document.hidden || !root.isConnected || root.hidden || root.closest('[hidden], [inert], [aria-hidden="true"]') || !allowed()) return false;
      if (typeof root.getClientRects === 'function' && !root.getClientRects().length) return false;
      const style = window.getComputedStyle?.(root);
      return !style || style.display !== 'none' && !['hidden', 'collapse'].includes(style.visibility);
    };
    const schedule = (delay = 50) => {
      clearTimeout(timer);
      if (retired || cancelled || failure || document.hidden || !root.isConnected || !token) return;
      timer = setTimeout(() => void poll(), delay);
    };
    const read = async (options = {}) => {
      if (!visible()) return false;
      abort();
      cancelled = failure = false;
      loading = true;
      const epoch = generation;
      const before = identity();
      const active = controller = new AbortController();
      show('loading', 'Loading the current collection…');
      try {
        const next = await refresh(active.signal, options);
        if (retired || epoch !== generation || active.signal.aborted || !visible() || before !== identity()) {
          next?.dispose?.();
          return false;
        }
        if (typeof next === 'function') token = next() || token;
        else token = next || token;
        show('idle');
        return true;
      } catch (error) {
        if (epoch === generation && !retired && !active.signal.aborted) {
          failure = true;
          show('error', error.message || 'Collection read failed. Retry reads only; no action will be repeated.');
        }
        return false;
      } finally {
        if (epoch === generation) {
          loading = false; controller = null;
          if (!failure && !cancelled && !retired && root.dataset.gfwLiveState === 'loading') show('idle');
          schedule();
        }
      }
    };
    const poll = async () => {
      if (!visible()) { schedule(250); return; }
      const epoch = generation;
      const before = identity();
      const active = controller = new AbortController();
      const timeout = setTimeout(() => active.abort(), 11000);
      show('watching', 'Live updates');
      try {
        const reply = await watch(token, active.signal);
        if (retired || epoch !== generation || active.signal.aborted || !visible() || before !== identity()) return;
        if (reply.error) throw new Error(reply.error.message);
        if (reply.changed) { clearTimeout(timeout); controller = null; await read({ live: true }); }
        else { token = reply.dataVersion || token; show('idle'); }
      } catch (error) {
        if (!retired && epoch === generation && visible()) {
          failure = true;
          show('error', error.message || 'Live updates are unavailable. Retry the read; no action will be repeated.');
        }
      } finally {
        clearTimeout(timeout);
        if (epoch === generation) { controller = null; schedule(); }
      }
    };
    const cancelRead = () => {
      abort(); loading = false; cancelled = true;
      show('cancelled', 'Collection read cancelled. The current items are unchanged.');
    };
    const visibility = () => {
      abort(); loading = false;
      if (!cancelled && !failure) { show('idle'); if (!document.hidden) schedule(); }
    };
    const retryRead = () => void read({ retry: true });
    retry.addEventListener('click', retryRead);
    cancel.addEventListener('click', cancelRead);
    document.addEventListener('visibilitychange', visibility);
    const observer = new MutationObserver(() => {
      if (retired) return;
      if (!visible()) {
        if (controller) { abort(); loading = false; if (!cancelled && !failure) show('idle'); }
        schedule(250);
      } else if (!controller) schedule();
    });
    observer.observe(document.documentElement, { attributes: true, subtree: true, attributeFilter: ['hidden', 'inert', 'aria-hidden'] });
    show('idle');
    void Promise.resolve(ready).then(() => schedule()).catch(() => {});
    return {
      refresh: read,
      suspend() { abort(); loading = false; if (!cancelled && !failure) show('idle'); },
      resume: () => schedule(),
      close() {
        retired = true; abort();
        observer.disconnect?.();
        retry.removeEventListener?.('click', retryRead); cancel.removeEventListener?.('click', cancelRead);
        document.removeEventListener?.('visibilitychange', visibility);
      },
      get loading() { return loading; },
    };
  };
  window.__gofastr_live = { create, rows };
  if (window.__gofastr) (window.__gofastr.loadedModules ||= {})['gfm-webview-live'] = true;
})();
