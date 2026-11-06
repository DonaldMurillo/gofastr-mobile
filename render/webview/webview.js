(() => {
  'use strict';
  const runtime = window.__gofastr;
  const presentations = window.__gofastr_presentations;
  const files = window.__gofastr_files;
  const services = window.__gofastr_services;
  const live = window.__gofastr_live;
  const components = window.__gofastr_components;
  const rendering = window.__gofastr_rendering;
  const modalOpen = () => presentations.has() || !!files?.has() || !!services?.has();
  if (document.__gfwBound) return;
  document.__gfwBound = true;
  const initialized = new WeakMap();
  const activeScreens = new Set();
  const current = state => initialized.get(state.root) === state && !state.retired;
  const observedEffects = new Set();
  const collectionSelections = new Map();
  let connection;
  let pendingNavigation;
  let pendingAnnouncement = '';
  let tabsMetadata;
  let tabsScope = '';
  let pendingTab;
  let restoredTabs = false;
  if (!history.state?.gfwAnnotated) {
    try { history.replaceState({ ...history.state, gfwAnnotated: true }, ''); } catch (_) {}
  }

  const values = (form, visibleOnly = false, defaults = false) => components.serialize(
    form.querySelectorAll('[data-gfw-field]'), 'gfwField', visibleOnly, defaults);
  const queryValues = state => ({
    ...components.serialize(state.root.querySelectorAll('[data-gfw-filter]'), 'gfwFilter'),
    ...components.serialize(state.root.querySelectorAll('[data-gfw-query]'), 'gfwQuery'),
  });
  const currentValues = state => {
    const current = components.listValues(state.config, values(state.form), queryValues(state), state.selected);
    if (state.actionRow) current.row = state.actionRow;
    return current;
  };
  const equal = (first, second) => Object.keys(first).length === Object.keys(second).length &&
    Object.keys(first).every(key => Object.hasOwn(second, key) && first[key] === second[key]);
  const status = (root, message, error = false) => {
    const live = root.querySelector('[data-gfw-status]');
    if (!live) return;
    live.textContent = message;
    live.setAttribute('aria-live', error ? 'assertive' : 'polite');
    root.querySelector('[data-gfw-form]').dataset.gfwError = String(error);
  };
  const storageKey = (state) => state.config.draft ? 'gofastr:webview:draft:' + state.config.draft.key : '';
  const dirty = (state) => !equal(values(state.form, true), state.baseline);
  const formResult = (state, ok) => state.form.dispatchEvent(new CustomEvent('gofastr:formresult', { bubbles: true, detail: { ok } }));
  const removeDraft = (state) => {
    if (!storageKey(state)) return true;
    try {
      sessionStorage.removeItem(storageKey(state));
      return true;
    } catch (_) {
      return false;
    }
  };

  const remember = (root, state) => {
    const changed = dirty(state);
    if (!changed) {
      const removed = removeDraft(state);
      formResult(state, true);
      status(root, removed ? state.initialStatus : 'Fields restored, but the old local draft could not be removed.');
      return;
    }
    if (!storageKey(state)) {
      status(root, 'Unsaved changes · save before leaving this screen');
      return;
    }
    try {
      sessionStorage.setItem(storageKey(state), JSON.stringify({ values: values(state.form, true) }));
      status(root, state.config.draft.changed || 'Unsaved changes · draft kept for this session');
      state.form.dataset.huiLeaveGuardMessage = state.config.draft.message + ' Leave this screen and keep the local draft?';
    } catch (_) {
      status(root, 'Draft storage is unavailable. Save before leaving this screen.', true);
      state.form.dataset.huiLeaveGuardMessage = 'The draft could not be stored. Your changes will be lost if you leave. Leave this screen?';
    }
  };

  const local = (config, destination) => components.localRoute(config, destination, location.href);

  const navigate = (root, state, destination, options = {}) => {
    if (!current(state)) return;
    if (modalOpen()) {
      status(root, 'Finish or cancel the open modal task before leaving.', true);
      return;
    }
    const path = local(state.config, destination);
    if (!path || typeof runtime?.navigate !== 'function') {
      status(root, 'Navigation is unavailable. Your text is still here.', true);
      return;
    }
    const proceed = () => {
      if (!root.isConnected || !current(state)) return;
      state.live?.suspend();
      services?.releasePlayback(root);
      saveTab(state);
      runtime.navigate(path, options);
    };
    if (!dirty(state) || !runtime._leaveGuard || runtime._leaveGuard.ok(document, proceed)) proceed();
  };

  const back = (root, state, fallback) => {
    const previous = history.state?.gfwPrevious;
    if (previous === '/' && state.config.root !== '/') {
      navigate(root, state, state.config.root, { replace: true, force: true });
      return;
    }
    if (previous && local(state.config, previous)) {
      const revision = state.revision;
      if (historyBarrier) historyBarrier.traverse(-1, () => current(state) && root.isConnected && state.revision === revision &&
        !state.uncertain && !state.composing.size && !state.editor?.pending && !state.editor?.unknown && !modalOpen());
      else history.back();
      return;
    }
    status(root, 'No earlier app screen is available. Returning to the start.');
    navigate(root, state, local(state.config, fallback) || state.config.root, { replace: true, force: true });
  };

  const confirmLeave = async state => {
    if (typeof runtime?.loadModule === 'function' && document.querySelector('template[data-cui-confirm-dialog]')) {
      await runtime.loadModule('confirm');
      if (typeof runtime.askWords !== 'function') throw new Error('Draft confirmation is unavailable. Stay here and try again.');
      return runtime.askWords({ message: state.form.dataset.huiLeaveGuardMessage || state.config.draft?.message || 'Keep this draft and leave?',
        title: state.form.dataset.huiLeaveGuardTitle, accept: state.form.dataset.huiLeaveGuardAccept, danger: true });
    }
    return typeof window.confirm === 'function' && window.confirm(state.config.draft?.message || 'Keep this draft and leave?');
  };

  const leaveOnce = async (root, state) => {
    if (modalOpen()) {
      status(root, 'Finish or cancel the open modal task before leaving.', true);
      return false;
    }
    if (state.busy) {
      status(root, 'Wait for the pending action. You can keep editing.', true);
      return false;
    }
    state.live?.suspend();
    try { await state.ready; }
    catch (error) { if (current(state)) status(root, error.message || 'This screen is not connected. Stay here and try again.', true); return false; }
    if (!current(state) || !root.isConnected) return false;
    if (state.composing.size || state.uncertain || state.editor?.unknown || state.editor?.pending) {
      status(root, 'Finish composing, or resolve the pending or uncertain action before leaving.', true);
      return false;
    }
    const revision = state.revision;
    let reply;
    try { reply = await command(root, state, 'leave'); }
    catch (error) { if (current(state)) status(root, error.message || 'Could not verify that it is safe to leave.', true); return false; }
    if (!current(state) || !root.isConnected) return false;
    observe(root, state, reply);
    if (reply.error) { status(root, reply.error.message, true); return false; }
    if (revision !== state.revision || state.composing.size || state.busy) {
      status(root, 'You kept editing. Your text is still here; finish the edit before leaving.');
      return false;
    }
    if (reply.editor?.canLeave !== false) { formResult(state, true); return true; }
    if (reply.editor.pending) { status(root, 'Wait for the pending save before leaving.', true); return false; }
    let confirmed;
    try { confirmed = await confirmLeave(state); }
    catch (error) { status(root, error.message, true); return false; }
    if (!confirmed || !current(state) || !root.isConnected || state.busy || state.composing.size) return false;
    const keptRevision = state.revision;
    let kept;
    try { kept = await command(root, state, 'leave', { choice: 'keep' }); }
    catch (error) { if (current(state)) status(root, error.message || 'The draft could not be checkpointed. Stay here and try again.', true); return false; }
    if (!current(state) || !root.isConnected) return false;
    observe(root, state, kept);
    if (kept.error || !kept.editor?.canLeave || keptRevision !== state.revision || state.composing.size || state.busy) {
      formResult(state, false);
      status(root, kept.error?.message || 'The draft could not be checkpointed. Stay here and save or discard it.', true);
      return false;
    }
    formResult(state, true);
    return true;
  };

  const leave = (root, state) => {
    if (state.leaving) return state.leaving;
    const release = holdCheckpoints(state);
    state.leaving = leaveOnce(root, state).finally(() => {
      state.leaving = undefined;
      release();
      if (current(state) && root.isConnected) state.live?.resume();
    });
    return state.leaving;
  };

  const capabilities = () => ({
    navigation: 'gofastr-history', sheet: 'full-page', presentation: 'modal-overlay', dismiss: 'history-or-root',
    tabs: 'last-route-scroll-focus-with-shared-history',
    share: typeof navigator.share === 'function' ? 'system-with-selection-fallback' : 'select-and-copy',
    haptic: typeof navigator.vibrate === 'function' ? 'vibration-or-announcement' : 'announcement-only',
    announcement: 'aria-live', draft: 'go-checkpoint-with-session-fallback', files: files ? 'user-activated-input-and-download' : 'unavailable',
    photos: files?.photoCapability?.() || 'unavailable', camera: files?.photoCapability?.() || 'unavailable',
  });

  const request = async (root, command, options = {}) => {
    const transport = window.__gofastr_transport || window.fetch.bind(window);
    const headers = { Accept: 'application/json', 'Content-Type': 'application/json', 'X-GoFastr-Mobile-Protocol': '1' };
    const csrf = root.querySelector('[name="_csrf"]');
    if (csrf) headers['X-CSRF-Token'] = csrf.value;
    const response = await transport('/_mobile/actions', { method: 'POST', headers, body: JSON.stringify({ version: 1, ...command }), ...options });
    const reply = await response.json();
    if (!response.ok || reply.version !== 1) throw new Error(reply.error?.message || 'The mobile renderer connection failed. No action was retried.');
    return reply;
  };

  const loadExtensions = async root => {
    const modules = JSON.parse(root.dataset.gfwConfig || '{}').extensions || [];
    for (const name of modules) {
      if (!/^gfn-extension-[a-f0-9]{32}$/.test(name) || typeof window.__gofastr?.loadModule !== 'function') throw new Error('A compiled extension could not be loaded.');
      await window.__gofastr.loadModule(name);
    }
  };
  const connect = root => connection ||= loadExtensions(root).then(async () => {
    const names = JSON.parse(root.dataset.gfwConfig || '{}').componentExtensions || [];
    if (names.length && !rendering) throw new Error('Required component rendering support is unavailable.');
    return { ...(await services?.capabilities() || { 'live/v1': 'available' }), ...(rendering?.capabilities(names) || {}) };
  }).then(serviceCapabilities => request(root, { kind: 'hello', hello: {
    minimum: 1, maximum: 1, renderer: 'webview', capabilities: {
      'navigation/v1': 'available', 'editor/v1': 'available', 'drafts/v1': 'available',
      'sheets/v1': 'fallback', 'share/v1': typeof navigator.share === 'function' ? 'available' : 'fallback',
      'haptics/v1': typeof navigator.vibrate === 'function' ? 'fallback' : 'unavailable', 'announcement/v1': 'available',
      'presentation/v1': 'available', 'files/v1': files?.capability || 'unavailable',
      'photos/v1': files?.photoCapability?.() || 'unavailable', 'camera/v1': files?.photoCapability?.() || 'unavailable',
      ...(files?.capabilities?.() || {}),
      ...serviceCapabilities,
    },
  } })).then(reply => {
    if (reply.error) throw new Error(reply.error.message);
    return reply;
  });

  const command = async (root, state, kind, extra = {}, options = {}) => {
    if (!['checkpoint', 'watch'].includes(kind)) state.checkpointEpoch = (state.checkpointEpoch || 0) + 1;
    const hello = await connect(root);
    return request(root, { kind, connection: hello.connection, instance: state.instance,
      revision: state.revision, values: currentValues(state), ...extra }, options);
  };

  const observe = (root, state, reply) => {
    if (reply.error?.code === 'unknown' || reply.editor?.unknown) state.uncertain = true;
    else if (reply.response) state.uncertain = false;
    if (reply.editor && reply.revision === state.revision) {
      state.editor = reply.editor;
      root.dataset.gfwEditor = JSON.stringify(reply.editor);
    }
  };

  const holdCheckpoints = state => {
    state.checkpointHolds = (state.checkpointHolds || 0) + 1;
    state.checkpointEpoch = (state.checkpointEpoch || 0) + 1;
    let released = false;
    return () => {
      if (released) return;
      released = true;
      if (--state.checkpointHolds === 0 && state.checkpointDeferred) {
        state.checkpointDeferred = false;
        void checkpoint(state.root, state, true).catch(error => {
          if (current(state) && state.root.isConnected) status(state.root, error.message || 'Draft checkpoint failed. Your text is still here.', true);
        });
      }
    };
  };

  const checkpoint = async (root, state, background = false) => {
    await state.ready;
    if (!root.isConnected || !current(state) || !state.instance || (!background && state.composing.size)) return;
    if (state.checkpointHolds) { state.checkpointDeferred = true; return; }
    const epoch = state.checkpointEpoch || 0;
    const sequence = state.checkpointSequence = (state.checkpointSequence || 0) + 1;
    const reply = await command(root, state, 'checkpoint');
    if (!root.isConnected || !current(state) || reply.revision !== state.revision ||
        epoch !== (state.checkpointEpoch || 0) || sequence !== state.checkpointSequence) return;
    observe(root, state, reply);
    if (reply.error) status(root, reply.error.message, true);
    else if (!state.busy && dirty(state)) {
      status(root, reply.editor?.draftStored ? state.config.draft?.changed || 'Unsaved changes · draft kept on this device' :
        'Unsaved changes · this session has a backup, but durable recovery is unavailable.');
    }
  };

  const start = async (root, state) => {
    const hello = await connect(root);
    const reply = await command(root, state, 'open', { route: state.config.route });
    if (reply.error) throw new Error(reply.error.message);
    if (!root.isConnected || !current(state)) {
      if (reply.instance) void command(root, { ...state, instance: reply.instance }, 'close', { values: {} }).catch(() => {});
      return;
    }
    state.instance = reply.instance;
    root.dataset.gfwInstance = reply.instance;
    root.dataset.gfwContract = JSON.stringify(hello.contract);
    root.dataset.gfwCapabilities = JSON.stringify({ ...capabilities(), ...hello.contract?.capabilities });
    showImages(root, state, reply.screen?.images || []);
    showAttachments(root, state, reply.screen?.attachments || []);
    showComponents(root, state, reply.screen?.components || []);
    state.hasEditor = !!reply.screen?.form;
    state.dataVersion = reply.screen?.live?.version || reply.dataVersion || state.config.live?.version || '';
    if (live && reply.screen?.list && !state.hasEditor) {
      const collection = root.querySelector('[data-gfw-collection-content]');
      if (collection) {
        const rendered = live.rows({ ...reply.screen, route: reply.screen.route || state.config.route });
        collection.replaceChildren(...Array.from(rendered.children));
        state.config.list = reply.screen.list;
        state.config.live = reply.screen.live;
        collectionValues(state, reply.screen);
        root.dataset.gfwConfig = JSON.stringify(state.config);
        state.source = root.dataset.gfwConfig;
        syncSelection(state);
      }
    }
    if (reply.editor) {
      const untouched = state.revision === 0 && state.composing.size === 0;
      state.baseline = reply.editor.baseline;
      if (untouched && reply.editor.restored) {
        for (const field of state.form.querySelectorAll('[data-gfw-field]')) {
          if (field.type !== 'hidden') components.setField(field, reply.editor.values[field.dataset.gfwField] ?? '');
        }
        if (dirty(state)) {
          remember(root, state);
          formResult(state, false);
          status(root, state.config.draft?.restored || 'Unsaved draft restored.');
        }
      }
      state.editor = reply.editor;
      root.dataset.gfwEditor = JSON.stringify(reply.editor);
      if (reply.editor.unknown) status(root, 'A previous save may have succeeded. Keep this draft and inspect the saved notes before trying again.', true);
    }
    if (live && state.config.list && !state.hasEditor && typeof AbortController === 'function') startCollection(root, state);
  };

  const startCollection = (root, state) => {
    const allowed = () => current(state) && root.isConnected && !state.busy && !modalOpen() && !state.uncertain && !state.composing.size && !state.hasEditor;
    const identity = () => JSON.stringify([state.instance, state.revision, state.config.route, queryValues(state)]);
    state.live = live.create({ root, version: state.dataVersion, allowed, identity,
      watch: (dataVersion, signal) => command(root, state, 'watch', { dataVersion, values: queryValues(state) }, { signal }),
      refresh: async (signal, options) => {
        const before = identity();
        const destination = options.route || components.queryRoute(state.config, queryValues(state), location.href, false);
        const hello = await connect(root);
        const reply = await request(root, { kind: 'open', connection: hello.connection, route: destination, values: {} }, { signal });
        const discard = () => {
          if (reply.instance) void request(root, { kind: 'close', connection: hello.connection, instance: reply.instance, values: {} }).catch(() => {});
        };
        if (!allowed() || signal.aborted || before !== identity()) { discard(); return ''; }
        if (reply.error || !reply.screen?.list || reply.screen.form) { discard(); throw new Error(reply.error?.message || 'The collection could not be read.'); }
        const commit = () => {
          if (!allowed() || signal.aborted || before !== identity()) { discard(); return ''; }
          const screen = reply.screen;
          const collection = root.querySelector('[data-gfw-collection-content]');
          if (!collection) { discard(); throw new Error('The collection display is unavailable.'); }
          const scroll = { x: window.scrollX || 0, y: window.scrollY || 0 };
          const active = document.activeElement;
          const focusedRow = active?.closest?.('[data-gfw-row]')?.dataset.gfwRow;
          const oldInstance = state.instance;
          resetSwipe();
          const rendered = live.rows(screen);
          collection.replaceChildren(...Array.from(rendered.children));
          state.instance = reply.instance;
          root.dataset.gfwInstance = reply.instance;
          state.config.list = screen.list;
          state.config.route = screen.route;
          state.config.live = screen.live;
          state.config.components = screen.components || [];
          state.dataVersion = screen.live?.version || reply.dataVersion || '';
          state.revision = reply.revision || 0;
          state.actionRow = '';
          collectionValues(state, screen);
          state.queryPending = '';
          root.dataset.gfwConfig = JSON.stringify(state.config);
          state.source = root.dataset.gfwConfig;
          syncSelection(state);
          showImages(root, state, screen.images || []);
          showAttachments(root, state, screen.attachments || []);
          showComponents(root, state, screen.components || []);
          if (options.route) {
            runtime?._pushURL?.(screen.route, { replace: options.replace !== false });
            const query = root.querySelector('[data-gfw-search]');
            if (query) query.setAttribute('action', screen.route.split('?')[0]);
          }
          if (focusedRow && !active.isConnected) {
            const row = Array.from(root.querySelectorAll('[data-gfw-row]')).find(item => item.dataset.gfwRow === focusedRow);
            (row?.querySelector(active.hasAttribute?.('data-gfw-select') ? '[data-gfw-select]' : 'a') || root).focus?.({ preventScroll: true });
          }
          window.scrollTo?.({ left: scroll.x, top: scroll.y, behavior: 'instant' });
          void request(root, { kind: 'close', connection: hello.connection, instance: oldInstance, values: {} }).catch(() => {});
          return state.dataVersion;
        };
        commit.dispose = discard;
        return commit;
      },
    });
  };

  const collectionValues = (state, screen) => {
    const present = new Set();
    for (const field of state.form.querySelectorAll('[data-gfw-field]')) {
      const key = field.dataset.gfwField;
      present.add(key);
      if (field.type !== 'hidden') continue;
      if (Object.hasOwn(screen.values || {}, key)) field.value = screen.values[key];
      else field.remove();
    }
    for (const [key, value] of Object.entries(screen.values || {})) {
      if (present.has(key)) continue;
      const field = document.createElement('input');
      field.setAttribute('type', 'hidden');
      field.setAttribute('name', key);
      field.setAttribute('data-gfw-field', key);
      field.value = value;
      state.form.append(field);
    }
    const toolbar = state.root.querySelector('[data-gfw-selection-count]')?.parentElement;
    if (toolbar) {
      const count = toolbar.querySelector('[data-gfw-selection-count]');
      const controls = count ? [count] : [];
      for (const action of screen.actions || []) {
        if (!action.commit || action.primary || action.leading) continue;
        const button = document.createElement('button');
        button.textContent = action.label;
        for (const [key, value] of Object.entries({ type: 'submit', form: 'gfw-form', name: 'action', value: action.id,
          formaction: '/_mobile/actions?' + new URLSearchParams({ route: screen.route || state.config.route, action: action.id }) })) button.setAttribute(key, value);
        controls.push(button);
      }
      toolbar.replaceChildren(...controls);
    }
  };

  const showComponents = (root, state, snapshots) => {
    state.config.components = snapshots;
    root.dataset.gfwConfig = JSON.stringify(state.config);
    state.source = root.dataset.gfwConfig;
    if (!rendering) {
      if (snapshots.length) throw new Error('Required component rendering support is unavailable.');
      return;
    }
    state.rendering ||= rendering.create(root);
    state.rendering.reconcile(snapshots, { names: state.config.componentExtensions || [], open: async (snapshot, key) => {
      if (!current(state)) return;
      const epoch = state.navigationEpoch = (state.navigationEpoch || 0) + 1;
      if (!(await leave(root, state)) || !current(state) || !root.isConnected || epoch !== state.navigationEpoch) return;
      const latest = state.config.components.find(item => item.id === snapshot.id && item.name === snapshot.name);
      const destination = latest && Object.hasOwn(latest.destinations || {}, key) && local(state.config, latest.destinations[key]);
      if (!destination) { status(root, 'This component destination is not an app screen.', true); return; }
      navigate(root, state, destination);
    } });
  };

  const showAttachments = (root, state, attachments) => {
    const section = Array.from(root.querySelectorAll('[data-gfw-attachments]')).find(item => !item.closest('[data-gfw-component]'));
    if (!section || !Array.isArray(attachments)) return;
    services?.releasePlayback(section);
    const heading = document.createElement('h2');
    heading.textContent = 'Attachments';
    const list = document.createElement('ul');
    for (const file of attachments) {
      const destination = file && local(state.config, file.destination);
      if (!destination || typeof file.name !== 'string' || typeof file.mediaType !== 'string' || !Number.isInteger(file.size) || file.size < 0) continue;
      const row = document.createElement('li');
      const link = document.createElement('a');
      link.href = destination;
      link.dataset.gfwAttachment = file.id;
      const name = document.createElement('strong');
      name.textContent = file.name;
      const detail = document.createElement('span');
      detail.textContent = typeof file.detail === 'string' ? file.detail : `File · ${file.size} B`;
      const icon = components.fileIcon(document, file.icon);
      const copy = document.createElement('div');
      copy.className = 'gfw-file-copy';
      copy.append(name, detail);
      link.append(icon, copy);
      row.append(link);
      services?.attachPlayback(row, file);
      list.append(row);
    }
    section.replaceChildren(heading, list);
    section.hidden = list.children.length === 0;
  };

  const showImages = (root, state, images) => {
    const gallery = Array.from(root.querySelectorAll('[data-gfw-images]')).find(item => !item.closest('[data-gfw-component]'));
    if (!gallery || !Array.isArray(images)) return;
    const children = [];
    const heading = document.createElement('h2');
    heading.textContent = 'Photos';
    children.push(heading);
    for (const preview of images) {
      if (!preview || typeof preview.data !== 'string' || preview.data.length > 175000 || !/^[A-Za-z0-9+/]+={0,2}$/.test(preview.data) ||
          !Number.isInteger(preview.width) || !Number.isInteger(preview.height) || preview.width < 1 || preview.height < 1 || preview.width > 1024 || preview.height > 1024) continue;
      const destination = preview.destination && local(state.config, preview.destination);
      const item = document.createElement(destination ? 'a' : 'figure');
      item.dataset.gfwImage = preview.id;
      if (destination) {
        item.href = destination;
        item.setAttribute('aria-label', 'Open photo: ' + preview.alt);
      }
      const image = document.createElement('img');
      image.alt = preview.alt;
      image.width = preview.width;
      image.height = preview.height;
      image.decoding = 'async';
      image.loading = 'lazy';
      image.src = 'data:image/jpeg;base64,' + preview.data;
      item.append(image);
      children.push(item);
    }
    gallery.replaceChildren(...children);
    gallery.hidden = children.length === 1;
  };

  const acknowledge = (root, state, requestId, name, identifier, outcome, message = '') => {
    if (!identifier || observedEffects.has(identifier)) return;
    observedEffects.add(identifier);
    const capability = { share: 'share/v1', haptic: 'haptics/v1', navigation: 'navigation/v1', announcement: 'announcement/v1' }[name];
    const receipt = { id: identifier, capability, status: outcome, message };
    state.receipts.push(receipt);
    root.dataset.gfwEffects = JSON.stringify(state.receipts);
    void command(root, state, 'ack', { requestId, receipt }).catch(() => {});
  };

  const syncSelection = state => {
    if (!current(state)) return;
    const selection = state.config.list?.selection;
    if (!selection) return;
    state.selected = components.reconcile(state.selected, state.config.list.rows, selection.maximum);
    const identity = components.collectionIdentity(state.config, location.href);
    collectionSelections.delete(identity.key);
    collectionSelections.set(identity.key, { query: identity.query, selected: state.selected });
    if (collectionSelections.size > 32) collectionSelections.delete(collectionSelections.keys().next().value);
    for (const checkbox of state.root.querySelectorAll('[data-gfw-select]')) {
      checkbox.checked = state.selected.includes(checkbox.dataset.gfwSelect);
      const row = state.config.list.rows.find(item => item.id === checkbox.dataset.gfwSelect);
      checkbox.disabled = state.busy || !!state.queryPending || state.uncertain || modalOpen() || !!row?.disabled || (!checkbox.checked && selection.maximum > 0 && state.selected.length >= selection.maximum);
    }
    const count = state.root.querySelector('[data-gfw-selection-count]');
    if (count) count.textContent = state.selected.length ? `${state.selected.length} selected` : 'Choose items';
    if (state.selected.length) state.selecting = true;
    state.root.dataset.gfwSelecting = String(!!state.selecting);
    const toggle = state.root.querySelector('[data-gfw-selection-toggle]');
    if (toggle) {
      toggle.textContent = state.selecting ? 'Done' : 'Select';
      toggle.setAttribute('aria-pressed', String(!!state.selecting));
      toggle.disabled = state.busy || !!state.queryPending || state.uncertain || modalOpen();
    }
  };

  const persistTabs = config => {
    if (!tabsMetadata) return;
    try {
      const clean = components.restoreMetadata(config, JSON.stringify(tabsMetadata), location.href);
      const raw = JSON.stringify(clean);
      if (raw.length <= 32768) localStorage.setItem(tabsScope, raw);
    } catch (_) {}
  };

  const saveTab = state => {
    if (!current(state) || !tabsMetadata || tabsScope !== components.metadataKey(state.config) || !state.tabID || !state.root.isConnected) return;
    const safe = local(state.config, state.config.route);
    if (!safe) return;
    const destination = new URL(safe, location.href);
    for (const field of state.form.querySelectorAll('[data-gfw-field]')) {
      if (field.type !== 'hidden') destination.searchParams.delete(field.dataset.gfwField);
    }
    for (const key of ['action', 'row', '_csrf', state.config.list?.selection?.key].filter(Boolean)) destination.searchParams.delete(key);
    const active = document.activeElement;
    tabsMetadata.contexts[state.tabID] = { route: destination.pathname + destination.search,
      scroll: Math.max(0, window.scrollY || 0), focus: state.root.contains(active) ? active?.dataset?.gfwField || active?.dataset?.gfwFilter || active?.dataset?.gfwQuery || '' : '' };
    tabsMetadata.active = state.tabID;
    persistTabs(state.config);
  };

  const restoreTabContext = (state, context) => {
    const restore = () => {
      if (!state.root.isConnected || !current(state)) return;
      window.scrollTo?.({ top: context?.scroll || 0, behavior: 'instant' });
      if (context?.focus) {
        const field = Array.from(state.root.querySelectorAll('[data-gfw-field], [data-gfw-filter], [data-gfw-query]'))
          .find(item => (item.dataset.gfwField ?? item.dataset.gfwFilter ?? item.dataset.gfwQuery) === context.focus && !item.disabled && item.type !== 'hidden');
        field?.focus({ preventScroll: true });
      }
    };
    (window.requestAnimationFrame || (callback => setTimeout(callback, 0)))(restore);
  };

  const initializeTabs = state => {
    const { config } = state;
    if (!config.tabs?.length || config.tabs.length > 16) return;
    const scope = components.metadataKey(config);
    if (scope !== tabsScope) {
      tabsScope = scope;
      let raw;
      try { raw = localStorage.getItem(scope); } catch (_) {}
      tabsMetadata = components.restoreMetadata(config, raw, location.href);
    }
    state.tabID = config.tabs.find(tab => tab.id === config.activeTab)?.id ||
      config.tabs.find(tab => tab.route.split('?')[0] === config.route.split('?')[0])?.id || tabsMetadata.active || config.tabs[0].id;
    for (const anchor of state.root.querySelectorAll('[data-gfw-tab]')) {
      if (anchor.dataset.gfwTab === state.tabID) anchor.setAttribute('aria-current', 'page');
      else anchor.removeAttribute('aria-current');
    }
    if (pendingTab) {
      const context = pendingTab.route === config.route && pendingTab.id === state.tabID ? pendingTab.context : null;
      pendingTab = null;
      tabsMetadata.active = state.tabID;
      persistTabs(config);
      if (context) restoreTabContext(state, context);
    }
    if (!restoredTabs) {
      restoredTabs = true;
      const entry = tabsMetadata.contexts[tabsMetadata.active];
      const address = new URL(location.href);
      const atRoot = !address.search && (address.pathname === '/' || address.pathname === config.root.split('?')[0]);
      if (atRoot && entry && entry.route !== config.route) {
        const id = tabsMetadata.active;
        void state.ready.then(async () => {
          if (!state.root.isConnected || state.navigationEpoch || state.queryEpoch || state.revision || !(await leave(state.root, state))) return;
          pendingTab = { id, route: entry.route, context: entry };
          navigate(state.root, state, entry.route, { replace: true, force: true });
        }).catch(() => {});
      } else if (entry?.route === config.route) restoreTabContext(state, entry);
    }
  };

  const retire = (state, retainRendering = false) => {
    if (state.retired) return;
    state.retired = true;
    state.live?.close();
    if (!retainRendering) state.rendering?.dispose();
    clearTimeout(state.checkpointTimer);
    if (swipe?.state === state) resetSwipe();
    presentations.close(state.root, false);
    files?.close(state.root, false);
    services?.close(state.root, false);
    services?.releasePlayback(state.root);
    activeScreens.delete(state);
    if (state.instance) void command(state.root, state, 'close', { values: {} }).catch(() => {});
  };

  const scan = (committed = false) => {
    for (const root of document.querySelectorAll('[data-gfw-screen]')) {
      const source = root.dataset.gfwConfig;
      const form = root.querySelector('[data-gfw-form]');
      const previous = initialized.get(root);
      if (previous && committed !== true && previous.source === source && previous.form === form) continue;
      let config;
      try { config = JSON.parse(source); } catch (_) { continue; }
      if (!form) continue;
      const retainRendering = previous && previous.form === form && previous.config.route === config.route;
      if (previous) retire(previous, retainRendering);
      delete root.dataset.gfwInstance;
      delete root.dataset.gfwEditor;
      const state = { config, source, form, root, baseline: values(form, true, true), initialStatus: root.querySelector('[data-gfw-status]').textContent,
        busy: false, revision: 0, receipts: [], checkpointTimer: null, composing: retainRendering ? previous.composing : new Set(),
        rendering: retainRendering ? previous.rendering : undefined,
        selected: components.reconcile(config.list?.selection?.selected, config.list?.rows || [], config.list?.selection?.maximum), actionRow: '' };
      const identity = components.collectionIdentity(config, location.href);
      const previousSelection = identity && collectionSelections.get(identity.key);
      if (identity && previousSelection?.query === identity.query) state.selected = previousSelection.selected;
      initialized.set(root, state);
      activeScreens.add(state);
      const overflow = root.querySelector('[data-gfw-header-menu]');
      const headerActions = root.querySelector('.gfm-nav > .gfm-nav-actions:last-child');
      if (overflow && headerActions && overflow.parentElement !== headerActions) headerActions.append(overflow);
      root.dataset.gfwCapabilities = JSON.stringify(capabilities());
      if (dirty(state)) {
        remember(root, state);
        formResult(state, false);
      } else if (config.committed) {
        if (!removeDraft(state)) status(root, 'Saved, but the old local draft could not be removed.', true);
      } else if (storageKey(state) && form.dataset.gfwError !== 'true' && !state.composing.size) {
        try {
          const raw = sessionStorage.getItem(storageKey(state));
          const draft = raw ? JSON.parse(raw) : null;
          if (draft && draft.values && typeof draft.values === 'object') {
            for (const field of form.querySelectorAll('[data-gfw-field]')) {
              if (field.type !== 'hidden' && Object.hasOwn(draft.values, field.dataset.gfwField) && typeof draft.values[field.dataset.gfwField] === 'string') {
                components.setField(field, draft.values[field.dataset.gfwField]);
              }
            }
            if (dirty(state)) {
              formResult(state, false);
              status(root, config.draft.restored || 'Unsaved draft restored · save to keep it');
            } else {
              removeDraft(state);
            }
          }
        } catch (_) {
          status(root, 'Local draft recovery is unavailable. Save changes before leaving this screen.', true);
        }
      }
      if (pendingAnnouncement) {
        status(root, pendingAnnouncement);
        pendingAnnouncement = '';
      }
      state.ready = start(root, state).catch(error => {
        if (current(state)) status(root, error.message || 'This screen could not connect to its Go services.', true);
        throw error;
      });
      void state.ready.catch(() => {});
      syncSelection(state);
      initializeTabs(state);
    }
    for (const state of activeScreens) {
      if (!state.root.isConnected) retire(state);
    }
  };

  window.addEventListener('gofastr:navigate', (event) => {
    if (pendingNavigation) {
      const pending = pendingNavigation;
      pendingNavigation = null;
      acknowledge(pending.root, pending.state, pending.requestId, 'navigation', pending.id, 'completed');
    }
    const root = document.querySelector('[data-gfw-screen]');
    let config;
    try { config = JSON.parse(root?.dataset.gfwConfig); } catch (_) { return; }
    if (!history.state?.gfwAnnotated) {
      const previous = local(config, event.detail?.prevPath);
      try { history.replaceState({ ...history.state, gfwAnnotated: true, gfwPrevious: previous }, ''); } catch (_) {}
    }
    scan(true);
  });

  const background = () => {
    for (const state of activeScreens) {
      saveTab(state); state.live?.suspend();
      if (!state.hasEditor || !current(state) || !state.root.isConnected) continue;
      clearTimeout(state.checkpointTimer);
      remember(state.root, state);
      void checkpoint(state.root, state, true).catch(error => {
        if (current(state)) status(state.root, error.message || 'The latest draft has not been checkpointed.', true);
      });
    }
  };
  window.addEventListener('pagehide', background);
  window.addEventListener('pageshow', () => { for (const state of activeScreens) state.live?.resume(); });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) background();
    else for (const state of activeScreens) state.live?.resume();
  });
  const activeScreen = () => Array.from(activeScreens).find(state => current(state) && state.root.isConnected);
  const historyBarrier = window.__gofastr_lifecycle?.createHistory({
    required: () => {
      const state = activeScreen();
      return !!state && (state.hasEditor || state.config.draft || state.form.querySelector('[data-gfw-field]:not([type="hidden"])') ||
        state.busy || state.uncertain || state.composing.size > 0 || modalOpen());
    },
    authorize: async () => {
      const state = activeScreen();
      if (!state || !(await leave(state.root, state))) return null;
      const revision = state.revision;
      return () => current(state) && state.root.isConnected && state.revision === revision && !state.busy &&
        !state.uncertain && !state.composing.size && !state.editor?.pending && !state.editor?.unknown && !modalOpen();
    },
    report: message => { const state = activeScreen(); if (state) status(state.root, message, true); },
  });
  let tabSaveTimer;
  window.addEventListener('scroll', () => {
    clearTimeout(tabSaveTimer);
    tabSaveTimer = setTimeout(() => { for (const state of activeScreens) saveTab(state); }, 120);
  }, { passive: true });
  window.__gofastr_webview = {
    async openRoute(destination) {
      const state = Array.from(activeScreens).find(item => item.root.isConnected);
      if (!state || historyBarrier?.pending || typeof runtime?.navigate !== 'function' || !local(state.config, destination)) return false;
      const epoch = state.navigationEpoch = (state.navigationEpoch || 0) + 1;
      if (!(await leave(state.root, state)) || !state.root.isConnected || epoch !== state.navigationEpoch) return false;
      navigate(state.root, state, destination, { force: true });
      return true;
    },
  };

  document.addEventListener('compositionstart', event => {
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (state && event.target.matches('[data-gfw-field], [data-gfw-filter]')) {
      state.composing.add(event.target);
      clearTimeout(state.checkpointTimer);
    }
  });

  document.addEventListener('compositionend', event => {
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state) return;
    state.composing.delete(event.target);
    if (event.target.matches('[data-gfw-field]')) scheduleCheckpoint(root, state);
    if (event.target.matches('[data-gfw-filter]')) void updateQuery(root, state);
  });

  const scheduleCheckpoint = (root, state) => {
    clearTimeout(state.checkpointTimer);
    if (state.composing.size) return;
    state.checkpointTimer = setTimeout(() => void checkpoint(root, state).catch(error => {
      if (root.isConnected && current(state)) status(root, error.message || 'The latest draft has not been checkpointed.', true);
    }), 250);
  };

  const updateQuery = async (root, state, paging = false) => {
    if (state.busy || state.composing.size || modalOpen()) return;
    const epoch = state.queryEpoch = (state.queryEpoch || 0) + 1;
    const query = queryValues(state);
    if (paging && state.config.list?.paging) query[state.config.list.paging.key] = state.config.list.paging.next;
    const destination = components.queryRoute(state.config, query, location.href, !paging);
    state.queryPending = destination;
    if (!(await leave(root, state)) || !root.isConnected || epoch !== state.queryEpoch) {
      if (epoch === state.queryEpoch) { state.queryPending = ''; syncSelection(state); }
      return;
    }
    if (!paging) state.selected = [];
    syncSelection(state);
    if (state.live) {
      await state.live.refresh({ route: destination, replace: !paging });
      if (current(state) && epoch === state.queryEpoch) { state.queryPending = ''; syncSelection(state); }
      return;
    }
    navigate(root, state, destination, { replace: !paging, force: true });
  };

  document.addEventListener('invalid', event => {
    const disclosure = event.target.closest('[data-gfw-details], [data-gfw-filters]');
    if (disclosure) disclosure.open = true;
  }, true);

  document.addEventListener('toggle', event => {
    const menu = event.target;
    if (!menu.matches?.('[data-gfw-row-menu], [data-gfw-header-menu]') || !menu.open) return;
    for (const other of document.querySelectorAll('[data-gfw-row-menu], [data-gfw-header-menu]')) {
      if (other !== menu) other.open = false;
    }
    const controls = menu.querySelector('div');
    const anchor = menu.querySelector('summary');
    if (!controls || !anchor) return;
    const bounds = anchor.getBoundingClientRect();
    const root = menu.closest('[data-gfw-screen]');
    const tabs = root?.querySelector('[aria-label="App tabs"]');
    const top = Math.max(12, root?.querySelector('.gfm-nav')?.getBoundingClientRect().bottom ?? 12);
    const bottom = Math.min(window.innerHeight - 12, tabs?.getBoundingClientRect().top ?? window.innerHeight - 12);
    controls.style.position = 'fixed';
    controls.style.maxHeight = Math.max(44, bottom - top - 16) + 'px';
    controls.style.overflowY = 'auto';
    controls.style.insetInlineEnd = 'auto';
    controls.style.bottom = 'auto';
    controls.style.top = Math.max(top + 8, Math.min(bounds.bottom + 4, bottom - controls.offsetHeight - 8)) + 'px';
    controls.style.left = Math.max(12, Math.min(bounds.right - controls.offsetWidth, window.innerWidth - controls.offsetWidth - 12)) + 'px';
  }, true);

  document.addEventListener('input', (event) => {
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (state && event.target.matches('[data-gfw-field]')) {
      if (event.target.disabled) return;
      if (event.target.type === 'checkbox') event.target.dataset.gfwOptionalEmpty = 'false';
      state.revision++;
      remember(root, state);
      if (!event.isComposing) scheduleCheckpoint(root, state);
    }
  });

  document.addEventListener('change', event => {
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state || event.target.disabled) return;
    if (event.target.matches('[data-gfw-select]')) {
      if (state.busy || state.queryPending || modalOpen()) { syncSelection(state); return; }
      const identifier = event.target.dataset.gfwSelect;
      state.selected = event.target.checked ? [...state.selected, identifier] : state.selected.filter(item => item !== identifier);
      syncSelection(state);
    } else if (event.target.matches('[data-gfw-filter]')) {
      if (event.target.type === 'checkbox') event.target.dataset.gfwOptionalEmpty = 'false';
      void updateQuery(root, state);
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter' || event.isComposing || !event.target.matches('input[data-gfw-field]:not([type="hidden"])') ||
        ['checkbox', 'date', 'time'].includes(event.target.type)) return;
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state?.config.defaultAction) return;
    const primary = Array.from(root.querySelectorAll('button[form="gfw-form"]')).find((button) => button.value === state.config.defaultAction);
    if (primary && !primary.disabled) {
      event.preventDefault();
      state.form.requestSubmit(primary);
    }
  });

  document.addEventListener('gofastr:beforenavigate', (event) => {
    const root = event.target.closest?.('[data-gfw-screen]') || document.querySelector('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (historyBarrier?.pending || modalOpen() || (state && (state.busy || state.leaving || state.editor?.pending || state.editor?.unknown))) {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (state) status(root, 'Finish the open task or resolve the pending action before leaving.', true);
    } else if (state && !local(state.config, event.detail?.path)) {
      event.preventDefault();
      status(root, 'This destination is not an app screen.', true);
    } else state?.live?.suspend();
  }, true);

  document.addEventListener('click', async (event) => {
    if (suppressedClick && event.detail !== 0 && Date.now() <= suppressedClick.until && suppressedClick.row.contains(event.target)) {
      event.preventDefault();
      event.stopImmediatePropagation();
      suppressedClick = null;
      return;
    }
    const root = event.target.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    if (state.leaving || historyBarrier?.pending) {
      event.preventDefault();
      event.stopImmediatePropagation();
      status(root, 'Wait for the draft and navigation check to finish. Your text is still here.');
      return;
    }
    if (event.target.closest('[data-gfw-file-download-link], [data-gfw-service-download-link]')) return;
    if (event.target.closest('[data-gfw-live-controls]')) return;
    if (modalOpen() && !event.target.closest('[data-gfw-file], [data-gfw-presentation], [data-gfw-service]')) {
      event.preventDefault();
      event.stopImmediatePropagation();
      status(root, 'Finish or cancel the open modal task first.', true);
      return;
    }
    if (event.target.closest('[data-gfw-selection-toggle]')) {
      if (state.busy || state.queryPending || state.uncertain) return;
      state.selecting = !state.selecting;
      if (!state.selecting) state.selected = [];
      syncSelection(state);
      return;
    }
    const discard = event.target.closest('[data-gfw-discard]');
    if (discard) {
      if (state.busy || !window.confirm(state.config.draft.discard + '?')) return;
      const finish = beginResponse(state);
      const release = holdCheckpoints(state);
      try {
        await state.ready;
        state.revision++;
        const revision = state.revision;
        const reply = await command(root, state, 'leave', { choice: 'discard' });
        if (!current(state) || !root.isConnected) return;
        observe(root, state, reply);
        if (reply.error || !reply.editor?.canLeave || state.revision !== revision || state.composing.size) {
          status(root, reply.error?.message || 'The draft changed or has a pending save. Your text is still here.', true);
          return;
        }
        for (const field of state.form.querySelectorAll('[data-gfw-field]')) {
          if (field.type !== 'hidden') components.setField(field, state.baseline[field.dataset.gfwField] ?? '');
        }
        const removed = removeDraft(state);
        showFields(state);
        formResult(state, true);
        status(root, removed ? 'Draft discarded. Saved fields have not been changed.' : 'Fields restored, but the local draft could not be removed.', !removed);
      } catch (error) {
        if (current(state)) status(root, error.message || 'The draft could not be discarded. Your text is still here.', true);
      } finally {
        finish();
        release();
      }
      return;
    }
    const anchor = event.target.closest('a[href]');
    if (!anchor) return;
    if (anchor.hasAttribute('data-gfw-tab')) {
      event.preventDefault();
      const tab = state.config.tabs?.find(item => item.id === anchor.dataset.gfwTab);
      if (!tab || tab.id === state.tabID) return;
      const epoch = state.navigationEpoch = (state.navigationEpoch || 0) + 1;
      if (!(await leave(root, state)) || !root.isConnected || epoch !== state.navigationEpoch) return;
      const context = tabsMetadata?.contexts[tab.id];
      const destination = local(state.config, context?.route) || local(state.config, tab.route);
      if (!destination) return;
      pendingTab = { id: tab.id, route: destination, context };
      navigate(root, state, destination, { force: true });
      return;
    }
    if (anchor.hasAttribute('data-gfw-load-more')) {
      event.preventDefault();
      if (!state.queryPending) void updateQuery(root, state, true);
      return;
    }
    if (!local(state.config, anchor.getAttribute('href'))) {
      event.preventDefault();
      status(root, 'This destination is not an app screen.', true);
    } else if (anchor.matches('.gfm-back')) {
      event.preventDefault();
      if (await leave(root, state)) back(root, state, anchor.getAttribute('href'));
    } else if (anchor.hasAttribute('data-gfw-refresh')) {
      event.preventDefault();
      if (await leave(root, state)) {
        if (state.live) await state.live.refresh({ live: true });
        else navigate(root, state, state.config.route, { replace: true, force: true });
      }
    } else {
      event.preventDefault();
      if (await leave(root, state)) navigate(root, state, anchor.getAttribute('href'));
    }
  }, true);

  let swipe;
  let suppressedClick;
  const rowSides = row => Object.fromEntries(['leading', 'trailing'].map(side => [side,
    Array.from(row.querySelectorAll('[data-gfw-row-action]')).filter(button => button.dataset.gfwSide === side && !button.disabled)
      .map(button => ({ id: button.dataset.gfwRowAction, label: button.textContent, role: button.dataset.gfwRole,
        fullSwipe: button.dataset.gfwFullSwipe === 'true', button }))]));
  const resetSwipe = () => {
    if (!swipe) return;
    const previous = swipe;
    swipe = null;
    const content = previous.row.querySelector('.gfw-row-content');
    if (content) content.style.transform = '';
    delete previous.row.dataset.gfwSwipeLabel;
    previous.row.classList.remove('gfw-swiping');
    try { previous.row.releasePointerCapture?.(previous.gesture.pointerId); } catch (_) {}
  };
  document.addEventListener('pointerdown', event => {
    suppressedClick = null;
    resetSwipe();
    if (event.target.closest('input, button, select, textarea, summary, details')) return;
    const row = event.target.closest('[data-gfw-row]');
    const root = row?.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state || !row.querySelector('[data-gfw-row-action]')) return;
    const gesture = components.gestureStart({ x: event.clientX, y: event.clientY, width: row.getBoundingClientRect().width,
      pointerId: event.pointerId, primary: event.isPrimary, button: event.button,
      blocked: state.busy || state.queryPending || state.uncertain || state.editor?.pending || state.editor?.unknown || state.composing.size > 0 || modalOpen() || row.dataset.gfwDisabled === 'true' });
    if (gesture) swipe = { row, state, gesture };
  }, { passive: true });
  document.addEventListener('pointermove', event => {
    if (!swipe || swipe.gesture.pointerId !== event.pointerId) return;
    if (swipe.state.busy || !swipe.row.isConnected || modalOpen()) { resetSwipe(); return; }
    const sides = rowSides(swipe.row);
    swipe.gesture = components.gestureMove(swipe.gesture, { x: event.clientX, y: event.clientY, pointerId: event.pointerId }, sides);
    if (swipe.gesture.phase !== 'horizontal') return;
    if (event.cancelable) event.preventDefault();
    try { swipe.row.setPointerCapture?.(event.pointerId); } catch (_) {}
    swipe.row.classList.add('gfw-swiping');
    swipe.row.dataset.gfwSwipeLabel = sides[swipe.gesture.side][0]?.label || 'Actions';
    const content = swipe.row.querySelector('.gfw-row-content');
    if (content) content.style.transform = `translateX(${swipe.gesture.dx}px)`;
  }, { passive: false });
  const finishSwipe = (event, cancelled = false) => {
    if (!swipe || swipe.gesture.pointerId !== event.pointerId) return;
    const { row, state, gesture } = swipe;
    const sides = rowSides(row);
    const outcome = components.gestureEnd(gesture, sides, cancelled || state.busy || modalOpen() || !row.isConnected);
    if (gesture.phase !== 'tracking') suppressedClick = { row, until: Date.now() + 500 };
    resetSwipe();
    if (outcome.kind === 'action') sides[outcome.side].find(action => action.id === outcome.id)?.button.click();
    else if (outcome.kind === 'menu') {
      const menu = row.querySelector('[data-gfw-row-menu]');
      menu.open = true;
      sides[outcome.side][0]?.button.focus({ preventScroll: true });
    }
  };
  document.addEventListener('pointerup', event => finishSwipe(event));
  document.addEventListener('pointercancel', event => finishSwipe(event, true));
  document.addEventListener('lostpointercapture', event => {
    if (event.target === swipe?.row) finishSwipe(event, true);
  });

  const share = async (root, text) => {
    if (typeof navigator.share === 'function') {
      try { await navigator.share({ text }); return { status: 'completed', message: 'Shared.' }; }
      catch (error) { if (error.name === 'AbortError') return { status: 'cancelled', message: 'Sharing cancelled. Your text is unchanged.' }; }
    }
    const disclosure = root.querySelector('[data-gfw-share]');
    disclosure.hidden = false;
    disclosure.open = true;
    disclosure.querySelector('textarea').value = text;
    return { status: 'unsupported', message: 'System sharing unavailable. Select and copy the share text below.' };
  };

  const showFields = (state, fields = {}) => {
    for (const field of state.root.querySelectorAll('[data-gfw-field], [data-gfw-filter]')) {
      const key = field.dataset.gfwField ?? field.dataset.gfwFilter;
      const message = Object.hasOwn(fields, key) ? fields[key] : '';
      if (message) field.setAttribute('aria-invalid', 'true');
      else field.removeAttribute('aria-invalid');
      if (message) {
        const group = field.closest('[data-gfw-details]') || field.closest('[data-gfw-filters]');
        if (group) group.open = true;
      }
      const error = Array.from(state.root.querySelectorAll('[data-gfw-field-error], [data-gfw-filter-error]')).find(item => (item.dataset.gfwFieldError ?? item.dataset.gfwFilterError) === key);
      if (error) {
        error.textContent = message;
        if (message) field.setAttribute('aria-describedby', error.id);
        else field.removeAttribute('aria-describedby');
      }
    }
  };

  const beginResponse = (state) => {
    state.live?.suspend();
    const buttons = new Map(Array.from(state.root.querySelectorAll('button[form="gfw-form"]')).map((button) => [button, button.disabled]));
    state.busy = true;
    state.form.setAttribute('aria-busy', 'true');
    for (const button of buttons.keys()) button.disabled = true;
    syncSelection(state);
    resetSwipe();
    return () => {
      const release = state.completionCheckpoint;
      state.completionCheckpoint = undefined;
      if (!current(state)) { release?.(); return; }
      state.busy = false;
      state.form.removeAttribute('aria-busy');
      for (const [button, wasDisabled] of buttons) button.disabled = wasDisabled;
      syncSelection(state);
      if (state.refreshAfterResponse && state.live) {
        state.refreshAfterResponse = false;
        void state.live.refresh({ live: true });
      } else state.live?.resume();
      release?.();
    };
  };

  const readCompletion = async (root, state, requestId) => {
    state.completionCheckpoint ||= holdCheckpoints(state);
    for (let attempt = 0; attempt < 2; attempt++) {
      const reply = await command(root, state, 'complete', { requestId });
      if (!root.isConnected || !current(state) || !(reply.error?.code === 'stale' || (reply.revision !== state.revision && !reply.error))) return reply;
    }
    state.uncertain = true;
    throw new Error('The operation outcome could not be confirmed for this buffer. It was not retried. Keep this draft and inspect saved items.');
  };

  const completeResponse = async (root, state, kind, requestId, extra) => {
    let submitted;
    let writeReply = false;
    try {
      submitted = await command(root, state, kind, { requestId, ...extra });
      writeReply = true;
    } catch (_) {
      try { submitted = await readCompletion(root, state, requestId); }
      catch (error) { state.uncertain = true; throw error; }
    }
    if (submitted.error && !submitted.response) return { ...submitted, writeRejected: writeReply };
    try { return await readCompletion(root, state, requestId); }
    catch (error) { state.uncertain = true; throw error; }
  };

  const respond = async (root, state, presentationId, choice, selected, opener) => {
    if (state.busy || !root.isConnected) return;
    const finish = beginResponse(state);
    const requestId = crypto.randomUUID();
    clearTimeout(state.checkpointTimer);
    try {
      const reply = await completeResponse(root, state, 'respond', requestId, { presentationId, choice, selected });
      if (!current(state)) return;
      observe(root, state, reply);
      if (reply.error) {
        if (reply.response || !['invalid_request', 'validation'].includes(reply.error.code)) presentations.close(root, false);
        showFields(state, reply.editor?.fields || reply.error.fields);
        throw new Error(reply.error.message);
      }
      presentations.close(root, false);
      await applyResponse(root, state, requestId, reply, opener);
    } catch (error) {
      if (current(state)) status(root, error.message || 'Could not complete this response. Your text is still here.', true);
      throw error;
    } finally {
      finish();
      if (current(state) && !modalOpen() && root.isConnected && opener?.isConnected && !opener.disabled) opener.focus({ preventScroll: true });
    }
  };

  const respondFile = async (root, state, fileId, fileResult, opener) => {
    if (state.busy || !root.isConnected) throw Object.assign(new Error('Wait for the pending action.'), { fileRetryable: true });
    const requestId = crypto.randomUUID();
    const run = async (readOnly = false) => {
      const finish = beginResponse(state);
      clearTimeout(state.checkpointTimer);
      let observed = false;
      try {
        const reply = readOnly ? await readCompletion(root, state, requestId) :
          await completeResponse(root, state, 'file-result', requestId, { fileId, fileResult });
        if (!current(state)) return;
        if (!reply.response && (!reply.error || !reply.writeRejected || reply.error.code === 'pending')) {
          throw new Error(reply.error?.message || 'The file result has not been observed. It was not retried.');
        }
        observed = true;
        observe(root, state, reply);
        if (reply.error) {
          const retryable = reply.writeRejected && !reply.response && ['invalid_request', 'validation'].includes(reply.error.code);
          if (!retryable) files.close(root, false);
          showFields(state, reply.editor?.fields || reply.error.fields);
          throw Object.assign(new Error(reply.error.message), { fileRetryable: retryable });
        }
        files.close(root, false);
        await applyResponse(root, state, requestId, reply, opener);
        if (fileResult.status === 'completed' && !fileResult.file) {
          const message = root.querySelector('[data-gfw-status]')?.textContent || '';
          status(root, 'Download initiated. This WebView cannot confirm an external disk save. ' + message);
        }
      } catch (error) {
        if (!observed) error.complete = () => run(true);
        if (current(state)) status(root, error.message || 'The file result is uncertain. It was not retried.', true);
        throw error;
      } finally {
        finish();
        if (current(state) && !modalOpen() && root.isConnected && opener?.isConnected && !opener.disabled) opener.focus({ preventScroll: true });
      }
    };
    await run();
  };

  const respondService = async (root, state, serviceId, serviceResult, opener) => {
    if (state.busy || !current(state) || !root.isConnected) throw new Error('The service screen is no longer ready. No operation was repeated.');
    const requestId = crypto.randomUUID();
    const run = async (readOnly = false) => {
      const finish = beginResponse(state);
      clearTimeout(state.checkpointTimer);
      let observed = false;
      try {
        const reply = readOnly ? await readCompletion(root, state, requestId) :
          await completeResponse(root, state, 'service-result', requestId, { serviceId, serviceResult });
        if (!current(state) || !root.isConnected) return;
        if (!reply.response && (!reply.writeRejected || ['pending', 'unknown'].includes(reply.error?.code))) throw new Error(reply.error?.message || 'The service result has not been observed. Only completion can be checked.');
        observed = true;
        observe(root, state, reply);
        if (reply.error) {
          showFields(state, reply.editor?.fields || reply.error.fields);
          throw new Error(reply.error.message);
        }
        services.close(root, false);
        await applyResponse(root, state, requestId, reply, opener);
      } catch (error) {
        if (!observed) error.complete = () => run(true);
        if (current(state)) status(root, error.message || 'The service result is uncertain. It was not retried.', true);
        throw error;
      } finally {
        finish();
        if (current(state) && !modalOpen() && root.isConnected && opener?.isConnected && !opener.disabled) opener.focus({ preventScroll: true });
      }
    };
    await run();
  };

  const applyResponse = async (root, state, requestId, reply, opener) => {
    if (!current(state) || !root.isConnected) return;
    observe(root, state, reply);
    if (reply.error) {
      showFields(state, reply.editor?.fields || reply.error.fields);
      throw new Error(reply.error.message);
    }
    runtime?.invalidate?.('*');
    if (!root.isConnected) return;
    const result = reply.response;
    if (!result) throw new Error('The action has no observed result. It was not retried.');
    if (result.saved) {
      const oldKey = storageKey(state);
      state.config.route = result.saved.route;
      if (state.config.draft && result.saved.draftKey) state.config.draft.key = result.saved.draftKey;
      if (oldKey && oldKey !== storageKey(state)) sessionStorage.removeItem(oldKey);
      const previous = history.state;
      runtime?._pushURL?.(result.saved.route, { replace: true });
      history.replaceState({ ...history.state, gfwAnnotated: true, gfwPrevious: previous?.gfwPrevious }, '');
      root.dataset.gfwConfig = JSON.stringify(state.config);
      state.source = root.dataset.gfwConfig;
    }
    if (reply.editor && reply.revision === state.revision) {
      state.baseline = reply.editor.baseline;
      if (!reply.editor.dirty) {
        for (const field of state.form.querySelectorAll('[data-gfw-field]')) {
          const confirmed = reply.editor.values[field.dataset.gfwField] ?? '';
          if (field.type !== 'hidden' && !state.composing.has(field)) components.setField(field, confirmed);
        }
      }
    }
    const messages = [];
    if (reply.editor?.dirty) {
      remember(root, state);
      formResult(state, false);
      if (result.saved || result.clearDraft) messages.push('The submitted version was saved. Newer edits remain unsaved; save again to keep them.');
    } else if (result.clearDraft) {
      const removed = removeDraft(state);
      formResult(state, true);
      if (!removed) messages.push('Saved, but the old local draft could not be removed.');
    }
    if (result.share) {
      const outcome = await share(root, result.share);
      messages.push(outcome.message);
      acknowledge(root, state, requestId, 'share', reply.effectIds?.share, outcome.status, outcome.message);
    }
    if (result.haptic) {
      let vibrated = false;
      try { vibrated = typeof navigator.vibrate === 'function' && navigator.vibrate(result.haptic === 'success' ? [15, 30, 15] : 20); } catch (_) {}
      if (!vibrated) messages.push('Haptic feedback is unavailable in this WebView.');
      acknowledge(root, state, requestId, 'haptic', reply.effectIds?.haptic, vibrated ? 'completed' : 'unsupported');
    }
    if (result.announcement) {
      messages.push(result.announcement);
      acknowledge(root, state, requestId, 'announcement', reply.effectIds?.announcement, 'completed');
    }
    if (result.navigation?.kind === 'sheet') messages.push('Opened as a full page in this WebView, not a native sheet.');
    const message = messages.join(' ') || (result.service ? 'Choose a device service in the open dialog.' : result.files ? 'Choose a file task in the open dialog.' :
      result.presentation ? 'Choose an action in the open presentation.' : result.clearDraft ? 'Saved.' : 'Action completed.');
    status(root, message);
    if (result.presentation) {
      if (!reply.presentationId) throw new Error('The presentation has no issued identity. Your text is still here.');
      presentations.open({ root, presentation: result.presentation, presentationId: reply.presentationId, opener,
        respond: (choice, selected, originalOpener) => respond(root, state, reply.presentationId, choice, selected, originalOpener) });
    }
    if (result.files) {
      if (!reply.fileId || !files) throw new Error('The file task has no issued identity or adapter. Your text is still here.');
      files.open({ root, files: result.files, fileId: reply.fileId, opener,
        respond: (fileResult, originalOpener) => respondFile(root, state, reply.fileId, fileResult, originalOpener) });
    }
    if (result.service) {
      if (!reply.serviceId || !services) throw new Error('The service has no issued identity or adapter. Your text is still here.');
      services.open({ root, request: result.service, serviceId: reply.serviceId, opener,
        respond: (outcome, originalOpener) => respondService(root, state, reply.serviceId, outcome, originalOpener) });
    }
    if (result.navigation) {
      pendingNavigation = { root, state, requestId, id: reply.effectIds?.navigation };
      pendingAnnouncement = message;
      switch (result.navigation.kind) {
        case 'push':
        case 'sheet':
          navigate(root, state, result.navigation.route, { force: true });
          break;
        case 'pop':
        case 'dismiss':
          back(root, state, result.navigation.route || state.config.root);
          break;
        default:
          pendingAnnouncement = '';
          status(root, 'Unsupported navigation. Your text is still here.', true);
      }
    } else if (state.config.list && !result.presentation && !result.files && !result.service) {
      pendingAnnouncement = message;
      if (state.live) {
        state.refreshAfterResponse = true;
        pendingAnnouncement = '';
      } else navigate(root, state, state.config.route, { replace: true, force: true });
    }
  };

  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    const root = form.closest('[data-gfw-screen]');
    const state = root && initialized.get(root);
    if (!state) return;
    event.preventDefault();
    if (modalOpen()) return;
    if (form.hasAttribute('data-gfw-search')) {
      void updateQuery(root, state);
      return;
    }
    if (form !== state.form || state.busy || state.leaving || historyBarrier?.pending || state.queryPending || state.uncertain || state.composing.size) return;
    const action = event.submitter?.value || '';
    const route = state.config.routes.find((item) => {
      const expected = item.path.split('/');
      const actual = state.config.route.split('?')[0].split('/').map(decodeURIComponent);
      return expected.length === actual.length && expected.every((segment, index) => segment.startsWith('{') && segment.endsWith('}') ? actual[index] !== '' : segment === actual[index]);
    });
    if (!route?.actions.includes(action)) {
      status(root, 'Choose an available action.', true);
      return;
    }
    const rowButton = event.submitter?.hasAttribute('data-gfw-row-action') && event.submitter;
    if (rowButton) {
      const row = state.config.list?.rows.find(item => item.id === rowButton.dataset.gfwRowId);
      if (!row || row.disabled || event.submitter.disabled || ![...(row.leadingActions || []), ...(row.trailingActions || [])].some(item => item.id === action)) return;
    }
    state.actionRow = rowButton ? rowButton.dataset.gfwRowId : '';
    const requestId = crypto.randomUUID();
    const opener = event.submitter || document.activeElement;
    const finish = beginResponse(state);
    showFields(state);
    status(root, 'Working…');
    try {
      await state.ready;
      if (!current(state) || !root.isConnected) return;
      clearTimeout(state.checkpointTimer);
      const reply = await completeResponse(root, state, 'action', requestId, { action });
      await applyResponse(root, state, requestId, reply, opener);
    } catch (error) {
      if (current(state)) {
        formResult(state, false);
        status(root, error.message || 'Could not complete the action. Your text is still here.', true);
      }
    } finally {
      finish();
    }
  }, true);

  new MutationObserver(() => scan()).observe(document.documentElement, {
    childList: true, subtree: true, attributes: true, attributeFilter: ['data-gfw-config'],
  });
  scan();
  if (runtime) (runtime.loadedModules ||= {})['gfm-webview'] = true;
})();
